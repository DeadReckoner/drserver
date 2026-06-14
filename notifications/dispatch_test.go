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
	queued         []types.PendingNotification
	queueErr       error
	digested       []types.DigestItem
	digestErr      error
	pushCount      int64
	pushCountErr   error

	friendCalls   int
	anchorCalls   int
	recordCalls   int
	settingsCalls int
	tzCalls       int
	queueCalls    int
	digestCalls   int
	countCalls    int
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
		QueuePending: func(p types.PendingNotification) error {
			f.queueCalls++
			f.queued = append(f.queued, p)
			return f.queueErr
		},
		QueueDigest: func(item types.DigestItem) error {
			f.digestCalls++
			f.digested = append(f.digested, item)
			return f.digestErr
		},
		CountRecentPushes: func(string, time.Time) (int64, error) {
			f.countCalls++
			return f.pushCount, f.pushCountErr
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

func TestDispatch_QuietHoursSnoozeQueuesPending(t *testing.T) {
	// Step 9: in quiet hours, defer via QueuePending instead of dropping.
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
		FriendID:         "friend-2",
		Title:            "New Friend Request",
		Body:             "Pat wants to connect",
		Data:             map[string]any{"friendship_id": "friendship-42"},
	})

	if decision.Reason != ReasonQuietHours {
		t.Errorf("reason = %q, want %q", decision.Reason, ReasonQuietHours)
	}
	if decision.SnoozeUntil == nil {
		t.Fatal("SnoozeUntil should be set during quiet hours")
	}
	if len(f.pushes) != 0 {
		t.Errorf("push fired during quiet hours: %+v", f.pushes)
	}
	if f.recordCalls != 0 {
		t.Error("delivery log claim should not happen on snooze")
	}
	if f.queueCalls != 1 {
		t.Fatalf("queue calls = %d, want 1 (snooze should defer to pending table)", f.queueCalls)
	}
	queued := f.queued[0]
	if queued.NotificationType != "friend_request" || queued.DedupKey != "friendship-42" {
		t.Errorf("queued row mismatch: %+v", queued)
	}
	if queued.Title != "New Friend Request" || queued.Body != "Pat wants to connect" {
		t.Errorf("queued row missing payload: %+v", queued)
	}
	if queued.Category != string(CategoryFAndA) {
		t.Errorf("queued category = %q, want %q", queued.Category, CategoryFAndA)
	}
	if queued.FriendID == nil || *queued.FriendID != "friend-2" {
		t.Errorf("queued FriendID = %v, want friend-2", queued.FriendID)
	}
	if !queued.ScheduledFor.Equal(*decision.SnoozeUntil) {
		t.Errorf("queued ScheduledFor = %v, want %v", queued.ScheduledFor, *decision.SnoozeUntil)
	}
}

func TestDispatch_PermanentSkipDoesNotQueue(t *testing.T) {
	// Step 9: only SnoozeUntil-bearing decisions defer. Permanent skips
	// (type disabled, muted, unknown) should NOT add anything to the
	// pending table — they're truly dropped.
	s := baseSettingsForDispatch()
	s.FAndAEnabled = false
	f := &fake{settings: s, now: dispatchTestNow}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
	})

	if f.queueCalls != 0 {
		t.Errorf("queueCalls = %d, want 0 (permanent skip should not defer)", f.queueCalls)
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

// =============================================================================
// Step 10: digest routing
// =============================================================================
// When the resolver decides Delivery=Digest, Dispatch must route to
// QueueDigest instead of pushing immediately. These tests guard that
// behavior, plus the safety fallback for partial deploys.

func TestDispatch_DigestDeliveryQueuesItemNotPush(t *testing.T) {
	// missed_task defaults to digest delivery. Dispatch should claim the
	// delivery slot AND queue a digest item, but NOT push.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
	}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryMyGoals,
		NotificationType: "missed_task", // default delivery: digest
		DedupKey:         "task-7",
		Title:            "Missed task",
		Body:             "Write the README",
		Data:             map[string]any{"task_id": "task-7"},
	})

	if len(f.pushes) != 0 {
		t.Errorf("push fired for digest-delivery notification: %+v", f.pushes)
	}
	if f.digestCalls != 1 {
		t.Fatalf("digestCalls = %d, want 1", f.digestCalls)
	}
	if f.recordCalls != 1 {
		t.Errorf("recordCalls = %d, want 1 (dedup slot must be claimed)", f.recordCalls)
	}
	item := f.digested[0]
	if item.NotificationType != "missed_task" || item.DedupKey != "task-7" {
		t.Errorf("digest item mismatch: %+v", item)
	}
	if item.Title != "Missed task" || item.Body != "Write the README" {
		t.Errorf("digest item missing payload: %+v", item)
	}
}

