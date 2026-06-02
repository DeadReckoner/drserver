// Package jobs hosts the cron-driven functions that scan the database for
// state changes and dispatch the right notifications. These run on the
// schedule registered in main.setupScheduler.
//
// Step 6 lands this package with stub bodies — each scan logs that it ran
// and returns. Steps 7–10 fill in the real queries:
//
//	ScanMissedTasks       -> Step 7 (missed-task EOD detection)
//	ScanAtRiskMilestones  -> Step 7 (uses ComputeAllStatuses output)
//	ScanFallingBehind     -> Step 8 (B ∪ D formula from the LOE doc)
//	DeliverDueDigests     -> Step 10 (daily + weekly composition)
//	DeliverAnchorBatches  -> Step 10 (9am-local fan-out)
//	CleanupDeliveryLog    -> Step 12 (retention sweep, e.g. drop > 90 days)
//
// Each function takes the *gorm.DB and returns a `func()` that cron can
// register. The closure captures the DB so jobs don't read a package
// global, which keeps them testable in isolation.
package jobs

import (
	"context"

	"dr/server/log"

	"gorm.io/gorm"
)

// scanCtx returns a log context tagged with the job name. Centralized so all
// jobs use a consistent "cron" correlation prefix and a future change to
// observability (metrics, tracing) only touches one place.
func scanCtx(jobName string) context.Context {
	return log.WithLogger(context.Background(), log.NewLogger("cron:"+jobName))
}

// ScanMissedTasks and ScanAtRiskMilestones are defined in missed_tasks.go and
// at_risk_milestones.go respectively as of Step 7.

// ScanFallingBehind applies the B ∪ D formula (≥3 missed in 7d OR pace
// deficit ≥5 tasks / ≥20%) to identify roadmaps that should fire a
// falling_behind notification. Cooldown is once-per-ISO-week per roadmap.
// STUB until Step 8.
func ScanFallingBehind(_ *gorm.DB) func() {
	return func() {
		ctx := scanCtx("scan_falling_behind")
		log.Info(ctx, "stub: 0 candidates")
	}
}

// DeliverDueDigests picks users whose digest hour-of-day matches the current
// minute in their local timezone and composes their pending digest. SQL
// filters by `now() AT TIME ZONE u.timezone` so the work happens in Postgres.
// STUB until Step 10.
func DeliverDueDigests(_ *gorm.DB) func() {
	return func() {
		ctx := scanCtx("deliver_due_digests")
		log.Info(ctx, "stub: 0 recipients")
	}
}

// DeliverAnchorBatches fans out the 9am-local routine accountability
// notifications to anchors. STUB until Step 10.
func DeliverAnchorBatches(_ *gorm.DB) func() {
	return func() {
		ctx := scanCtx("deliver_anchor_batches")
		log.Info(ctx, "stub: 0 anchors")
	}
}

// CleanupDeliveryLog drops notification_delivery_log rows older than the
// retention window. STUB until Step 12; suggested retention is 90 days.
func CleanupDeliveryLog(_ *gorm.DB) func() {
	return func() {
		ctx := scanCtx("cleanup_delivery_log")
		log.Info(ctx, "stub: 0 rows removed")
	}
}
