//go:build integration

package integration

// StartRoadmapSchedule moved the roadmap-start date math out of Go and into two
// set-based UPDATEs (see docs/db-optimization-plan.md §1.1). The Go loop it
// replaced was covered by sqlmock assertions on the per-row UPDATE arguments;
// sqlmock cannot execute SQL, so that coverage does not carry over. These tests
// are the replacement: they run the real statement against real Postgres and
// compare the stored dates against the Go expression that used to produce them.
//
// The oracle is deliberately the old implementation, spelled out inline:
//
//	types.ScheduleDayIn(now, loc).AddDate(0, 0, types.ScheduleOffset(day, dayZero))
//
// so a divergence between the SQL and the Go semantics fails here rather than
// showing up as silently misdated roadmaps.

import (
	"fmt"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/types"
)

// scheduleFixture is one roadmap with milestones and tasks at given Day offsets.
type scheduleFixture struct {
	roadmapID    string
	milestoneIDs map[int]string // day -> milestone_id
	taskIDs      map[int]string // day -> task_id
}

// seedSchedulable creates an unstarted roadmap with one milestone and one task
// per entry in milestoneDays / taskDays, all dates NULL.
func seedSchedulable(t *testing.T, userID string, milestoneDays, taskDays []int) scheduleFixture {
	t.Helper()

	f := scheduleFixture{
		milestoneIDs: map[int]string{},
		taskIDs:      map[int]string{},
	}
	if err := testDB.Raw(
		`insert into public.roadmaps (user_id, goal) values (?, ?) returning roadmap_id`,
		userID, "Learn Go",
	).Row().Scan(&f.roadmapID); err != nil {
		t.Fatalf("seed roadmap: %v", err)
	}

	for _, day := range milestoneDays {
		var id string
		if err := testDB.Raw(
			`insert into public.milestones (roadmap_id, user_id, sub_goal, day)
			 values (?, ?, ?, ?) returning milestone_id`,
			f.roadmapID, userID, fmt.Sprintf("m%d", day), day,
		).Row().Scan(&id); err != nil {
			t.Fatalf("seed milestone day=%d: %v", day, err)
		}
		f.milestoneIDs[day] = id
	}

	// Tasks need a parent milestone; reuse the first one seeded, creating a
	// bare one when the case asked for tasks only. Scheduling is scoped by
	// roadmap_id, so which milestone owns them doesn't affect the assertions —
	// picking milestoneDays[0] rather than ranging over the map just keeps the
	// fixture deterministic.
	parent := ""
	if len(milestoneDays) > 0 {
		parent = f.milestoneIDs[milestoneDays[0]]
	}
	if parent == "" && len(taskDays) > 0 {
		if err := testDB.Raw(
			`insert into public.milestones (roadmap_id, user_id, day)
			 values (?, ?, 0) returning milestone_id`,
			f.roadmapID, userID,
		).Row().Scan(&parent); err != nil {
			t.Fatalf("seed parent milestone: %v", err)
		}
	}

	for _, day := range taskDays {
		var id string
		if err := testDB.Raw(
			`insert into public.tasks (roadmap_id, milestone_id, user_id, title, day)
			 values (?, ?, ?, ?, ?) returning task_id`,
			f.roadmapID, parent, userID, fmt.Sprintf("t%d", day), day,
		).Row().Scan(&id); err != nil {
			t.Fatalf("seed task day=%d: %v", day, err)
		}
		f.taskIDs[day] = id
	}

	return f
}

// readDate fetches one row's stored date.
func readDate(t *testing.T, table, pk, id string) time.Time {
	t.Helper()
	var got time.Time
	if err := testDB.Raw(
		fmt.Sprintf(`select date from public.%s where %s = ?`, table, pk), id,
	).Row().Scan(&got); err != nil {
		t.Fatalf("read %s.date: %v", table, err)
	}
	return got
}

