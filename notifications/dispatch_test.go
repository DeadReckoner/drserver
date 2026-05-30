package notifications

import (
	"errors"
	"testing"
	"time"

	"dr/server/types"
)

// Dispatch is glue, but it has a non-trivial branching tree:
//   - settings load fails    → skip
//   - resolver says skip     → no delivery log, no send
//   - resolver says send + log claim OK → push fires
//   - resolver says send + log claim says already-delivered → no push (dedup)
//   - apply_to_all_friends   → override lookup is skipped entirely
//   - override available     → flows into resolver
//
// These cases ensure each branch is exercised.

// =============================================================================
// Test plumbing — a fake Dispatcher whose stubs the test controls.
// =============================================================================

type sentPush struct {
	userID, notificationType, title, body string
	data                                  map[string]any
}

type fake struct {
	settings       types.NotificationSettings
	settingsErr    error
	tz             string
	friendOverride *types.NotificationOverride
	anchorOverride *types.NotificationOverride
	overrideErr    error
	recordInserted bool
	recordErr      error
	now            time.Time
	pushes         []sentPush

	friendCalls   int
	anchorCalls   int
	recordCalls   int
	settingsCalls int
	tzCalls       int
}

func (f *fake) dispatcher() Dispatcher {
	return Dispatcher{
		LoadSettings: func(string) (types.NotificationSettings, error) {
			f.settingsCalls++
			return f.settings, f.settingsErr
		},
		LoadUserTimezone: func(string) string {
			f.tzCalls++
			return f.tz
		},
		LoadFriendOverride: func(string, string) (*types.NotificationOverride, error) {
			f.friendCalls++
			return f.friendOverride, f.overrideErr
		},
		LoadAnchoredOverride: func(string, string) (*types.NotificationOverride, error) {
			f.anchorCalls++
			return f.anchorOverride, f.overrideErr
		},
		RecordDelivery: func(_, _, _, _ string) (bool, error) {
			f.recordCalls++
			return f.recordInserted, f.recordErr
		},
		Send: func(userID, notificationType, title, body string, data map[string]any) {
			f.pushes = append(f.pushes, sentPush{userID, notificationType, title, body, data})
		},
		Now: func() time.Time { return f.now },
	}
}

func baseSettingsForDispatch() types.NotificationSettings {
	return types.NotificationSettings{
		UserID:                  "user-1",
		PushEnabled:             true,
		EmailEnabled:            true,
		MyGoalsPrefs:            types.DefaultMyGoalsPrefs(),
		FAndAPrefs:              types.DefaultFAndAPrefs(),
		FAndAEnabled:            true,
		ApplyToAllFriends:       true,
		DigestFrequency:         types.DigestFrequencyNone,
		DigestDeliveryHour:      8,
		DigestDeliveryDayOfWeek: 1,
		QuietHoursStart:         "22:00",
		QuietHoursEnd:           "08:00",
	}
}

var dispatchTestNow = time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)

// =============================================================================
// Cases
// =============================================================================

func TestDispatch_HappyPath_FriendRequest(t *testing.T) {
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
		FriendID:         "friend-2",
		Title:            "New Friend Request",
		Body:             "Pat wants to connect",
		Data:             map[string]any{"friendship_id": "friendship-42"},
	})

	if !decision.Send {
		t.Fatalf("decision.Send = false, want true (reason=%q)", decision.Reason)
	}
	if len(f.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1", len(f.pushes))
	}
	if f.pushes[0].notificationType != "friend_request" {
		t.Errorf("notificationType = %q, want friend_request", f.pushes[0].notificationType)
	}
	if f.recordCalls != 1 {
		t.Errorf("recordCalls = %d, want 1", f.recordCalls)
	}
	// ApplyToAllFriends=true → override loaders MUST NOT be called.
	if f.friendCalls != 0 || f.anchorCalls != 0 {
		t.Errorf("override loaders called when apply_to_all=true (friend=%d, anchor=%d)", f.friendCalls, f.anchorCalls)
	}
}

func TestDispatch_SkipsWhenDeliveryAlreadyLogged(t *testing.T) {
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: false, // dedup says already delivered
		now:            dispatchTestNow,
	}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
		FriendID:         "friend-2",
	})

	if len(f.pushes) != 0 {
		t.Errorf("push fired despite dedup says already delivered: %+v", f.pushes)
	}
	if f.recordCalls != 1 {
		t.Errorf("recordCalls = %d, want 1", f.recordCalls)
	}
}

func TestDispatch_SkipsOnDecisionReject(t *testing.T) {
	s := baseSettingsForDispatch()
	s.FAndAEnabled = false // disables every F&A notification
	f := &fake{settings: s, now: dispatchTestNow}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
	})

	if decision.Reason != ReasonFAndADisabled {
		t.Errorf("reason = %q, want %q", decision.Reason, ReasonFAndADisabled)
	}
	if f.recordCalls != 0 {
		t.Error("RecordDelivery should not be called when decision is skip")
	}
	if len(f.pushes) != 0 {
		t.Errorf("push fired despite skip decision: %+v", f.pushes)
	}
}

