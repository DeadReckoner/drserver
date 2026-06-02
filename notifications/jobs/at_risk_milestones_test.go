package jobs

import (
	"strings"
	"testing"

	"dr/server/internal/testutil"
	"dr/server/notifications"
)

func TestScanAtRiskMilestonesWith_DispatchesPerCandidate(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	mock.ExpectQuery(`SELECT DISTINCT`).
		WillReturnRows(mock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal", "iso_year_week"}).
			AddRow("milestone-1", "roadmap-A", "user-1", "HTML basics", "2026-21").
			AddRow("milestone-2", "roadmap-B", "user-2", "Setup tooling", "2026-21"))

	var dispatched []notifications.DispatchOptions
	fakeDispatch := func(opts notifications.DispatchOptions) notifications.Decision {
		dispatched = append(dispatched, opts)
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	}

	scanAtRiskMilestonesWith(db, fakeDispatch)()

	if got := len(dispatched); got != 2 {
		t.Fatalf("dispatched %d times, want 2", got)
	}
	for i, opts := range dispatched {
		if opts.NotificationType != "milestone_at_risk" {
			t.Errorf("[%d] NotificationType = %q, want milestone_at_risk", i, opts.NotificationType)
		}
		if opts.Category != notifications.CategoryMyGoals {
			t.Errorf("[%d] Category = %q, want my_goals", i, opts.Category)
		}
		// DedupKey must combine milestone_id with the ISO year-week so
		// the same milestone can re-trigger next week.
		if !strings.Contains(opts.DedupKey, ":2026-21") {
			t.Errorf("[%d] DedupKey = %q, expected to contain :2026-21", i, opts.DedupKey)
		}
	}
	if dispatched[0].DedupKey != "milestone-1:2026-21" {
		t.Errorf("dedup key[0] = %q, want milestone-1:2026-21", dispatched[0].DedupKey)
	}
}

func TestScanAtRiskMilestonesWith_NoCandidatesDispatchesNothing(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	mock.ExpectQuery(`SELECT DISTINCT`).
		WillReturnRows(mock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal", "iso_year_week"}))

	dispatched := 0
	fakeDispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	}

	scanAtRiskMilestonesWith(db, fakeDispatch)()

	if dispatched != 0 {
		t.Errorf("dispatched %d times, want 0 (no candidates)", dispatched)
	}
}
