package database

import (
	"time"

	"dr/server/types"

	"gorm.io/gorm/clause"
)

// =============================================================================
// pending_notifications
// =============================================================================
// Holds notifications the resolver said to DEFER. The
// DeliverPendingNotifications cron picks rows where ScheduledFor <= now() and
// re-runs Dispatch on them.

// UpsertPendingNotification creates a new pending row or, if one already
// exists for (UserID, NotificationType, DedupKey, Channel), updates its
// ScheduledFor to the new target and increments its Attempts counter. This
// makes Dispatch idempotent during a deferral — a second trigger firing
// while the first is still pending just refreshes the schedule.
func UpsertPendingNotification(p types.PendingNotification) error {
	// data is jsonb NOT NULL; a nil map would serialize to jsonb 'null'.
	// Normalize to {} so the stored shape matches the column's '{}' default.
	if p.Data == nil {
		p.Data = map[string]any{}
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "notification_type"},
			{Name: "dedup_key"},
			{Name: "channel"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"scheduled_for": p.ScheduledFor,
			"attempts":      gormExpr("pending_notifications.attempts + 1"),
			"updated_at":    time.Now(),
			// Don't refresh the payload on conflict — the original capture
			// is what the user is owed; trigger spam shouldn't be able to
			// rewrite the title/body of a queued notification.
		}),
	}).Create(&p).Error
}

// ListDuePendingNotifications returns pending rows ready for delivery
// (ScheduledFor <= now) ordered oldest-first, with an attempts cap so a
// stuck row doesn't get retried forever. Cap is intentionally large
// (~100); the goal is just to drop runaways, not to be aggressive.
func ListDuePendingNotifications(maxAttempts int) ([]types.PendingNotification, error) {
	var out []types.PendingNotification
	err := db.Where("scheduled_for <= ? AND attempts < ?", time.Now(), maxAttempts).
		Order("scheduled_for ASC").
		Find(&out).Error
	return out, err
}

// DeletePendingNotification removes a pending row by ID. Called when the
// row was successfully delivered or permanently skipped.
func DeletePendingNotification(id string) error {
	return db.Where("id = ?", id).Delete(&types.PendingNotification{}).Error
}

// CleanupExhaustedPendingNotifications deletes pending rows that hit the
// attempts cap. ListDuePendingNotifications already excludes them from
// delivery; this retention sweep stops them from accumulating forever.
// Returns the number of rows removed.
func CleanupExhaustedPendingNotifications(maxAttempts int) (int64, error) {
	res := db.Where("attempts >= ?", maxAttempts).Delete(&types.PendingNotification{})
	return res.RowsAffected, res.Error
}

// gormExpr is a thin wrapper around gorm.Expr so the call sites read cleanly.
// Kept here rather than exposing gorm internals at the use site.
func gormExpr(s string) any {
	return clause.Expr{SQL: s}
}