func TestDispatch_QuietHoursSnoozeDropsPushInV1(t *testing.T) {
	// Step 9 will defer; in Step 5 we just don't send.
	s := baseSettingsForDispatch()
	s.QuietHoursEnabled = true
	f := &fake{
		settings: s,
		tz:       "UTC",
		// 23:00 UTC is inside 22:00–08:00 UTC quiet hours
		now: time.Date(2026, 5, 18, 23, 0, 0, 0, time.UTC),
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request", // default delivery: immediate
		DedupKey:         "friendship-42",
	})

	if decision.Reason != ReasonQuietHours {
		t.Errorf("reason = %q, want %q", decision.Reason, ReasonQuietHours)
	}
	if decision.SnoozeUntil == nil {
		t.Error("SnoozeUntil should be set during quiet hours")
	}
	if len(f.pushes) != 0 {
		t.Errorf("push fired during quiet hours: %+v", f.pushes)
	}
	if f.recordCalls != 0 {
		t.Error("delivery log claim should not happen on snooze")
	}
}

func TestDispatch_SettingsErrorAborts(t *testing.T) {
	f := &fake{settingsErr: errors.New("db down"), now: dispatchTestNow}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
	})

	if decision.Reason != "settings_load_error" {
		t.Errorf("reason = %q, want settings_load_error", decision.Reason)
	}
	if len(f.pushes) != 0 || f.recordCalls != 0 {
		t.Error("nothing should happen if settings can't load")
	}
}

func TestDispatch_AppliesFriendOverrideWhenApplyToAllOff(t *testing.T) {
	// User has flipped "apply same to all" off and muted a specific friend.
	// We should look up the friend override and skip per-target mute.
	s := baseSettingsForDispatch()
	s.ApplyToAllFriends = false
	muteUntil := dispatchTestNow.Add(1 * time.Hour)
	f := &fake{
		settings: s,
		friendOverride: &types.NotificationOverride{
			FriendID:  strPtrDispatch("friend-2"),
			MuteUntil: &muteUntil,
		},
		now: dispatchTestNow,
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		FriendID:         "friend-2",
		DedupKey:         "friendship-42",
	})

	if f.friendCalls != 1 {
		t.Errorf("friendCalls = %d, want 1", f.friendCalls)
	}
	if decision.Reason != ReasonTargetMute {
		t.Errorf("reason = %q, want %q", decision.Reason, ReasonTargetMute)
	}
	if len(f.pushes) != 0 {
		t.Errorf("push fired despite target mute: %+v", f.pushes)
	}
}

func TestDispatch_AnchoredGoalOverrideTakesPriorityWhenSet(t *testing.T) {
	// When both FriendID and AnchoredRoadmapID could be passed, the resolver
	// branch in Dispatch picks the anchored-goal override (it's checked first).
	s := baseSettingsForDispatch()
	s.ApplyToAllFriends = false
	f := &fake{
		settings:       s,
		anchorOverride: nil, // no override row exists for this roadmap
		now:            dispatchTestNow,
	}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:            "user-1",
		Category:          CategoryFAndA,
		NotificationType:  "anchor_request",
		AnchoredRoadmapID: "roadmap-99",
		FriendID:          "friend-2", // present but anchored takes priority
		DedupKey:          "request-7",
	})

	if f.anchorCalls != 1 {
		t.Errorf("anchorCalls = %d, want 1", f.anchorCalls)
	}
	if f.friendCalls != 0 {
		t.Errorf("friendCalls = %d, want 0 (anchored should be checked first)", f.friendCalls)
	}
}

func TestDispatch_OverrideLoadErrorFallsThroughToDefault(t *testing.T) {
	// If the override query errors, we shouldn't block the notification —
	// fall back to the user's F&A default.
	s := baseSettingsForDispatch()
	s.ApplyToAllFriends = false
	f := &fake{
		settings:       s,
		overrideErr:    errors.New("db blip"),
		recordInserted: true,
		now:            dispatchTestNow,
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		FriendID:         "friend-2",
		DedupKey:         "friendship-42",
	})

	if !decision.Send {
		t.Errorf("decision.Send = false, want true (reason=%q)", decision.Reason)
	}
	if len(f.pushes) != 1 {
		t.Errorf("expected push to fire despite override load error, got %d", len(f.pushes))
	}
}

func TestDispatch_MyGoalsCategoryNeverLooksUpOverrides(t *testing.T) {
	s := baseSettingsForDispatch()
	s.ApplyToAllFriends = false // would normally trigger override lookup
	f := &fake{settings: s, recordInserted: true, now: dispatchTestNow}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryMyGoals,
		NotificationType: "missed_task",
		DedupKey:         "task-1",
	})

	if f.friendCalls != 0 || f.anchorCalls != 0 {
		t.Errorf("override loaders called for MyGoals (friend=%d, anchor=%d)", f.friendCalls, f.anchorCalls)
	}
}

// Helper to avoid colliding with the strPtr in resolver_test.go.
func strPtrDispatch(s string) *string { return &s }
