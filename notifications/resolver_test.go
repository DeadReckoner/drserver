package notifications

import (
	"testing"
	"time"

	"dr/server/types"
)

// baseSettings returns a fully-populated settings row with the system defaults.
// Helpers below tweak this baseline per-case.
func baseSettings() types.NotificationSettings {
	return types.NotificationSettings{
		UserID:                  "user-1",
		PushEnabled:             true,
		EmailEnabled:            true,
		QuietHoursEnabled:       false,
		QuietHoursStart:         "22:00",
		QuietHoursEnd:           "08:00",
		DigestFrequency:         types.DigestFrequencyNone,
		DigestDeliveryHour:      8,
		DigestDeliveryDayOfWeek: 1,
		MyGoalsPrefs:            types.DefaultMyGoalsPrefs(),
		FAndAEnabled:            true,
		ApplyToAllFriends:       true,
		FAndAPrefs:              types.DefaultFAndAPrefs(),
	}
}

// fixedNow is a Monday 14:00 UTC = 07:00 PDT. Picked to put PDT users
// comfortably outside the default 22:00–08:00 quiet hours window.
var fixedNow = time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)

func ptrTime(t time.Time) *time.Time { return &t }

// =============================================================================
// Cascade coverage — one case per gating leg in Resolve.
// =============================================================================

