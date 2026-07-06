// Package jobs hosts the cron-driven functions that scan the database for
// state changes and dispatch the right notifications. These run on the
// schedule registered in main.setupScheduler.
//
//	ScanMissedTasks             -> missed_tasks.go            (Step 7)
//	ScanAtRiskMilestones        -> at_risk_milestones.go      (Step 7)
//	ScanFallingBehind           -> falling_behind.go          (Step 8)
//	ScanNextMilestoneNudges     -> next_milestone.go          (drserver#67)
//	DeliverPendingNotifications -> deliver_pending.go         (Step 9)
//	DeliverDueDigests           -> deliver_digests.go         (Step 10)
//	DeliverAnchorBatches        -> deliver_anchor_batches.go  (Step 10)
//	CleanupDeliveryLog          -> cleanup.go                 (Step 12)
//
// Each function takes the *gorm.DB and returns a `func()` that cron can
// register. The closure captures the DB so jobs don't read a package
// global, which keeps them testable in isolation.
package jobs

import (
	"context"
	"fmt"
	"time"

	"dr/server/log"
)

// scanCtx returns a log context tagged with the job name. Centralized so all
// jobs use a consistent "cron" correlation prefix and a future change to
// observability (metrics, tracing) only touches one place.
func scanCtx(jobName string) context.Context {
	return log.WithLogger(context.Background(), log.NewLogger("cron:"+jobName))
}

// summarize logs a job's per-run summary line — Info when the run did real
// work, Debug when it was a no-op. The per-minute cadence makes Info-level
// no-op lines pure noise at production log levels.
func summarize(ctx context.Context, didWork bool, msg string) {
	if didWork {
		log.Info(ctx, msg)
		return
	}
	log.Debug(ctx, msg)
}

// slowJobThreshold is how long a single cron run can take before Timed logs
// a warning. SkipIfStillRunning starts dropping ticks once a job crosses its
// own cadence (60s for the per-minute jobs), so warn well before that.
const slowJobThreshold = 10 * time.Second

// Timed wraps a job closure with duration logging: Debug for normal runs,
// Warn when a run crosses slowJobThreshold. main.setupScheduler wraps every
// registration so slow scans surface in logs before they start eating ticks.
func Timed(name string, fn func()) func() {
	return func() {
		start := time.Now()
		fn()
		elapsed := time.Since(start).Round(time.Millisecond)

		ctx := scanCtx(name)
		if elapsed >= slowJobThreshold {
			log.Warn(ctx, fmt.Sprintf("slow job: run took %s", elapsed))
			return
		}
		log.Debug(ctx, fmt.Sprintf("run took %s", elapsed))
	}
}
