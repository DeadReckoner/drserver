package jobs

import (
	"strings"
	"testing"

	"dr/server/internal/testutil"
	"dr/server/notifications"
)

// =============================================================================
// Pure formula tests — table-driven coverage of every threshold boundary.
// =============================================================================

func TestIsFallingBehind_FormulaThresholds(t *testing.T) {
	cases := []struct {
		name string
		// inputs
		missedIn7d   int
		shouldBeDone int
		actuallyDone int
		// expectation
		want bool
		// short rationale, included in test failure for readability
		why string
	}{
		// =====================================================================
		// Leg B: ≥3 missed in 7d
		// =====================================================================
		{"leg B: 3 missed in 7d trips", 3, 0, 0, true, "exactly at threshold"},
		{"leg B: 2 missed in 7d does not trip", 2, 0, 0, false, "below threshold"},
		{"leg B: 10 missed in 7d trips", 10, 100, 100, true, "well past, even when pace is fine"},

		// =====================================================================
		// Leg D absolute: deficit ≥5
		// =====================================================================
		{"leg D abs: deficit of exactly 5 trips", 0, 10, 5, true, "10-5 = 5"},
		{"leg D abs: deficit of 4 does not trip", 0, 100, 96, false, "deficit 4 < 5 abs and 4% < 20% ratio"},
		{"leg D abs: deficit of 20 trips", 0, 50, 30, true, "well past"},

		// =====================================================================
		// Leg D ratio: ≥20% behind WITH should_be_done ≥ 5 floor
		// =====================================================================
		{"leg D ratio: 1/1 = 100% behind but below floor does not trip", 0, 1, 0, false, "should_be_done < 5 floor"},
		{"leg D ratio: 4 should / 0 done = 100% but below floor", 0, 4, 0, false, "should_be_done < 5 floor"},
		{"leg D ratio: 5 should / 4 done = 20% deficit trips", 0, 5, 4, true, "5/5 = 100%? no, deficit 1/5 = 20% — at threshold"},
		{"leg D ratio: 10 should / 8 done = 20% deficit trips", 0, 10, 8, true, "2/10 = 20% at threshold"},
		{"leg D ratio: 10 should / 9 done = 10% does not trip", 0, 10, 9, false, "1/10 = 10% < 20%"},
		{"leg D ratio: 100 should / 80 done = 20% trips", 0, 100, 80, true, "boundary on a big roadmap"},

		// =====================================================================
		// Multiple legs trip — still true
		// =====================================================================
		{"both legs trip", 5, 50, 20, true, "missed 5 AND deficit 30 — either alone trips"},

		// =====================================================================
		// All clear
		// =====================================================================
		{"nothing trips, small roadmap", 0, 3, 3, false, "small roadmap, on pace"},
		{"nothing trips, big roadmap", 1, 100, 99, false, "deficit 1 = 1%, 1 missed this week"},
		{"empty stats", 0, 0, 0, false, "no scheduled tasks"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fallingBehindStats{
				MissedIn7d:   tc.missedIn7d,
				ShouldBeDone: tc.shouldBeDone,
				ActuallyDone: tc.actuallyDone,
			}
			got := isFallingBehind(s)
			if got != tc.want {
				t.Errorf("isFallingBehind(%+v) = %v, want %v — %s", s, got, tc.want, tc.why)
			}
		})
	}
}

// =============================================================================
// Body string — verify which leg wins when multiple trip.
// =============================================================================