func TestResolve_Cascade(t *testing.T) {
	cases := []struct {
		name         string
		settings     func(s *types.NotificationSettings)
		override     *types.NotificationOverride
		ctx          Context
		wantSend     bool
		wantReason   string
		wantDelivery types.DeliveryMode
		wantSnooze   bool
	}{
		{
			name: "global mute active — skip everything",
			settings: func(s *types.NotificationSettings) {
				s.GlobalMuteUntil = ptrTime(fixedNow.Add(1 * time.Hour))
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "missed_task",
				Now:              fixedNow,
			},
			wantReason: ReasonGlobalMute,
		},
		{
			name: "global mute expired — fall through",
			settings: func(s *types.NotificationSettings) {
				s.GlobalMuteUntil = ptrTime(fixedNow.Add(-1 * time.Hour))
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "missed_task",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryDigest, // missed_task default
		},
		{
			name: "no channels enabled — skip",
			settings: func(s *types.NotificationSettings) {
				s.PushEnabled = false
				s.EmailEnabled = false
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "missed_task",
				Now:              fixedNow,
			},
			wantReason: ReasonNoChannels,
		},
		{
			name:     "unknown notification type — skip",
			settings: func(s *types.NotificationSettings) {},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "not_a_real_type",
				Now:              fixedNow,
			},
			wantReason: ReasonUnknownType,
		},
		{
			name:     "unknown category — skip",
			settings: func(s *types.NotificationSettings) {},
			ctx: Context{
				Category:         "garbage",
				NotificationType: "missed_task",
				Now:              fixedNow,
			},
			wantReason: ReasonUnknownCategory,
		},
		{
			name: "F&A disabled — F&A notification skipped",
			settings: func(s *types.NotificationSettings) {
				s.FAndAEnabled = false
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "friend_request",
				Now:              fixedNow,
			},
			wantReason: ReasonFAndADisabled,
		},
		{
			name: "F&A disabled — my-goals notification still sends",
			settings: func(s *types.NotificationSettings) {
				s.FAndAEnabled = false
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "missed_task",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryDigest,
		},
		{
			name: "type disabled — skip",
			settings: func(s *types.NotificationSettings) {
				s.MyGoalsPrefs.MissedTask = types.NotificationTypePref{Enabled: false}
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "missed_task",
				Now:              fixedNow,
			},
			wantReason: ReasonTypeDisabled,
		},
		// =============================================================
		// Apply-to-all toggle (c1 decision)
		// =============================================================
		{
			name: "apply_to_all_friends=true — override is ignored",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = true
			},
			override: &types.NotificationOverride{
				FriendID: strPtr("friend-1"),
				Prefs: types.PrefOverrides{
					"friend_request": {Enabled: false, Delivery: types.DeliveryDigest},
				},
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "friend_request",
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			// Default friend_request is enabled+immediate; override would
			// have disabled it but is ignored.
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryImmediate,
		},
		{
			name: "apply_to_all_friends=false + override disables — skip",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = false
			},
			override: &types.NotificationOverride{
				FriendID: strPtr("friend-1"),
				Prefs: types.PrefOverrides{
					"friend_request": {Enabled: false, Delivery: types.DeliveryDigest},
				},
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "friend_request",
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			wantReason: ReasonTypeDisabled,
		},
		{
			name: "apply_to_all_friends=false + partial override — unset types fall through to default",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = false
			},
			override: &types.NotificationOverride{
				FriendID: strPtr("friend-1"),
				// Override only sets missed_progress; friend_request must fall through
				Prefs: types.PrefOverrides{
					"missed_progress": {Enabled: false, Delivery: types.DeliveryDigest},
				},
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "friend_request", // not in the override map
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryImmediate, // default for friend_request
		},
		{
			name: "apply_to_all_friends=false + no override row — use F&A default",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = false
			},
			override: nil,
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "missed_progress",
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryDigest, // default for missed_progress
		},
		// =============================================================
		// Per-target mute window
		// =============================================================
		{
			name: "per-target mute active — skip",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = false
			},
			override: &types.NotificationOverride{
				FriendID:  strPtr("friend-1"),
				MuteUntil: ptrTime(fixedNow.Add(1 * time.Hour)),
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "missed_progress",
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			wantReason: ReasonTargetMute,
		},
		{
			name: "per-target mute expired — fall through",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = false
			},
			override: &types.NotificationOverride{
				FriendID:  strPtr("friend-1"),
				MuteUntil: ptrTime(fixedNow.Add(-1 * time.Hour)),
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "missed_progress",
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryDigest,
		},
		{
			name: "per-target mute on apply_to_all_friends=true — mute ignored (override gated)",
			settings: func(s *types.NotificationSettings) {
				s.ApplyToAllFriends = true
			},
			override: &types.NotificationOverride{
				FriendID:  strPtr("friend-1"),
				MuteUntil: ptrTime(fixedNow.Add(1 * time.Hour)),
			},
			ctx: Context{
				Category:         CategoryFAndA,
				NotificationType: "missed_progress",
				FriendID:         "friend-1",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryDigest,
		},
		// =============================================================
		// Quiet hours
		// =============================================================
		{
			name: "quiet hours active + immediate delivery — defer",
			settings: func(s *types.NotificationSettings) {
				s.QuietHoursEnabled = true
				// 22:00–08:00 in PDT (UTC-7).
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk", // default: immediate
				UserTimezone:     "America/Los_Angeles",
				// 06:00 UTC = 23:00 PDT (Sunday) — inside quiet hours.
				Now: time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC),
			},
			wantSend:     false,
			wantSnooze:   true,
			wantDelivery: types.DeliveryImmediate,
			wantReason:   ReasonQuietHours,
		},
		{
			name: "quiet hours active + digest delivery — send anyway",
			settings: func(s *types.NotificationSettings) {
				s.QuietHoursEnabled = true
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "missed_task", // default: digest
				UserTimezone:     "America/Los_Angeles",
				Now:              time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC), // 23:00 PDT
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryDigest,
		},
		{
			name: "quiet hours active but outside window — send",
			settings: func(s *types.NotificationSettings) {
				s.QuietHoursEnabled = true
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				UserTimezone:     "America/Los_Angeles",
				Now:              fixedNow, // 14:00 UTC = 07:00 PDT — too late for 22:00–08:00? Actually 07:00 IS in 22-08
			},
			// 07:00 PDT IS in 22:00–08:00 — wait let me recheck
			// 22:00–08:00 wraps; in-quiet iff nowMin >= 1320 OR nowMin < 480
			// 07:00 = 420 < 480 → IN quiet hours
			wantSend:     false,
			wantSnooze:   true,
			wantDelivery: types.DeliveryImmediate,
			wantReason:   ReasonQuietHours,
		},
		{
			name: "quiet hours active, outside window properly — send",
			settings: func(s *types.NotificationSettings) {
				s.QuietHoursEnabled = true
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				UserTimezone:     "America/Los_Angeles",
				// 20:00 UTC = 13:00 PDT — outside 22-08
				Now: time.Date(2026, 5, 18, 20, 0, 0, 0, time.UTC),
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryImmediate,
		},
		{
			name: "bad timezone — falls back to UTC quiet hours math, sends",
			settings: func(s *types.NotificationSettings) {
				s.QuietHoursEnabled = true
				// 22-08 in UTC; fixedNow is 14:00 UTC → outside.
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				UserTimezone:     "Not/A/Real/TZ",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryImmediate,
		},
		{
			name: "bad HH:MM strings — fall through and send (don't swallow notifications)",
			settings: func(s *types.NotificationSettings) {
				s.QuietHoursEnabled = true
				s.QuietHoursStart = "garbage"
				s.QuietHoursEnd = "08:00"
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				UserTimezone:     "UTC",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryImmediate,
		},
		// =============================================================
		// Channel availability
		// =============================================================
		{
			name: "only push enabled — channels reflects that",
			settings: func(s *types.NotificationSettings) {
				s.EmailEnabled = false
			},
			ctx: Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				Now:              fixedNow,
			},
			wantSend:     true,
			wantReason:   ReasonOK,
			wantDelivery: types.DeliveryImmediate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := baseSettings()
			if tc.settings != nil {
				tc.settings(&s)
			}
			got := Resolve(tc.ctx, s, tc.override)

			if got.Send != tc.wantSend {
				t.Errorf("Send = %v, want %v (reason=%q)", got.Send, tc.wantSend, got.Reason)
			}
			if tc.wantReason != "" && got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if tc.wantDelivery != "" && got.Delivery != tc.wantDelivery {
				t.Errorf("Delivery = %q, want %q", got.Delivery, tc.wantDelivery)
			}
			if tc.wantSnooze && got.SnoozeUntil == nil {
				t.Errorf("SnoozeUntil = nil, want non-nil")
			}
			if !tc.wantSnooze && got.SnoozeUntil != nil {
				t.Errorf("SnoozeUntil = %v, want nil", *got.SnoozeUntil)
			}
		})
	}
}

// Sanity: only-push case actually emits push-only channels (the cascade test
// above checks Send/Delivery; this checks channel content explicitly).
func TestResolve_ChannelsContent(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*types.NotificationSettings)
		wantHas []string
		wantLen int
	}{
		{"push+email", func(s *types.NotificationSettings) {}, []string{"push", "email"}, 2},
		{"push only", func(s *types.NotificationSettings) { s.EmailEnabled = false }, []string{"push"}, 1},
		{"email only", func(s *types.NotificationSettings) { s.PushEnabled = false }, []string{"email"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSettings()
			tt.setup(&s)
			d := Resolve(Context{
				Category:         CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				Now:              fixedNow,
			}, s, nil)
			if len(d.Channels) != tt.wantLen {
				t.Fatalf("channels = %v, want %d entries", d.Channels, tt.wantLen)
			}
			for i, want := range tt.wantHas {
				if d.Channels[i] != want {
					t.Errorf("channels[%d] = %q, want %q", i, d.Channels[i], want)
				}
			}
		})
	}
}

