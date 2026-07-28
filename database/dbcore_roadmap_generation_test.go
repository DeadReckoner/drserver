package database

// Tests for the AI-generation persistence paths in roadmap.go:
// UpdateRoadmapWithGeneration's in-memory UUID/FK/status wiring and
// SaveNextMilestone's task anchoring, adjustment application, and
// spike/risk replacement semantics.

import (
	"database/sql/driver"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UpdateRoadmapWithGeneration ---

func TestUpdateRoadmapWithGeneration_WiresEntitiesAndReturnsSaveError(t *testing.T) {
	mock := withMockDB(t)

	// Let the FullSaveAssociations save fail on its first statement (the
	// roadmap UPDATE) so the tail is a single expectation; the in-memory
	// mutations have already happened by then and are asserted below.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET`).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	presetMilestoneID := "eeeeeeee-0000-0000-0000-000000000002"
	rm := types.Roadmap{
		RoadmapID:        dbcoreRoadmapID,
		UserID:           dbcoreUserID,
		Goal:             "g",
		GenerationStatus: "completed",
		Phases: []types.Phase{{
			Name: "Phase 1",
			Milestones: []types.Milestone{{
				SubGoal: "m",
				Spikes:  []types.Spike{{Description: "spike"}},
				Risks:   []types.Risk{{Description: "risk"}},
				Tasks: []types.Task{{
					Title:  "t",
					Status: "in_progress", // pre-set status preserved
					Steps:  []types.Step{{Description: "s"}},
				}},
			}},
		}},
		Milestones: []types.Milestone{{MilestoneID: presetMilestoneID, SubGoal: "top"}},
	}

	err := UpdateRoadmapWithGeneration(t.Context(), &rm)
	require.ErrorIs(t, err, dbcoreErrDB, "save error is returned")

	phase := rm.Phases[0]
	dbcoreRequireUUID(t, phase.PhaseID, "phase id")
	assert.Equal(t, dbcoreRoadmapID, phase.RoadmapID)
	assert.Equal(t, "not_started", phase.Status, "empty phase status defaulted")

	m := phase.Milestones[0]
	dbcoreRequireUUID(t, m.MilestoneID, "milestone id")
	assert.Equal(t, dbcoreRoadmapID, m.RoadmapID)
	assert.Equal(t, dbcoreUserID, m.UserID, "UserID propagated from the roadmap")
	require.NotNil(t, m.PhaseID)
	assert.Equal(t, phase.PhaseID, *m.PhaseID)
	assert.Equal(t, "not_started", m.Status)

	dbcoreRequireUUID(t, m.Spikes[0].SpikeID, "spike id")
	assert.Equal(t, m.MilestoneID, m.Spikes[0].MilestoneID)
	dbcoreRequireUUID(t, m.Risks[0].RiskID, "risk id")
	assert.Equal(t, m.MilestoneID, m.Risks[0].MilestoneID)

	task := m.Tasks[0]
	dbcoreRequireUUID(t, task.TaskID, "task id")
	assert.Equal(t, m.MilestoneID, task.MilestoneID)
	assert.Equal(t, dbcoreRoadmapID, task.RoadmapID)
	assert.Equal(t, dbcoreUserID, task.UserID)
	assert.Equal(t, "in_progress", task.Status, "pre-set status preserved, not clobbered")
	dbcoreRequireUUID(t, task.Steps[0].StepID, "step id")
	assert.Equal(t, task.TaskID, task.Steps[0].TaskID)

	top := rm.Milestones[0]
	assert.Equal(t, presetMilestoneID, top.MilestoneID, "pre-set top-level MilestoneID kept")
	assert.Equal(t, dbcoreUserID, top.UserID)
	assert.Equal(t, "not_started", top.Status)
}

// --- SaveNextMilestone ---

// dbcoreUTCMidnight mirrors SaveNextMilestone's anchor normalization: it
// derives "today" from time.Now().UTC(), unlike RestartRoadmap/
// RealignRoadmapDates/GetOverdueTasks which use the server's LOCAL date
// pinned to UTC (see dbcoreMidnightUTC). The two disagree by a day whenever
// the server's local date differs from the UTC date (e.g. every evening in
// US timezones) — a production inconsistency this test suite documents by
// needing two helpers.
func dbcoreUTCMidnight() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// dbcoreExpectNextMilestoneFetch stubs the opening milestone load.
func dbcoreExpectNextMilestoneFetch(mock sqlmock.Sqlmock, date any) {
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal", "date"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, dbcoreUserID, "sg", date))
}

// dbcoreTaskInsertArgs returns the 14 WithArgs values for one inserted task
// row (column order: roadmap_id, milestone_id, user_id, title, description,
// status, rationale, est_minutes, dependencies, confidence, day, completed,
// date, task_id).
func dbcoreTaskInsertArgs(title string, day int, date time.Time) []driver.Value {
	return []driver.Value{
		dbcoreRoadmapID, dbcoreMilestoneID, dbcoreUserID, title, "", "not_started",
		"", 0, nil, "", day, false, date, sqlmock.AnyArg(),
	}
}

func TestSaveNextMilestone_FetchErrorReturnedBeforeTransaction(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnError(dbcoreErrDB)

	err := SaveNextMilestone(t.Context(), dbcoreMilestoneID, nil, []types.Task{{Title: "t"}})
	require.ErrorIs(t, err, dbcoreErrDB, "no transaction expected after a fetch failure")
}

func TestSaveNextMilestone_PastMilestone_AnchorsTasksAtToday(t *testing.T) {
	mock := withMockDB(t)
	today := dbcoreUTCMidnight()
	// Milestone planned 5 days ago -> anchor snaps forward to today.
	dbcoreExpectNextMilestoneFetch(mock, today.AddDate(0, 0, -5))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 2))
	// Day=1 -> today; Day=3 -> today+2d. Batch insert: 14 args per row.
	args := append(dbcoreTaskInsertArgs("first", 1, today), dbcoreTaskInsertArgs("second", 3, today.AddDate(0, 0, 2))...)
	mock.ExpectQuery(`INSERT INTO "tasks"`).
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow(dbcoreTaskID).AddRow(dbcoreTaskID2))
	// Milestone re-dated to the earliest new task date (today).
	mock.ExpectExec(`UPDATE "milestones" SET "date"=\$1 WHERE milestone_id = \$2`).
		WithArgs(today, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Post-commit recompute failure is logged and nil is still returned.
	dbcoreExpectRecomputeSwallowed(mock)

	err := SaveNextMilestone(t.Context(), dbcoreMilestoneID, nil, []types.Task{
		{Title: "first", Day: 1},
		{Title: "second", Day: 3},
	})
	require.NoError(t, err)
}