func TestFallingBehindBody_PrioritizesDeficit(t *testing.T) {
	cases := []struct {
		name   string
		stats  fallingBehindStats
		expect string // substring the body must contain
	}{
		{
			name:   "deficit wins when both trip",
			stats:  fallingBehindStats{Goal: "Learn Go", MissedIn7d: 5, ShouldBeDone: 50, ActuallyDone: 40},
			expect: "10 tasks behind",
		},
		{
			name:   "missed-count message when only leg B trips",
			stats:  fallingBehindStats{Goal: "Learn Go", MissedIn7d: 4, ShouldBeDone: 10, ActuallyDone: 9},
			expect: "missed 4 tasks",
		},
		{
			name:   "generic message when only ratio leg trips",
			stats:  fallingBehindStats{Goal: "Learn Go", MissedIn7d: 0, ShouldBeDone: 10, ActuallyDone: 8},
			expect: "falling behind",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fallingBehindBody(tc.stats)
			if !strings.Contains(got, tc.expect) {
				t.Errorf("body = %q, want substring %q", got, tc.expect)
			}
			if !strings.Contains(got, tc.stats.Goal) {
				t.Errorf("body = %q, want it to include the goal name %q", got, tc.stats.Goal)
			}
		})
	}
}

// =============================================================================
// Scan composition tests — sqlmock for the stats query + fake dispatch.
// =============================================================================

func TestScanFallingBehindWith_OnlyDispatchesForFailingFormula(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)

	// Three roadmaps: one trips leg B, one trips leg D abs, one is fine.
	mock.ExpectQuery(`WITH user_today`).
		WillReturnRows(mock.NewRows([]string{
			"roadmap_id", "user_id", "goal", "iso_year_week",
			"missed_in_7d", "should_be_done", "actually_done",
		}).
			AddRow("roadmap-B", "user-1", "Learn Go", "2026-21", 5, 0, 0).         // leg B
			AddRow("roadmap-D", "user-2", "Run a marathon", "2026-21", 0, 20, 10). // leg D abs
			AddRow("roadmap-OK", "user-3", "Read more", "2026-21", 1, 100, 99))    // deficit 1 = 1%, not behind

	var dispatched []notifications.DispatchOptions
	fakeDispatch := func(opts notifications.DispatchOptions) notifications.Decision {
		dispatched = append(dispatched, opts)
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	}

	scanFallingBehindWith(db, fakeDispatch)()

	if got := len(dispatched); got != 2 {
		t.Fatalf("dispatched %d times, want 2 (B and D legs)", got)
	}
	// Verify each dispatched roadmap by inspecting the dedup key.
	got := map[string]bool{}
	for _, opts := range dispatched {
		got[opts.DedupKey] = true
		if opts.NotificationType != "falling_behind" {
			t.Errorf("NotificationType = %q, want falling_behind", opts.NotificationType)
		}
		if opts.Category != notifications.CategoryMyGoals {
			t.Errorf("Category = %q, want my_goals", opts.Category)
		}
	}
	if !got["roadmap-B:2026-21"] {
		t.Errorf("missing dispatch for roadmap-B; got %v", got)
	}
	if !got["roadmap-D:2026-21"] {
		t.Errorf("missing dispatch for roadmap-D; got %v", got)
	}
	if got["roadmap-OK:2026-21"] {
		t.Errorf("dispatched for on-pace roadmap-OK; should have been skipped")
	}
}

func TestScanFallingBehindWith_NoStatsDispatchesNothing(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	mock.ExpectQuery(`WITH user_today`).
		WillReturnRows(mock.NewRows([]string{
			"roadmap_id", "user_id", "goal", "iso_year_week",
			"missed_in_7d", "should_be_done", "actually_done",
		}))

	dispatched := 0
	fakeDispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	}
	scanFallingBehindWith(db, fakeDispatch)()

	if dispatched != 0 {
		t.Errorf("dispatched %d times, want 0", dispatched)
	}
}

func TestScanFallingBehindWith_QueryErrorSwallowed(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	mock.ExpectQuery(`WITH user_today`).WillReturnError(errFakeDB)

	dispatched := 0
	fakeDispatch := func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	}
	scanFallingBehindWith(db, fakeDispatch)()

	if dispatched != 0 {
		t.Errorf("dispatched %d times on error path, want 0", dispatched)
	}
}