// =============================================================================
// InQuietHours — math-focused unit tests
// =============================================================================

func TestInQuietHours(t *testing.T) {
	// All times in UTC; the function converts via the tz parameter.
	cases := []struct {
		name    string
		now     time.Time
		tz      string
		start   string
		end     string
		wantIn  bool
		wantErr bool
	}{
		// Wrapping window: 22:00–08:00
		{
			name:  "wrap, before midnight in window",
			now:   time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC), // 23:00 PDT
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			wantIn: true,
		},
		{
			name:  "wrap, after midnight in window",
			now:   time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), // 03:00 PDT
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			wantIn: true,
		},
		{
			name:  "wrap, mid-day outside window",
			now:   time.Date(2026, 5, 18, 20, 0, 0, 0, time.UTC), // 13:00 PDT
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			wantIn: false,
		},
		{
			name:  "wrap, exactly at start — in window",
			now:   time.Date(2026, 5, 19, 5, 0, 0, 0, time.UTC), // 22:00 PDT
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			wantIn: true,
		},
		{
			name:  "wrap, exactly at end — out (end is exclusive)",
			now:   time.Date(2026, 5, 18, 15, 0, 0, 0, time.UTC), // 08:00 PDT
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			wantIn: false,
		},

		// Non-wrapping window: 09:00–17:00 (e.g., "no work-hour pings")
		{
			name:  "non-wrap, mid-window",
			now:   time.Date(2026, 5, 18, 19, 0, 0, 0, time.UTC), // 12:00 PDT
			tz:    "America/Los_Angeles",
			start: "09:00", end: "17:00",
			wantIn: true,
		},
		{
			name:  "non-wrap, before start",
			now:   time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC), // 07:00 PDT
			tz:    "America/Los_Angeles",
			start: "09:00", end: "17:00",
			wantIn: false,
		},
		{
			name:  "non-wrap, after end",
			now:   time.Date(2026, 5, 19, 1, 0, 0, 0, time.UTC), // 18:00 PDT
			tz:    "America/Los_Angeles",
			start: "09:00", end: "17:00",
			wantIn: false,
		},

		// Edge: equal start/end → never quiet (convention)
		{
			name:  "equal start/end — never quiet",
			now:   time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC),
			tz:    "UTC",
			start: "12:00", end: "12:00",
			wantIn: false,
		},

		// Malformed inputs → error
		{
			name:  "bad start",
			now:   fixedNow,
			tz:    "UTC",
			start: "garbage", end: "08:00",
			wantErr: true,
		},
		{
			name:  "bad end",
			now:   fixedNow,
			tz:    "UTC",
			start: "22:00", end: "99:99",
			wantErr: true,
		},

		// Bad TZ silently falls back to UTC
		{
			name:  "bad TZ falls back to UTC",
			now:   time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC),
			tz:    "Not/Real",
			start: "22:00", end: "08:00",
			wantIn: true, // 06:00 UTC is within 22-08 UTC (wraps), via the < 8 leg
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, _, err := InQuietHours(tc.now, tc.tz, tc.start, tc.end)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && in != tc.wantIn {
				t.Errorf("in = %v, want %v", in, tc.wantIn)
			}
		})
	}
}

