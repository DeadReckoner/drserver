package main

import (
	"context"
	"dr/server/agent"
	"dr/server/api"
	"dr/server/database"
	"dr/server/notifications/jobs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// setupScheduler builds the cron used for scheduled notification work and
// registers every job the notification system needs. Returns the cron
// ready to Start(); the caller is responsible for Start/Stop lifecycle.
//
// All jobs are wrapped by:
//   - cron.Recover         — a panic in one job is logged, not fatal
//   - cron.SkipIfStillRunning — same job won't run concurrent with itself,
//     so a slow scan won't pile up overlapping invocations
//
// Phase 1 of the scheduler architecture: in-process, single Fly machine.
// See `DeadReckoner/Notification Settings LOE.md` (vault) for the migration
// path to a dedicated worker process and eventually `river`.
func setupScheduler(db *gorm.DB) *cron.Cron {
	logger := cron.VerbosePrintfLogger(log.Default())
	c := cron.New(
		cron.WithLocation(time.UTC),
		cron.WithChain(
			cron.Recover(logger),
			cron.SkipIfStillRunning(logger),
		),
	)

	// Cadences chosen for responsiveness vs. DB load:
	//   - Every minute for time-of-day-sensitive deliveries (digest fan-out,
	//     9am-local anchor batch, missed-task EOD detection).
	//   - Every 5 minutes for status-change scans that don't need sub-minute
	//     freshness (falling-behind, at-risk).
	//   - Hourly for retention sweeps.
	//
	// Names match each job's internal scanCtx tag so the Timed duration lines
	// correlate with the job's own log output.
	mustRegister(c, "* * * * *", "scan_missed_tasks", jobs.ScanMissedTasks(db))
	mustRegister(c, "*/5 * * * *", "scan_falling_behind", jobs.ScanFallingBehind(db))
	mustRegister(c, "*/5 * * * *", "scan_at_risk_milestones", jobs.ScanAtRiskMilestones(db))
	mustRegister(c, "*/5 * * * *", "scan_next_milestone", jobs.ScanNextMilestoneNudges(db))
	mustRegister(c, "* * * * *", "deliver_due_digests", jobs.DeliverDueDigests(db))
	mustRegister(c, "* * * * *", "deliver_anchor_batches", jobs.DeliverAnchorBatches(db))
	mustRegister(c, "* * * * *", "deliver_pending", jobs.DeliverPendingNotifications(db))
	mustRegister(c, "0 * * * *", "cleanup_delivery_log", jobs.CleanupDeliveryLog(db))

	return c
}

// mustRegister adds a job to the cron and fatally exits if the spec
// fails to parse. We register at startup, so a misconfigured spec should
// crash the binary loudly rather than silently fail. Every job is wrapped
// with jobs.Timed so run durations land in the logs (Warn when slow).
func mustRegister(c *cron.Cron, spec, name string, fn func()) {
	if _, err := c.AddFunc(spec, jobs.Timed(name, fn)); err != nil {
		log.Fatalf("cron.AddFunc(%q, %q): %v", spec, name, err)
	}
}

func main() {
	ctx := context.Background()

	// Fail fast on an AI backend this process cannot actually use: an unknown
	// AI_PROVIDER, the offline mock on a hosted runtime, or a missing/truncated
	// key for the selected provider. Without this the process starts happily and
	// every generation fails at request time instead. The error names the
	// environment variable at fault and never its value.
	if err := agent.ValidateProviderConfig(); err != nil {
		log.Fatalf("AI provider configuration is invalid: %v", err)
	}

	db := database.Connect(ctx)

	scheduler := setupScheduler(db)
	scheduler.Start()
	log.Printf("Scheduler started with %d jobs", len(scheduler.Entries()))

	router := api.NewRouter(db)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop the scheduler first; cron.Stop() returns a context that's done
	// when in-flight jobs finish. We bound the wait so a hung job doesn't
	// keep the process alive past the shutdown deadline.
	stopCtx := scheduler.Stop()
	select {
	case <-stopCtx.Done():
		log.Println("Scheduler stopped cleanly")
	case <-time.After(5 * time.Second):
		log.Println("Scheduler shutdown timed out; in-flight jobs were interrupted")
	}

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %s", err)
	}
	log.Println("Server exiting")
}
