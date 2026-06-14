//go:build integration

package integration

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/notifications"
	"dr/server/types"
)

// capturedPush records a push the fake sender "delivered".
type capturedPush struct {
	userID, notificationType, title, body string
}

// realDispatcher wires a Dispatcher to the real database.* functions (against
// the container) but captures pushes instead of hitting Expo. `now` is the
// clock the resolver sees; `pushes` collects what would have been sent.
func realDispatcher(now time.Time, pushes *[]capturedPush) notifications.Dispatcher {
	return notifications.Dispatcher{
		LoadSettings: database.EnsureNotificationSettings,
		LoadUserTimezone: func(userID string) string {
			u, err := database.GetUserByID(userID)
			if err != nil {
				return ""
			}
			return u.Timezone
		},
		LoadFriendOverride:   database.GetFriendOverride,
		LoadAnchoredOverride: database.GetAnchoredGoalOverride,
		RecordDelivery:       database.RecordDelivery,
		QueuePending:         database.UpsertPendingNotification,
		QueueDigest:          database.UpsertDigestItem,
		CountRecentPushes:    database.CountRecentPushes,
		Send: func(userID, notificationType, title, body string, _ map[string]any) {
			*pushes = append(*pushes, capturedPush{userID, notificationType, title, body})
		},
		Now: func() time.Time { return now },
	}
}

func prefsMap(t *testing.T, v any) map[string]types.NotificationTypePref {
	t.Helper()
	var b []byte
	switch x := v.(type) {
	case string:
		b = []byte(x)
	default:
		var err error
		if b, err = json.Marshal(x); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	m := map[string]types.NotificationTypePref{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal prefs: %v", err)
	}
	return m
}

// The auto-create trigger fires on user insert and writes the SQL default
// blob. This proves, against real Postgres, that the trigger runs AND that its
// JSONB defaults match the Go defaults (including the carry-over keys added by
// the latest migration).
func TestIntegration_UserInsertTriggersDefaultSettings(t *testing.T) {
	uid := createUser(t, "UTC")

	var mgJSON, faJSON string
	err := testDB.Raw(
		`select my_goals_prefs::text, f_and_a_prefs::text
		 from notification_settings where user_id = ?`, uid,
	).Row().Scan(&mgJSON, &faJSON)
	if err != nil {
		t.Fatalf("trigger did not create a settings row: %v", err)
	}

	if got, want := prefsMap(t, mgJSON), prefsMap(t, types.DefaultMyGoalsPrefs()); !reflect.DeepEqual(got, want) {
		t.Errorf("my_goals_prefs trigger default != Go default\n got: %+v\nwant: %+v", got, want)
	}
	if got, want := prefsMap(t, faJSON), prefsMap(t, types.DefaultFAndAPrefs()); !reflect.DeepEqual(got, want) {
		t.Errorf("f_and_a_prefs trigger default != Go default\n got: %+v\nwant: %+v", got, want)
	}
}

// EnsureNotificationSettings reads (or creates) a settings row and scans the
// JSONB into the typed structs — exercising the Scanner + merge-on-read path
// against real Postgres. The carry-over keys must come back enabled.
func TestIntegration_EnsureNotificationSettingsReturnsDefaults(t *testing.T) {
	uid := createUser(t, "UTC")

	s, err := database.EnsureNotificationSettings(uid)
	if err != nil {
		t.Fatalf("EnsureNotificationSettings: %v", err)
	}
	if !s.FAndAEnabled || !s.ApplyToAllFriends {
		t.Errorf("FAndAEnabled=%v ApplyToAllFriends=%v, want both true", s.FAndAEnabled, s.ApplyToAllFriends)
	}
	if !s.MyGoalsPrefs.AgentUpdates.Enabled {
		t.Error("agent_updates should be enabled by default")
	}
	if !s.FAndAPrefs.Nudges.Enabled || !s.FAndAPrefs.Messages.Enabled {
		t.Errorf("nudges=%v messages=%v, want both enabled", s.FAndAPrefs.Nudges.Enabled, s.FAndAPrefs.Messages.Enabled)
	}
}

// RecordDelivery is the idempotency claim: first insert wins, a duplicate
// (same user/type/dedup/channel) is a no-op. This is the entire dedup
// guarantee, validated against the real UNIQUE constraint.
func TestIntegration_RecordDeliveryIdempotent(t *testing.T) {
	uid := createUser(t, "UTC")

	first, err := database.RecordDelivery(uid, "friend_request", "dedup-1", types.NotificationChannelPush)
	if err != nil {
		t.Fatalf("RecordDelivery #1: %v", err)
	}
	if !first {
		t.Fatal("first RecordDelivery should report inserted=true")
	}
	second, err := database.RecordDelivery(uid, "friend_request", "dedup-1", types.NotificationChannelPush)
	if err != nil {
		t.Fatalf("RecordDelivery #2: %v", err)
	}
	if second {
		t.Error("duplicate RecordDelivery should report inserted=false")
	}
}

// Full Dispatch path against real Postgres: an immediate F&A push with no
// quiet hours sends once and writes one delivery-log row; a duplicate trigger
// with the same dedup key does not double-send.
func TestIntegration_DispatchImmediateSendsOnceAndLogs(t *testing.T) {
	uid := createUser(t, "UTC")
	var pushes []capturedPush
	// Mid-afternoon UTC: outside the default 22:00–08:00 quiet window (which is
	// off by default anyway).
	d := realDispatcher(time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC), &pushes)

	dec := d.Dispatch(notifications.DispatchOptions{
		UserID:           uid,
		Category:         notifications.CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "fr-1",
		FriendID:         "00000000-0000-0000-0000-000000000001",
		Title:            "New Friend Request",
		Body:             "Pat wants to connect",
	})
	if !dec.Send {
		t.Fatalf("expected send, got reason %q", dec.Reason)
	}
	if len(pushes) != 1 {
		t.Fatalf("pushes = %d, want 1", len(pushes))
	}
	if n := countRows(t, `select count(*) from notification_delivery_log where user_id = ? and dedup_key = 'fr-1'`, uid); n != 1 {
		t.Errorf("delivery_log rows = %d, want 1", n)
	}

	// Duplicate trigger, same dedup key → no second push.
	d.Dispatch(notifications.DispatchOptions{
		UserID:           uid,
		Category:         notifications.CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "fr-1",
		FriendID:         "00000000-0000-0000-0000-000000000001",
		Title:            "New Friend Request",
		Body:             "Pat wants to connect",
	})
	if len(pushes) != 1 {
		t.Errorf("after duplicate, pushes = %d, want 1 (idempotent)", len(pushes))
	}
}

