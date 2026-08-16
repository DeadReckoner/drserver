package main

import (
	"context"
	"dr/server/analytics"
	"dr/server/api"
	"dr/server/database"
	"dr/server/notifications/jobs"
	"dr/server/observability"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embeds the IANA timezone database in the binary as a fallback for when
	// the host has none. Quiet hours and digest delivery resolve each user's
	// local time via time.LoadLocation, and both callers treat a lookup
	// failure as "use UTC" *silently* (notifications/resolver.go:203,
	// types/types.go:643). On a base image without tzdata that means every
	// user's quiet hours are computed in UTC and nothing logs a complaint.
	// This import makes correctness independent of the runtime image.
	_ "time/tzdata"

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

	// Recovery, not notification work: generation goroutines run detached from
	// the request and die with the process, so this is what unsticks a user
	// whose roadmap was mid-generation during a deploy. Every 5 minutes bounds
	// how long they wait past the staleness timeout itself.
	mustRegister(c, "*/5 * * * *", "reap_stale_jobs", jobs.ReapStaleJobs(db))

	// Telemetry heartbeat. Every other signal in this system is emitted only
	// when something happens, which means total instrumentation failure is
	// indistinguishable from a quiet night — the dashboards just go flat and
	// nobody can tell whether that is real. A fixed-cadence event gives the
	// monitors something whose *absence* is unambiguous.
	//
	// Every minute, and deliberately not wrapped in jobs.Timed: the wrapper's
	// own duration logging would be noise for a job whose entire purpose is to
	// be a single predictable line.
	if _, err := c.AddFunc("* * * * *", func() {
		analytics.Heartbeat(context.Background())
	}); err != nil {
		log.Fatalf("cron.AddFunc(heartbeat): %v", err)
	}

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

	role, err := resolveRole(os.Args[1:])
	if err != nil {
		log.Fatalf("invalid role: %v", err)
	}

	// Error reporting first, so a panic during the rest of startup is still
	// reported. No-op unless SENTRY_DSN is set.
	flushSentry := observability.Init()
	defer flushSentry()

	// Product-event pipeline. Buffered and batched, so the deferred flush is
	// what stops a deploy from discarding the last few seconds of events.
	// Falls back to the log stream when Axiom ingest isn't configured.
	flushAnalytics := analytics.Init()
	defer flushAnalytics()

	db := database.Connect(ctx)

	// Cron runs only where the role says it should. Running it on every web
	// machine would double-fire every scan.
	var scheduler *cron.Cron
	if role.runsScheduler() {
		scheduler = setupScheduler(db)
		scheduler.Start()
		log.Printf("[%s] Scheduler started with %d jobs", role, len(scheduler.Entries()))
	}

	// The scheduler-only process still serves HTTP, but just the probes: Fly
	// needs something to health check, and a wedged scheduler should be
	// restartable. It is not attached to the public http_service, so these
	// routes are only reachable inside the private network.
	var handler http.Handler
	if role.runsWeb() {
		handler = api.NewRouter(db)
		log.Printf("[%s] API routes registered", role)
	} else {
		handler = api.NewHealthOnlyRouter(db)
		log.Printf("[%s] health-only routes registered", role)
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler,
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
	if scheduler != nil {
		stopCtx := scheduler.Stop()
		select {
		case <-stopCtx.Done():
			log.Println("Scheduler stopped cleanly")
		case <-time.After(5 * time.Second):
			log.Println("Scheduler shutdown timed out; in-flight jobs were interrupted")
		}
	}

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %s", err)
	}
	log.Println("Server exiting")
}