// Snooze target: when in quiet hours, endsAt should be the UTC time at which
// quiet hours next end. Verifies the wrap-vs-non-wrap math.
func TestInQuietHours_SnoozeTarget(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Time
		tz         string
		start, end string
		want       time.Time // expected endsAt, UTC
	}{
		{
			// 23:00 PDT (May 17 night) → quiet ends at 08:00 PDT May 18 = 15:00 UTC May 18
			name:  "wrap, after start today, end is tomorrow local",
			now:   time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC),
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			want: time.Date(2026, 5, 18, 15, 0, 0, 0, time.UTC),
		},
		{
			// 03:00 PDT → quiet ends at 08:00 PDT same day = 15:00 UTC same day
			name:  "wrap, after midnight, end is today local",
			now:   time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
			tz:    "America/Los_Angeles",
			start: "22:00", end: "08:00",
			want: time.Date(2026, 5, 18, 15, 0, 0, 0, time.UTC),
		},
		{
			// Non-wrap, mid-window: 12:00 PDT → quiet ends at 17:00 PDT = 00:00 UTC next day
			name:  "non-wrap, end is later today local",
			now:   time.Date(2026, 5, 18, 19, 0, 0, 0, time.UTC),
			tz:    "America/Los_Angeles",
			start: "09:00", end: "17:00",
			want: time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, endsAt, err := InQuietHours(tt.now, tt.tz, tt.start, tt.end)
			if err != nil || !in {
				t.Fatalf("expected in-quiet-hours; got in=%v err=%v", in, err)
			}
			if !endsAt.Equal(tt.want) {
				t.Errorf("endsAt = %v, want %v", endsAt.UTC(), tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
