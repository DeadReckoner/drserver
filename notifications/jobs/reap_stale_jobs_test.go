package jobs

import (
	"errors"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/notifications"
)

var reapTestNow = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

// fakeReap wires reapStaleJobsWith with recorders so tests can assert the
// cutoff passed down and the notifications dispatched back up.
type fakeReap struct {
	now time.Time

	stale     []database.StaleJob
	failErr   error
	failCalls int
	cutoff    time.Time

	dispatched []notifications.DispatchOptions
}

func (f *fakeReap) run() {
	reapStaleJobsWith(
		func(cutoff time.Time) ([]database.StaleJob, error) {
			f.failCalls++
			f.cutoff = cutoff
			return f.stale, f.failErr
		},
		func(opts notifications.DispatchOptions) notifications.Decision {
			f.dispatched = append(f.dispatched, opts)
			return notifications.Decision{}
		},
		func() time.Time { return f.now },
	)()
}

func TestReapStaleJobs_UsesStaleTimeoutAsCutoff(t *testing.T) {
	f := &fakeReap{now: reapTestNow}
	f.run()

	if f.failCalls != 1 {
		t.Fatalf("failStale calls = %d, want 1", f.failCalls)
	}
	if want := reapTestNow.Add(-staleJobTimeout); !f.cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", f.cutoff, want)
	}
}

// The timeout has to clear the longest silent stretch of a healthy generation
// — one provider call, bounded by the 180s client timeout plus SDK retries.
// A regression here kills live generations, so pin the floor.
func TestReapStaleJobs_TimeoutClearsWorstCaseProviderCall(t *testing.T) {
	if staleJobTimeout < 10*time.Minute {
		t.Errorf("staleJobTimeout = %v, too low: a healthy generation can go "+
			"several minutes between progress updates during one AI call",
			staleJobTimeout)
	}
}

func TestReapStaleJobs_NotifiesOwnerOfEachReapedJob(t *testing.T) {
	f := &fakeReap{
		now: reapTestNow,
		stale: []database.StaleJob{
			{JobID: "job-1", UserID: "user-1", Type: "roadmap_generation", RoadmapID: "rm-1"},
			{JobID: "job-2", UserID: "user-2", Type: "roadmap_import", RoadmapID: "rm-2"},
		},
	}
	f.run()

	if len(f.dispatched) != 2 {
		t.Fatalf("dispatched %d notifications, want 2", len(f.dispatched))
	}

	first := f.dispatched[0]
	if first.UserID != "user-1" {
		t.Errorf("UserID = %q, want user-1", first.UserID)
	}
	if first.NotificationType != "roadmap_failed" {
		t.Errorf("NotificationType = %q, want roadmap_failed", first.NotificationType)
	}
	if first.Data["roadmap_id"] != "rm-1" {
		t.Errorf("roadmap_id = %v, want rm-1", first.Data["roadmap_id"])
	}
}

// The reaper and the in-process error path can both fire for the same job —
// a generation that failed normally but whose row was already stale. Matching
// dedup keys means the delivery log collapses them to one push.
func TestReapStaleJobs_DedupKeyMatchesInProcessFailurePath(t *testing.T) {
	f := &fakeReap{
		now:   reapTestNow,
		stale: []database.StaleJob{{JobID: "job-1", UserID: "user-1", RoadmapID: "rm-1"}},
	}
	f.run()

	if got, want := f.dispatched[0].DedupKey, "job-1:failed"; got != want {
		t.Errorf("DedupKey = %q, want %q — must match handleGenerationError", got, want)
	}
}

func TestReapStaleJobs_NoStaleJobsSendsNothing(t *testing.T) {
	f := &fakeReap{now: reapTestNow}
	f.run()

	if len(f.dispatched) != 0 {
		t.Errorf("dispatched %d notifications on an empty sweep, want 0", len(f.dispatched))
	}
}

// A failed sweep must not notify: an error means we don't know what was
// claimed, and guessing would push "generation failed" at users whose jobs are
// still running.
func TestReapStaleJobs_SweepErrorSendsNothing(t *testing.T) {
	f := &fakeReap{
		now:     reapTestNow,
		failErr: errors.New("db down"),
		stale:   []database.StaleJob{{JobID: "job-1", UserID: "user-1"}},
	}
	f.run()

	if len(f.dispatched) != 0 {
		t.Errorf("dispatched %d notifications after a sweep error, want 0", len(f.dispatched))
	}
}
