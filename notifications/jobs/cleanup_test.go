package jobs

import (
	"testing"
	"time"
)

// fakeCleanup wires cleanupWith with counters so tests can assert which
// sweeps ran and with what cutoffs.
type fakeCleanup struct {
	now time.Time

	deliveryLogCutoff time.Time
	deliveryLogErr    error
	deliveryLogCalls  int

	pendingMaxAttempts int
	pendingErr         error
	pendingCalls       int

	digestCutoff time.Time
	digestErr    error
	digestCalls  int
}

func (f *fakeCleanup) run() {
	cleanupWith(
		func(cutoff time.Time) (int64, error) {
			f.deliveryLogCalls++
			f.deliveryLogCutoff = cutoff
			return 1, f.deliveryLogErr
		},
		func(maxAttempts int) (int64, error) {
			f.pendingCalls++
			f.pendingMaxAttempts = maxAttempts
			return 1, f.pendingErr
		},
		func(cutoff time.Time) (int64, error) {
			f.digestCalls++
			f.digestCutoff = cutoff
			return 1, f.digestErr
		},
		func() time.Time { return f.now },
	)()
}

var cleanupTestNow = time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

func TestCleanup_RunsAllSweepsWithRightCutoffs(t *testing.T) {
	f := &fakeCleanup{now: cleanupTestNow}
	f.run()

	if f.deliveryLogCalls != 1 || f.pendingCalls != 1 || f.digestCalls != 1 {
		t.Fatalf("sweep calls = (%d, %d, %d), want (1, 1, 1)",
			f.deliveryLogCalls, f.pendingCalls, f.digestCalls)
	}
	if want := cleanupTestNow.Add(-90 * 24 * time.Hour); !f.deliveryLogCutoff.Equal(want) {
		t.Errorf("delivery log cutoff = %v, want %v (90 days)", f.deliveryLogCutoff, want)
	}
	if want := cleanupTestNow.Add(-30 * 24 * time.Hour); !f.digestCutoff.Equal(want) {
		t.Errorf("digest cutoff = %v, want %v (30 days)", f.digestCutoff, want)
	}
	if f.pendingMaxAttempts != maxPendingAttempts {
		t.Errorf("pending maxAttempts = %d, want %d (must match the deliver_pending cap)",
			f.pendingMaxAttempts, maxPendingAttempts)
	}
}

// TestCleanup_SweepFailureDoesNotBlockOthers guards the independence
// contract: one bad table can't stop the other retention sweeps.
func TestCleanup_SweepFailureDoesNotBlockOthers(t *testing.T) {
	f := &fakeCleanup{now: cleanupTestNow, deliveryLogErr: errFakeDB, pendingErr: errFakeDB}
	f.run() // must not panic

	if f.deliveryLogCalls != 1 || f.pendingCalls != 1 || f.digestCalls != 1 {
		t.Errorf("sweep calls = (%d, %d, %d), want (1, 1, 1) despite failures",
			f.deliveryLogCalls, f.pendingCalls, f.digestCalls)
	}
}

// TestCleanup_RetentionExceedsDedupHorizon is a tripwire: if someone shrinks
// deliveryLogRetention below the missed-task candidate window, once-ever
// dedup rows would age out while their task is still a candidate and the
// user would be re-notified. See cleanup.go's constant comment.
func TestCleanup_RetentionExceedsDedupHorizon(t *testing.T) {
	if horizon := missedTaskWindowDays * 24 * time.Hour; deliveryLogRetention <= horizon {
		t.Errorf("deliveryLogRetention (%v) must exceed the missed-task candidate window (%v)",
			deliveryLogRetention, horizon)
	}
}
