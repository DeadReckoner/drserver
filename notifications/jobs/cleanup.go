package jobs

import (
	"fmt"
	"time"

	"dr/server/database"
	"dr/server/log"

	"gorm.io/gorm"
)

// Retention windows for the hourly cleanup sweep.
//
// deliveryLogRetention MUST stay comfortably longer than every dedup horizon
// that reads notification_delivery_log, or aged-out rows would let an old
// event re-notify:
//
//   - missed_task — once-ever per task, but candidates are bounded to the
//     last missedTaskWindowDays (30) days, so 30 days is the horizon
//   - at_risk_milestone / falling_behind — once per ISO week (7 days)
//
// 90 days against a 30-day max horizon leaves a 3x margin. If you shrink
// this, re-check missed_tasks.go first.
const (
	deliveryLogRetention = 90 * 24 * time.Hour
	digestItemRetention  = 30 * 24 * time.Hour
)

// CleanupDeliveryLog is the cron entry point for the hourly retention sweep.
// Despite the historical name it owns all three notification housekeeping
// sweeps: the delivery log, exhausted pending rows, and stale digest items.
// The db param is unused (the database helpers bind their own connection)
// but kept so registration in main.setupScheduler reads like every other job.
func CleanupDeliveryLog(_ *gorm.DB) func() {
	return cleanupWith(
		database.CleanupDeliveryLog,
		database.CleanupExhaustedPendingNotifications,
		database.CleanupStaleDigestItems,
		time.Now,
	)
}

// cleanupWith is the testable form. Each sweep is independent — a failure in
// one is logged and the others still run, so a bad table can't block all
// retention.
func cleanupWith(
	cleanDeliveryLog func(cutoff time.Time) (int64, error),
	cleanExhaustedPending func(maxAttempts int) (int64, error),
	cleanStaleDigestItems func(cutoff time.Time) (int64, error),
	now func() time.Time,
) func() {
	return func() {
		ctx := scanCtx("cleanup_delivery_log")

		logRows, err := cleanDeliveryLog(now().Add(-deliveryLogRetention))
		if err != nil {
			log.Error(ctx, "delivery log sweep failed", err)
		}
		pendingRows, err := cleanExhaustedPending(maxPendingAttempts)
		if err != nil {
			log.Error(ctx, "exhausted pending sweep failed", err)
		}
		digestRows, err := cleanStaleDigestItems(now().Add(-digestItemRetention))
		if err != nil {
			log.Error(ctx, "stale digest items sweep failed", err)
		}

		summarize(ctx, logRows+pendingRows+digestRows > 0,
			fmt.Sprintf("removed %d delivery log, %d exhausted pending, %d stale digest rows",
				logRows, pendingRows, digestRows))
	}
}
