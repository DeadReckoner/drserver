package jobs

import (
	"errors"
	"testing"
	"time"

	"dr/server/notifications"
	"dr/server/types"
)

// deliverPendingNotificationsWith has three branches per row:
//   - decision.Send=true                       → delete row, count as "sent"
//   - decision.Send=false, SnoozeUntil != nil  → leave row (Dispatch UPSERTed it), count as "re-snoozed"
//   - decision.Send=false, SnoozeUntil == nil  → delete row, count as "cleaned up"
//
// Tests below exercise each branch via a fake listDue + deleteRow + dispatch.

func TestDeliverPendingNotifications_SendDeletesRow(t *testing.T) {
	row := types.PendingNotification{
		ID:               "pending-1",
		UserID:           "user-1",
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
		Channel:          "push",
		Category:         string(notifications.CategoryFAndA),
		Title:            "New Friend Request",
		Body:             "Pat wants to connect",
		Data:             map[string]any{"friendship_id": "friendship-42"},
		ScheduledFor:     time.Now().Add(-1 * time.Minute),
	}

	deleted := map[string]bool{}
	dispatch := func(opts notifications.DispatchOptions) notifications.Decision {
		// Verify the row's payload was reconstructed faithfully.
		if opts.NotificationType != row.NotificationType {
			t.Errorf("opts.NotificationType = %q, want %q", opts.NotificationType, row.NotificationType)
		}
		if opts.Title != row.Title || opts.Body != row.Body {
			t.Errorf("payload not carried through: title=%q body=%q", opts.Title, opts.Body)
		}
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	}
	listDue := func(_ int) ([]types.PendingNotification, error) {
		return []types.PendingNotification{row}, nil
	}
	deleteRow := func(id string) error {
		deleted[id] = true
		return nil
	}

	deliverPendingNotificationsWith(nil, dispatch, listDue, deleteRow)()

	if !deleted["pending-1"] {
		t.Error("expected pending-1 to be deleted after successful send")
	}
}

func TestDeliverPendingNotifications_ResnoozeLeavesRow(t *testing.T) {
	// User is STILL in quiet hours when the cron retries — resolver
	// returns SnoozeUntil again, Dispatch's QueuePending UPSERTs the row's
	// scheduled_for. The cron itself should NOT delete the row.
	row := types.PendingNotification{
		ID:               "pending-1",
		UserID:           "user-1",
		NotificationType: "friend_request",
		ScheduledFor:     time.Now().Add(-1 * time.Minute),
	}

	later := time.Now().Add(2 * time.Hour)
	dispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		return notifications.Decision{
			Send:        false,
			SnoozeUntil: &later,
			Reason:      notifications.ReasonQuietHours,
		}
	}
	listDue := func(_ int) ([]types.PendingNotification, error) {
		return []types.PendingNotification{row}, nil
	}
	deleted := 0
	deleteRow := func(_ string) error {
		deleted++
		return nil
	}

	deliverPendingNotificationsWith(nil, dispatch, listDue, deleteRow)()

	if deleted != 0 {
		t.Errorf("delete called %d times, want 0 (re-snooze keeps row)", deleted)
	}
}

func TestDeliverPendingNotifications_PermanentSkipDeletesRow(t *testing.T) {
	// User disabled the notification type between queue and delivery.
	// Resolver returns Send=false with no SnoozeUntil — that's a permanent
	// skip, so the cron should drop the row.
	row := types.PendingNotification{
		ID:           "pending-1",
		ScheduledFor: time.Now().Add(-1 * time.Minute),
	}

	dispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		return notifications.Decision{
			Send:   false,
			Reason: notifications.ReasonTypeDisabled,
		}
	}
	listDue := func(_ int) ([]types.PendingNotification, error) {
		return []types.PendingNotification{row}, nil
	}
	deleted := map[string]bool{}
	deleteRow := func(id string) error {
		deleted[id] = true
		return nil
	}

	deliverPendingNotificationsWith(nil, dispatch, listDue, deleteRow)()

	if !deleted["pending-1"] {
		t.Error("expected pending-1 to be deleted after permanent skip")
	}
}

func TestDeliverPendingNotifications_ListErrorSwallowed(t *testing.T) {
	dispatched := 0
	dispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	}
	listDue := func(_ int) ([]types.PendingNotification, error) {
		return nil, errors.New("db down")
	}
	deleteRow := func(_ string) error { return nil }

	// Must not panic, must not call dispatch.
	deliverPendingNotificationsWith(nil, dispatch, listDue, deleteRow)()

	if dispatched != 0 {
		t.Errorf("dispatch called %d times on error path, want 0", dispatched)
	}
}

func TestPendingToDispatchOptions_ConvertsCleanly(t *testing.T) {
	friendID := "friend-2"
	row := types.PendingNotification{
		UserID:           "user-1",
		NotificationType: "friend_request",
		DedupKey:         "friendship-42",
		Category:         string(notifications.CategoryFAndA),
		FriendID:         &friendID,
		Title:            "T",
		Body:             "B",
		Data:             map[string]any{"k": "v"},
	}
	opts := pendingToDispatchOptions(row)
	if opts.UserID != "user-1" || opts.NotificationType != "friend_request" {
		t.Errorf("wrong basic fields: %+v", opts)
	}
	if opts.Category != notifications.CategoryFAndA {
		t.Errorf("Category = %q, want %q", opts.Category, notifications.CategoryFAndA)
	}
	if opts.FriendID != "friend-2" {
		t.Errorf("FriendID = %q, want friend-2 (should be dereferenced)", opts.FriendID)
	}
	// nil pointer FriendID → empty string (not "<nil>")
	row.FriendID = nil
	opts = pendingToDispatchOptions(row)
	if opts.FriendID != "" {
		t.Errorf("FriendID with nil pointer = %q, want empty", opts.FriendID)
	}
}