// A digest-delivery type (missed_task defaults to digest) must buffer into
// digest_items rather than pushing, via the real UpsertDigestItem.
func TestIntegration_DispatchDigestTypeQueuesItemNotPush(t *testing.T) {
	uid := createUser(t, "UTC")
	var pushes []capturedPush
	d := realDispatcher(time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC), &pushes)

	d.Dispatch(notifications.DispatchOptions{
		UserID:           uid,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "missed_task",
		DedupKey:         "task-1",
		Title:            "Missed task",
		Body:             "You missed a task",
	})

	if len(pushes) != 0 {
		t.Errorf("pushes = %d, want 0 (digest delivery must not push)", len(pushes))
	}
	items, err := database.ListDigestItemsForUser(uid)
	if err != nil {
		t.Fatalf("ListDigestItemsForUser: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("digest items = %d, want 1", len(items))
	}
}

// During quiet hours an immediate push is deferred: no push, no delivery-log
// claim, but a pending_notifications row written via the real UpsertPendingNotification.
func TestIntegration_QuietHoursDefersToPending(t *testing.T) {
	uid := createUser(t, "UTC")
	if err := testDB.Exec(
		`update notification_settings
		   set quiet_hours_enabled = true, quiet_hours_start = '22:00', quiet_hours_end = '08:00'
		 where user_id = ?`, uid).Error; err != nil {
		t.Fatalf("enable quiet hours: %v", err)
	}

	var pushes []capturedPush
	// 23:00 UTC is inside the 22:00–08:00 window.
	d := realDispatcher(time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC), &pushes)

	dec := d.Dispatch(notifications.DispatchOptions{
		UserID:           uid,
		Category:         notifications.CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         "fr-q",
		FriendID:         "00000000-0000-0000-0000-000000000001",
		Title:            "New Friend Request",
		Body:             "Pat wants to connect",
	})
	if dec.Reason != notifications.ReasonQuietHours {
		t.Errorf("reason = %q, want %q", dec.Reason, notifications.ReasonQuietHours)
	}
	if len(pushes) != 0 {
		t.Errorf("pushes = %d, want 0 (quiet hours)", len(pushes))
	}
	if n := countRows(t, `select count(*) from notification_delivery_log where user_id = ? and dedup_key = 'fr-q'`, uid); n != 0 {
		t.Errorf("delivery_log rows = %d, want 0 (deferred, not delivered)", n)
	}
	if n := countRows(t, `select count(*) from pending_notifications where user_id = ? and dedup_key = 'fr-q'`, uid); n != 1 {
		t.Errorf("pending rows = %d, want 1", n)
	}
}

