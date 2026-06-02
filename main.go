package main

import (
	"context"
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
	mustRegister(c, "* * * * *", jobs.ScanMissedTasks(db))
	mustRegister(c, "*/5 * * * *", jobs.ScanFallingBehind(db))
	mustRegister(c, "*/5 * * * *", jobs.ScanAtRiskMilestones(db))
	mustRegister(c, "* * * * *", jobs.DeliverDueDigests(db))
	mustRegister(c, "* * * * *", jobs.DeliverAnchorBatches(db))
	mustRegister(c, "* * * * *", jobs.DeliverPendingNotifications(db))
	mustRegister(c, "0 * * * *", jobs.CleanupDeliveryLog(db))

	return c
}

// mustRegister adds a job to the cron and fatally exits if the spec
// fails to parse. We register at startup, so a misconfigured spec should
// crash the binary loudly rather than silently fail.
func mustRegister(c *cron.Cron, spec string, fn func()) {
	if _, err := c.AddFunc(spec, fn); err != nil {
		log.Fatalf("cron.AddFunc(%q): %v", spec, err)
	}
}

func main() {
	ctx := context.Background()

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