func TestDispatch_DigestDedupSkipsBothPushAndQueue(t *testing.T) {
	// recordInserted=false means the dedup slot is already claimed
	// (duplicate trigger). Neither push NOR digest queue should fire.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: false,
		now:            dispatchTestNow,
	}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryMyGoals,
		NotificationType: "missed_task",
		DedupKey:         "task-7",
	})

	if len(f.pushes) != 0 || f.digestCalls != 0 {
		t.Errorf("either push or digest fired on dedup hit: pushes=%d digestCalls=%d", len(f.pushes), f.digestCalls)
	}
}

func TestDispatch_DigestWithNilQueueFallsBackToImmediatePush(t *testing.T) {
	// If QueueDigest isn't wired (older test or partial deploy), don't
	// silently lose the notification — push it instead. The user sees
	// an out-of-cadence push but doesn't miss the event entirely.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
	}
	d := f.dispatcher()
	d.QueueDigest = nil // safety fallback

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryMyGoals,
		NotificationType: "missed_task",
		DedupKey:         "task-7",
		Title:            "Missed task",
		Body:             "Write the README",
	})

	if f.digestCalls != 0 {
		t.Errorf("digestCalls = %d on nil-QueueDigest path, want 0", f.digestCalls)
	}
	if len(f.pushes) != 1 {
		t.Fatalf("expected 1 push as fallback, got %d", len(f.pushes))
	}
}

// =============================================================================
// Frequency cap (Step 12) — at most maxPushesPerWindow immediate pushes per
// user per rolling window. Capped events are NOT claimed in the delivery log
// so scan-driven triggers self-heal on a later tick.
// =============================================================================

func TestDispatch_FrequencyCapBlocksImmediatePush(t *testing.T) {
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
		pushCount:      maxPushesPerWindow, // window is full
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request", // immediate delivery
		DedupKey:         "friendship-42",
	})

	if decision.Send {
		t.Fatal("decision.Send = true, want false (cap hit)")
	}
	if decision.Reason != ReasonFrequencyCap {
		t.Errorf("Reason = %q, want %q", decision.Reason, ReasonFrequencyCap)
	}
	if len(f.pushes) != 0 {
		t.Errorf("push fired despite cap: %+v", f.pushes)
	}
	// The dedup slot must NOT be burned — the event has to be able to
	// deliver on a later tick once the window has room.
	if f.recordCalls != 0 {
		t.Errorf("recordCalls = %d, want 0 (capped event must not claim the dedup slot)", f.recordCalls)
	}
}

func TestDispatch_FrequencyCapUnderLimitSends(t *testing.T) {
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
		pushCount:      maxPushesPerWindow - 1, // one slot left
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
	})

	if !decision.Send || len(f.pushes) != 1 {
		t.Errorf("Send=%v pushes=%d, want true/1 (under cap)", decision.Send, len(f.pushes))
	}
	if f.countCalls != 1 {
		t.Errorf("countCalls = %d, want 1", f.countCalls)
	}
}