func TestSaveNextMilestone_FutureMilestone_AnchorsAtMilestoneDate_ClampsNonPositiveDay(t *testing.T) {
	mock := withMockDB(t)
	future := dbcoreUTCMidnight().AddDate(0, 0, 5)
	dbcoreExpectNextMilestoneFetch(mock, future)

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Day=0 clamps the offset to 0 -> the task lands exactly on the future
	// milestone date.
	mock.ExpectQuery(`INSERT INTO "tasks"`).
		WithArgs(dbcoreTaskInsertArgs("only", 0, future)...).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow(dbcoreTaskID))
	mock.ExpectExec(`UPDATE "milestones" SET "date"=\$1 WHERE milestone_id = \$2`).
		WithArgs(future, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRecomputeSwallowed(mock)

	err := SaveNextMilestone(t.Context(), dbcoreMilestoneID, nil, []types.Task{{Title: "only", Day: 0}})
	require.NoError(t, err)
}

func TestSaveNextMilestone_AdjustmentsSubGoalOnly_SparseUpdateMap(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectNextMilestoneFetch(mock, nil)

	mock.ExpectBegin()
	// Only sub_goal is set; sentinel-empty fields are excluded from the map.
	mock.ExpectExec(`UPDATE "milestones" SET "sub_goal"=\$1 WHERE milestone_id = \$2`).
		WithArgs("Sharper subgoal", dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Empty Spikes/Risks arrays leave existing rows untouched (no DELETE) —
	// straight to the task delete. Empty tasks -> no INSERT and no re-date.
	mock.ExpectExec(`DELETE FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRecomputeSwallowed(mock)

	err := SaveNextMilestone(t.Context(), dbcoreMilestoneID,
		&types.MilestoneAdjustments{Adjusted: true, SubGoal: "Sharper subgoal"}, nil)
	require.NoError(t, err)
}

func TestSaveNextMilestone_ReplacesSpikesAndRisks(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectNextMilestoneFetch(mock, nil)

	mock.ExpectBegin()
	// No scalar overrides -> no milestone UPDATE. Non-empty arrays replace:
	// DELETE by milestone_id then INSERT with fresh UUIDs.
	mock.ExpectExec(`DELETE FROM "spikes" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`INSERT INTO "spikes"`).
		WithArgs(dbcoreMilestoneID, "try the API", 2, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"spike_id"}).AddRow("11111111-2222-3333-4444-555555555555"))
	mock.ExpectExec(`DELETE FROM "risks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`INSERT INTO "risks"`).
		WithArgs(dbcoreMilestoneID, "rate limits", "high", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"risk_id"}).AddRow("11111111-2222-3333-4444-666666666666"))
	mock.ExpectExec(`DELETE FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	dbcoreExpectRecomputeSwallowed(mock)

	err := SaveNextMilestone(t.Context(), dbcoreMilestoneID, &types.MilestoneAdjustments{
		Adjusted: true,
		Spikes:   []types.Spike{{Description: "try the API", TimeboxDays: 2}},
		Risks:    []types.Risk{{Description: "rate limits", Severity: "high"}},
	}, nil)
	require.NoError(t, err)
}

func TestSaveNextMilestone_MidTransactionErrorRollsBack(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectNextMilestoneFetch(mock, nil)

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	err := SaveNextMilestone(t.Context(), dbcoreMilestoneID, nil, nil)
	require.ErrorIs(t, err, dbcoreErrDB)
}
