package jobs

import (
	"fmt"
	"time"

	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"

	"gorm.io/gorm"
)

// staleJobTimeout is how long a background job may sit in pending/processing
// without a progress update before the reaper declares it dead.
//
// Sizing constraint: this MUST exceed the longest stretch a healthy generation
// can run without touching background_jobs.updated_at, or the reaper will kill
// live work. The progress updates in processRoadmapGeneration bracket each AI
// call (10 -> 50 -> 80), so the longest silent gap is one provider request:
// the custom client in agent/agent.go uses a 180s timeout, and the Anthropic
// SDK retries on top of its own. 15 minutes leaves several multiples of
// headroom over the realistic worst case.
//
// The cost of raising it is how long a user stares at a spinner after a deploy
// kills their generation; the cost of lowering it too far is killing healthy
// generations. Err high.
const staleJobTimeout = 15 * time.Minute

// ReapStaleJobs is the cron entry point for the stale background-job sweep.
//
// Generation goroutines run detached from the HTTP request and are not tracked
// by the server's graceful shutdown, so a deploy, crash, or OOM kills them
// mid-flight with their job row still marked 'processing'. Nothing else ever
// moves that row, so without this sweep the user's roadmap is stuck on a
// spinner permanently, with no retry path.
//
// The db param is unused (the database helpers bind their own connection) but
// kept so registration in main.setupScheduler reads like every other job.
func ReapStaleJobs(_ *gorm.DB) func() {
	return reapStaleJobsWith(database.FailStaleJobs, notifications.Dispatch, time.Now)
}

// reapStaleJobsWith is the testable form.
func reapStaleJobsWith(
	failStale func(cutoff time.Time) ([]database.StaleJob, error),
	dispatch func(notifications.DispatchOptions) notifications.Decision,
	now func() time.Time,
) func() {
	return func() {
		ctx := scanCtx("reap_stale_jobs")

		stale, err := failStale(now().Add(-staleJobTimeout))
		if err != nil {
			log.Error(ctx, "stale job sweep failed", err)
			return
		}

		for _, job := range stale {
			// Log every reap: a healthy deploy reaps nothing, so a nonzero
			// count here is the signal that generations are being interrupted.
			log.Warn(ctx, "failed stale background job",
				fmt.Sprintf("job_id=%s type=%s user_id=%s roadmap_id=%s",
					job.JobID, job.Type, job.UserID, job.RoadmapID))

			// Tell the user, so a dead generation surfaces as a failure they
			// can retry instead of a spinner that never resolves. Reuses the
			// same notification type the in-process error path sends, and the
			// same dedup key shape, so a job that failed both ways notifies
			// once.
			dispatch(notifications.DispatchOptions{
				UserID:           job.UserID,
				Category:         notifications.CategoryMyGoals,
				NotificationType: "roadmap_failed",
				DedupKey:         job.JobID + ":failed",
				Title:            "Roadmap Generation Failed",
				Body:             "We encountered an issue. Please try again.",
				Data: map[string]any{
					"roadmap_id": job.RoadmapID,
					"error":      database.StaleJobError,
				},
			})
		}

		summarize(ctx, len(stale) > 0,
			fmt.Sprintf("reaped %d stale job(s)", len(stale)))
	}
}
