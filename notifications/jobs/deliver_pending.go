package jobs

import (
	"fmt"

	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"

	"gorm.io/gorm"
)

// maxPendingAttempts is the soft cap on how many times the cron will retry a
// pending row before dropping it. The goal is just an infinite-loop backstop
// for a misconfigured TZ or a stuck setting — not aggressive retry limiting.
//
// At a per-minute cron cadence, 100 attempts ≈ 100 minutes of trying.
const maxPendingAttempts = 100

// DeliverPendingNotifications is the cron entry point. Every minute, picks
// up pending rows whose ScheduledFor has elapsed and re-runs them through
// Dispatch. Rows that send or skip permanently are deleted; rows that
// re-snooze are left for the next tick (Dispatch's QueuePending UPSERT
// updates ScheduledFor).
func DeliverPendingNotifications(db *gorm.DB) func() {
	return deliverPendingNotificationsWith(
		db,
		notifications.Dispatch,
		database.ListDuePendingNotifications,
		database.DeletePendingNotification,
	)
}

// deliverPendingNotificationsWith is the testable form. All side-effecting
// dependencies are injected.
func deliverPendingNotificationsWith(
	db *gorm.DB,
	dispatch func(notifications.DispatchOptions) notifications.Decision,
	listDue func(maxAttempts int) ([]types.PendingNotification, error),
	deleteRow func(id string) error,
) func() {
	return func() {
		ctx := scanCtx("deliver_pending")

		pending, err := listDue(maxPendingAttempts)
		if err != nil {
			log.Error(ctx, "list due pending failed", err)
			return
		}

		var (
			sent      int
			resnoozed int
			cleanedUp int
		)
		for _, p := range pending {
			opts := pendingToDispatchOptions(p)
			decision := dispatch(opts)

			if decision.SnoozeUntil != nil {
				// Re-snoozed. QueuePending inside Dispatch already updated
				// the row's ScheduledFor via UPSERT, so we leave it alone.
				resnoozed++
				continue
			}

			// Either sent (decision.Send=true) or permanently skipped
			// (decision.Send=false with no SnoozeUntil — type disabled,
			// muted, etc). Either way the row is done.
			if err := deleteRow(p.ID); err != nil {
				log.Error(ctx, "delete pending row failed", err, "id", p.ID)
				continue
			}
			if decision.Send {
				sent++
			} else {
				cleanedUp++
			}
		}
		summarize(ctx, len(pending) > 0, fmt.Sprintf("processed %d pending: %d sent, %d re-snoozed, %d cleaned up",
			len(pending), sent, resnoozed, cleanedUp))
	}
}

// pendingToDispatchOptions reconstructs the DispatchOptions captured at queue
// time. Title/Body are sent as-was; if the situation has changed (e.g. user
// renamed a roadmap), the user sees the description that was true when the
// notification was originally triggered, which is correct.
func pendingToDispatchOptions(p types.PendingNotification) notifications.DispatchOptions {
	opts := notifications.DispatchOptions{
		UserID:           p.UserID,
		Category:         notifications.Category(p.Category),
		NotificationType: p.NotificationType,
		DedupKey:         p.DedupKey,
		Title:            p.Title,
		Body:             p.Body,
		Data:             p.Data,
	}
	if p.FriendID != nil {
		opts.FriendID = *p.FriendID
	}
	if p.AnchoredRoadmapID != nil {
		opts.AnchoredRoadmapID = *p.AnchoredRoadmapID
	}
	return opts
}