func TestDispatch_FrequencyCapCountErrorFailsOpen(t *testing.T) {
	// A counting failure must not block notifications.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
		pushCountErr:   errors.New("db unavailable"),
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
	})

	if !decision.Send || len(f.pushes) != 1 {
		t.Errorf("Send=%v pushes=%d, want true/1 (fail open on count error)", decision.Send, len(f.pushes))
	}
}

func TestDispatch_FrequencyCapSkipsDigestDelivery(t *testing.T) {
	// Digest-mode notifications collapse into one email — the push cap
	// must not starve them.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
		pushCount:      maxPushesPerWindow + 5, // way over; irrelevant for digest
	}
	d := f.dispatcher()

	d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryMyGoals,
		NotificationType: "missed_task", // default delivery: digest
		DedupKey:         "task-7",
	})

	if f.countCalls != 0 {
		t.Errorf("countCalls = %d, want 0 (cap not consulted for digest delivery)", f.countCalls)
	}
	if f.digestCalls != 1 {
		t.Errorf("digestCalls = %d, want 1 (digest must still queue)", f.digestCalls)
	}
}

func TestDispatch_FrequencyCapNilCounterDisablesCap(t *testing.T) {
	// Older tests / partial deploys may not wire CountRecentPushes — the
	// cap silently disables rather than blocking sends.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
	}
	d := f.dispatcher()
	d.CountRecentPushes = nil

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
	})

	if !decision.Send || len(f.pushes) != 1 {
		t.Errorf("Send=%v pushes=%d, want true/1 (nil counter = cap disabled)", decision.Send, len(f.pushes))
	}
}

// =============================================================================
// message_new policy: cap-exempt + quiet-hours-drop
// =============================================================================

func TestDispatch_MessageNewExemptFromFrequencyCap(t *testing.T) {
	// An active chat can legitimately blow past the cap. message_new must
	// bypass it entirely — and must not even consult the counter.
	f := &fake{
		settings:       baseSettingsForDispatch(),
		recordInserted: true,
		now:            dispatchTestNow,
		pushCount:      maxPushesPerWindow + 50, // wildly over the cap
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "message_new",
		DedupKey:         "msg-99",
		FriendID:         "friend-2",
		Title:            "Pat",
		Body:             "hey!",
	})

	if !decision.Send || len(f.pushes) != 1 {
		t.Errorf("Send=%v pushes=%d, want true/1 (message_new is cap-exempt)", decision.Send, len(f.pushes))
	}
	if f.countCalls != 0 {
		t.Errorf("countCalls = %d, want 0 (cap-exempt types must not consult the counter)", f.countCalls)
	}
}

func TestDispatch_MessageNewDroppedInQuietHoursNotDeferred(t *testing.T) {
	// Messages are stale by morning, so quiet-hours messages drop rather than
	// pile up in the pending queue. Contrast TestDispatch_QuietHoursSnooze...
	// where friend_request DOES defer.
	s := baseSettingsForDispatch()
	s.QuietHoursEnabled = true
	f := &fake{
		settings: s,
		tz:       "UTC",
		now:      time.Date(2026, 5, 18, 23, 0, 0, 0, time.UTC), // inside 22:00–08:00
	}
	d := f.dispatcher()

	decision := d.Dispatch(DispatchOptions{
		UserID:           "user-1",
		Category:         CategoryFAndA,
		NotificationType: "message_new",
		DedupKey:         "msg-99",
		FriendID:         "friend-2",
		Title:            "Pat",
		Body:             "hey!",
	})

	if decision.Reason != ReasonQuietHours {
		t.Errorf("reason = %q, want %q", decision.Reason, ReasonQuietHours)
	}
	if len(f.pushes) != 0 {
		t.Errorf("push fired during quiet hours: %+v", f.pushes)
	}
	if f.queueCalls != 0 {
		t.Errorf("queueCalls = %d, want 0 (message_new drops in quiet hours, never defers)", f.queueCalls)
	}
}

// Helper to avoid colliding with the strPtr in resolver_test.go.
func strPtrDispatch(s string) *string { return &s }