// TestStartRoadmapSchedule_MatchesGoScheduleOffset is the core equivalence
// check: for every Day offset, the date Postgres writes must equal the instant
// the replaced Go expression would have produced.
func TestStartRoadmapSchedule_MatchesGoScheduleOffset(t *testing.T) {
	cases := []struct {
		name string
		tz   string
		// now is the moment the user pressed start, in their own timezone.
		now           time.Time
		milestoneDays []int
		taskDays      []int
	}{
		{
			name:          "utc plan numbered from zero",
			tz:            "UTC",
			now:           time.Date(2026, 9, 22, 19, 30, 0, 0, time.UTC),
			milestoneDays: []int{0, 7, 30},
			taskDays:      []int{0, 1, 14},
		},
		{
			// The rebasing case: nothing sits on day 0, so dayZero is the
			// plan's own minimum and the earliest item must land on the start
			// day rather than a day late.
			name:          "plan numbered from one rebases onto start day",
			tz:            "America/New_York",
			now:           time.Date(2026, 9, 22, 21, 0, 0, 0, time.UTC),
			milestoneDays: []int{1, 8},
			taskDays:      []int{1, 2, 9},
		},
		{
			// Spans the 2026-11-01 US DST end. A naive `date + interval 'n
			// days'` on a timestamptz would drift the anchor hour here.
			name:          "spans a DST transition",
			tz:            "America/New_York",
			now:           time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC),
			milestoneDays: []int{0, 5, 10},
			taskDays:      []int{0, 3, 6},
		},
		{
			// East of Greenwich, where local midnight would fall on the
			// previous UTC day — the reason ScheduleAnchorHour is midday.
			name:          "east of greenwich",
			tz:            "Asia/Tokyo",
			now:           time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC),
			milestoneDays: []int{0, 4},
			taskDays:      []int{0, 2},
		},
		{
			// A stray Day below the plan's minimum must clamp to the start day,
			// never schedule before it. dayZero here is 2, so day 3 is +1 and
			// the day-2 rows are the floor.
			name:          "stray low day clamps to start",
			tz:            "UTC",
			now:           time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
			milestoneDays: []int{2, 3},
			taskDays:      []int{2, 3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.tz)
			if err != nil {
				t.Fatalf("load location %s: %v", tc.tz, err)
			}

			userID := createUser(t, tc.tz)
			f := seedSchedulable(t, userID, tc.milestoneDays, tc.taskDays)

			// dayZero is the plan's earliest milestone Day, matching
			// types.Roadmap.MinScheduledDay. Every task here sits under the
			// first milestone at or after its day, so tasks are global and
			// their offset from the start is just day - dayZero.
			dayZero := tc.milestoneDays[0]
			for _, d := range tc.milestoneDays {
				if d < dayZero {
					dayZero = d
				}
			}

			startDate := types.ScheduleDayIn(tc.now, loc)

			if err := database.StartRoadmapSchedule(f.roadmapID, startDate, dayZero, loc); err != nil {
				t.Fatalf("StartRoadmapSchedule: %v", err)
			}

			// The oracle: what the replaced Go loop would have written.
			assertDay := func(table, pk string, ids map[int]string) {
				for day, id := range ids {
					want := startDate.AddDate(0, 0, types.ScheduleOffset(day, dayZero))
					got := readDate(t, table, pk, id)
					if !got.Equal(want) {
						t.Errorf("%s day=%d: date = %s, want %s (tz=%s dayZero=%d)",
							table, day,
							got.UTC().Format(time.RFC3339),
							want.UTC().Format(time.RFC3339),
							tc.tz, dayZero)
					}
				}
			}
			assertDay("milestones", "milestone_id", f.milestoneIDs)
			assertDay("tasks", "task_id", f.taskIDs)

			// Every scheduled item must also render on the intended local
			// calendar day — the property ScheduleAnchorHour exists to protect.
			for day, id := range f.taskIDs {
				got := readDate(t, "tasks", "task_id", id)
				wantDay := startDate.In(loc).AddDate(0, 0, types.ScheduleOffset(day, dayZero)).Format("2006-01-02")
				if gotDay := got.In(loc).Format("2006-01-02"); gotDay != wantDay {
					t.Errorf("task day=%d: local calendar day = %s, want %s", day, gotDay, wantDay)
				}
			}
		})
	}
}

// TestStartRoadmapSchedule_RecordsStartDate checks the other half of the
// transaction: the roadmap itself is marked started, which is what makes the
// status computation treat its tasks as live.
func TestStartRoadmapSchedule_RecordsStartDate(t *testing.T) {
	loc := time.UTC
	userID := createUser(t, "UTC")
	f := seedSchedulable(t, userID, []int{0, 3}, []int{0, 1})

	startDate := types.ScheduleDayIn(time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC), loc)
	if err := database.StartRoadmapSchedule(f.roadmapID, startDate, 0, loc); err != nil {
		t.Fatalf("StartRoadmapSchedule: %v", err)
	}

	var got time.Time
	if err := testDB.Raw(
		`select start_date from public.roadmaps where roadmap_id = ?`, f.roadmapID,
	).Row().Scan(&got); err != nil {
		t.Fatalf("read start_date: %v", err)
	}
	if !got.Equal(startDate) {
		t.Errorf("start_date = %s, want %s", got.UTC(), startDate.UTC())
	}

	// Unreached milestones exist, so the roadmap must not be auto-completed.
	var completed bool
	if err := testDB.Raw(
		`select completed from public.roadmaps where roadmap_id = ?`, f.roadmapID,
	).Row().Scan(&completed); err != nil {
		t.Fatalf("read completed: %v", err)
	}
	if completed {
		t.Error("roadmap marked completed despite unreached milestones")
	}
}

// TestStartRoadmapSchedule_LeavesOtherRoadmapsAlone guards the WHERE clause:
// the set-based UPDATEs must not reach rows outside the roadmap being started.
func TestStartRoadmapSchedule_LeavesOtherRoadmapsAlone(t *testing.T) {
	loc := time.UTC
	userID := createUser(t, "UTC")

	target := seedSchedulable(t, userID, []int{0}, []int{0, 2})
	other := seedSchedulable(t, userID, []int{0}, []int{0, 2})

	startDate := types.ScheduleDayIn(time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC), loc)
	if err := database.StartRoadmapSchedule(target.roadmapID, startDate, 0, loc); err != nil {
		t.Fatalf("StartRoadmapSchedule: %v", err)
	}

	n := countRows(t,
		`select count(*) from public.tasks where roadmap_id = ? and date is not null`,
		other.roadmapID)
	if n != 0 {
		t.Errorf("%d task(s) in an unrelated roadmap were dated", n)
	}
	n = countRows(t,
		`select count(*) from public.tasks where roadmap_id = ? and date is null`,
		target.roadmapID)
	if n != 0 {
		t.Errorf("%d task(s) in the started roadmap were left undated", n)
	}
}