// The digest queue's real upsert dedups on (user, type, dedup_key); list/delete
// round-trip against real Postgres.
func TestIntegration_DigestQueueUpsertDedupesAndDeletes(t *testing.T) {
	uid := createUser(t, "UTC")
	item := types.DigestItem{
		UserID:           uid,
		NotificationType: "missed_task",
		DedupKey:         "task-9",
		Title:            "Missed task",
		Body:             "body",
	}
	if err := database.UpsertDigestItem(item); err != nil {
		t.Fatalf("UpsertDigestItem #1: %v", err)
	}
	if err := database.UpsertDigestItem(item); err != nil {
		t.Fatalf("UpsertDigestItem #2: %v", err)
	}
	items, err := database.ListDigestItemsForUser(uid)
	if err != nil {
		t.Fatalf("ListDigestItemsForUser: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("digest items = %d, want 1 (ON CONFLICT DO NOTHING)", len(items))
	}
	n, err := database.DeleteDigestItemsForUser(uid)
	if err != nil {
		t.Fatalf("DeleteDigestItemsForUser: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if remaining := countRows(t, `select count(*) from digest_items where user_id = ?`, uid); remaining != 0 {
		t.Errorf("remaining digest items = %d, want 0", remaining)
	}
}

// Re-queuing a pending notification with the same key UPSERTs (refreshes
// scheduled_for, bumps attempts) instead of inserting a duplicate.
func TestIntegration_PendingUpsertRefreshesNotDuplicates(t *testing.T) {
	uid := createUser(t, "UTC")
	base := types.PendingNotification{
		UserID:           uid,
		NotificationType: "friend_request",
		DedupKey:         "fr-p",
		Channel:          types.NotificationChannelPush,
		Category:         string(notifications.CategoryFAndA),
		Title:            "t",
		Body:             "b",
		ScheduledFor:     time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC),
	}
	if err := database.UpsertPendingNotification(base); err != nil {
		t.Fatalf("UpsertPendingNotification #1: %v", err)
	}
	base.ScheduledFor = time.Date(2026, 6, 16, 8, 0, 0, 0, time.UTC)
	if err := database.UpsertPendingNotification(base); err != nil {
		t.Fatalf("UpsertPendingNotification #2: %v", err)
	}

	if n := countRows(t, `select count(*) from pending_notifications where user_id = ? and dedup_key = 'fr-p'`, uid); n != 1 {
		t.Fatalf("pending rows = %d, want 1 (upsert, not duplicate)", n)
	}
	if attempts := countRows(t, `select attempts from pending_notifications where user_id = ? and dedup_key = 'fr-p'`, uid); attempts != 1 {
		t.Errorf("attempts = %d, want 1 (incremented once on conflict)", attempts)
	}
}
