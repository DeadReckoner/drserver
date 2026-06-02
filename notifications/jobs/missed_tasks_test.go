package jobs

import (
	"testing"

	"dr/server/internal/testutil"
	"dr/server/notifications"
)

// TestScanMissedTasksWith_DispatchesPerCandidate verifies the scan calls
// Dispatch once per row returned by the candidate query, with the right
// notification type and dedup key.
func TestScanMissedTasksWith_DispatchesPerCandidate(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)

	// Stub out the candidate query with two rows. Regex matcher is loose
	// on whitespace; the important thing is we return the right columns.
	mock.ExpectQuery(`SELECT t\.task_id`).
		WillReturnRows(mock.NewRows([]string{"task_id", "roadmap_id", "user_id", "title"}).
			AddRow("task-1", "roadmap-A", "user-1", "Do thing one").
			AddRow("task-2", "roadmap-A", "user-1", "Do thing two"))

	var dispatched []notifications.DispatchOptions
	fakeDispatch := func(opts notifications.DispatchOptions) notifications.Decision {
		dispatched = append(dispatched, opts)
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	}

	scanMissedTasksWith(db, fakeDispatch)()

	if got := len(dispatched); got != 2 {
		t.Fatalf("dispatched %d times, want 2", got)
	}
	for i, opts := range dispatched {
		if opts.Category != notifications.CategoryMyGoals {
			t.Errorf("[%d] Category = %q, want my_goals", i, opts.Category)
		}
		if opts.NotificationType != "missed_task" {
			t.Errorf("[%d] NotificationType = %q, want missed_task", i, opts.NotificationType)
		}
		if opts.UserID != "user-1" {
			t.Errorf("[%d] UserID = %q, want user-1", i, opts.UserID)
		}
		if opts.DedupKey != opts.Data["task_id"] {
			t.Errorf("[%d] DedupKey = %q, expected to equal task_id %v", i, opts.DedupKey, opts.Data["task_id"])
		}
	}
	if dispatched[0].DedupKey != "task-1" || dispatched[1].DedupKey != "task-2" {
		t.Errorf("dedup keys = [%q, %q], want [task-1, task-2]", dispatched[0].DedupKey, dispatched[1].DedupKey)
	}
}

// TestScanMissedTasksWith_NoCandidatesDispatchesNothing verifies the
// happy-zero-rows path doesn't accidentally fire.
func TestScanMissedTasksWith_NoCandidatesDispatchesNothing(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	mock.ExpectQuery(`SELECT t\.task_id`).
		WillReturnRows(mock.NewRows([]string{"task_id", "roadmap_id", "user_id", "title"}))

	dispatched := 0
	fakeDispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	}

	scanMissedTasksWith(db, fakeDispatch)()

	if dispatched != 0 {
		t.Errorf("dispatched %d times, want 0 (no candidates)", dispatched)
	}
}

// TestScanMissedTasksWith_QueryErrorSwallowed verifies the scan logs and
// returns rather than panicking when the DB errors. cron.Recover would
// catch panics, but the contract is "log + bail."
func TestScanMissedTasksWith_QueryErrorSwallowed(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	mock.ExpectQuery(`SELECT t\.task_id`).WillReturnError(errFakeDB)

	dispatched := 0
	fakeDispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	}

	// Should not panic and should not dispatch.
	scanMissedTasksWith(db, fakeDispatch)()

	if dispatched != 0 {
		t.Errorf("dispatched on error path: %d times", dispatched)
	}
}
