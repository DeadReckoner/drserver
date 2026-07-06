package jobs

import (
	"fmt"
	"testing"

	"dr/server/internal/testutil"
	"dr/server/notifications"

	"github.com/DATA-DOG/go-sqlmock"
)

// expectNextMilestoneMaintenance stubs the upsert + prune statements that run
// at the top of every scan.
func expectNextMilestoneMaintenance(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`INSERT INTO next_milestone_nudges`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM next_milestone_nudges n`).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func dueRows(mock sqlmock.Sqlmock) *sqlmock.Rows {
	return mock.NewRows([]string{"roadmap_id", "user_id", "stage", "goal", "age_hours", "detected_on"})
}

func TestScanNextMilestone_FirstReminderAt24h(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	expectNextMilestoneMaintenance(mock)

	mock.ExpectQuery(`SELECT n\.roadmap_id, n\.user_id, n\.stage, r\.goal`).
		WillReturnRows(dueRows(mock).
			AddRow("roadmap-A", "user-1", 0, "Run a marathon", 25.0, "2026-07-04"))

	mock.ExpectExec(`UPDATE next_milestone_nudges SET stage = \$1 WHERE roadmap_id = \$2`).
		WithArgs(1, "roadmap-A").
		WillReturnResult(sqlmock.NewResult(0, 1))

	var dispatched []notifications.DispatchOptions
	scanNextMilestoneNudgesWith(db, func(opts notifications.DispatchOptions) notifications.Decision {
		dispatched = append(dispatched, opts)
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	})()

	if len(dispatched) != 1 {
		t.Fatalf("dispatched %d times, want 1", len(dispatched))
	}
	opts := dispatched[0]
	if opts.NotificationType != "follow_on_milestone_ready" {
		t.Errorf("NotificationType = %q, want follow_on_milestone_ready", opts.NotificationType)
	}
	if opts.Category != notifications.CategoryMyGoals {
		t.Errorf("Category = %q, want my_goals", opts.Category)
	}
	if opts.UserID != "user-1" {
		t.Errorf("UserID = %q, want user-1", opts.UserID)
	}
	wantKey := "roadmap-A:next_milestone:1:2026-07-04"
	if opts.DedupKey != wantKey {
		t.Errorf("DedupKey = %q, want %q", opts.DedupKey, wantKey)
	}
	if opts.Data["stage"] != 1 {
		t.Errorf("Data.stage = %v, want 1", opts.Data["stage"])
	}
}

func TestScanNextMilestone_SkipsToHighestDueStage(t *testing.T) {
	// Row is 8+ days old with no reminders sent (job downtime): the user gets
	// ONE reminder at stage 3, not a burst of three.
	db, mock := testutil.NewSqlmockDBRegex(t)
	expectNextMilestoneMaintenance(mock)

	mock.ExpectQuery(`SELECT n\.roadmap_id, n\.user_id, n\.stage, r\.goal`).
		WillReturnRows(dueRows(mock).
			AddRow("roadmap-A", "user-1", 0, "Run a marathon", 200.0, "2026-06-27"))

	mock.ExpectExec(`UPDATE next_milestone_nudges SET stage = \$1 WHERE roadmap_id = \$2`).
		WithArgs(3, "roadmap-A").
		WillReturnResult(sqlmock.NewResult(0, 1))

	var dispatched []notifications.DispatchOptions
	scanNextMilestoneNudgesWith(db, func(opts notifications.DispatchOptions) notifications.Decision {
		dispatched = append(dispatched, opts)
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	})()

	if len(dispatched) != 1 {
		t.Fatalf("dispatched %d times, want exactly 1 (highest due stage only)", len(dispatched))
	}
	if dispatched[0].Data["stage"] != 3 {
		t.Errorf("stage = %v, want 3", dispatched[0].Data["stage"])
	}
}

func TestScanNextMilestone_AlreadySentStageNotRepeated(t *testing.T) {
	// 30h old and stage 1 already sent: nothing new is due until 72h.
	db, mock := testutil.NewSqlmockDBRegex(t)
	expectNextMilestoneMaintenance(mock)

	mock.ExpectQuery(`SELECT n\.roadmap_id, n\.user_id, n\.stage, r\.goal`).
		WillReturnRows(dueRows(mock).
			AddRow("roadmap-A", "user-1", 1, "Run a marathon", 30.0, "2026-07-04"))

	dispatched := 0
	scanNextMilestoneNudgesWith(db, func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	})()

	if dispatched != 0 {
		t.Fatalf("dispatched %d times, want 0", dispatched)
	}
}

func TestScanNextMilestone_EscalatesTo72hThenWeek(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	expectNextMilestoneMaintenance(mock)

	mock.ExpectQuery(`SELECT n\.roadmap_id, n\.user_id, n\.stage, r\.goal`).
		WillReturnRows(dueRows(mock).
			AddRow("roadmap-A", "user-1", 1, "Run a marathon", 80.0, "2026-07-01").
			AddRow("roadmap-B", "user-2", 2, "Learn Spanish", 170.0, "2026-06-28"))

	mock.ExpectExec(`UPDATE next_milestone_nudges SET stage = \$1 WHERE roadmap_id = \$2`).
		WithArgs(2, "roadmap-A").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE next_milestone_nudges SET stage = \$1 WHERE roadmap_id = \$2`).
		WithArgs(3, "roadmap-B").
		WillReturnResult(sqlmock.NewResult(0, 1))

	var stages []any
	scanNextMilestoneNudgesWith(db, func(opts notifications.DispatchOptions) notifications.Decision {
		stages = append(stages, opts.Data["stage"])
		return notifications.Decision{Send: true, Reason: notifications.ReasonOK}
	})()

	if fmt.Sprint(stages) != "[2 3]" {
		t.Fatalf("dispatched stages = %v, want [2 3]", stages)
	}
}

func TestScanNextMilestone_NoDueRowsDispatchesNothing(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	expectNextMilestoneMaintenance(mock)

	mock.ExpectQuery(`SELECT n\.roadmap_id, n\.user_id, n\.stage, r\.goal`).
		WillReturnRows(dueRows(mock))

	dispatched := 0
	scanNextMilestoneNudgesWith(db, func(_ notifications.DispatchOptions) notifications.Decision {
		dispatched++
		return notifications.Decision{}
	})()

	if dispatched != 0 {
		t.Fatalf("dispatched %d times, want 0", dispatched)
	}
}

func TestNextMilestoneDueStage(t *testing.T) {
	cases := []struct {
		ageHours float64
		want     int
	}{
		{23.9, 0},
		{24, 1},
		{71.9, 1},
		{72, 2},
		{167.9, 2},
		{168, 3},
		{1000, 3},
	}
	for _, tc := range cases {
		got := nextMilestoneDue{AgeHours: tc.ageHours}.dueStage()
		if got != tc.want {
			t.Errorf("dueStage(age=%vh) = %d, want %d", tc.ageHours, got, tc.want)
		}
	}
}
