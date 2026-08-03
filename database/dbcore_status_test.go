package database

// Tests for status.go: resolveUserLocation, RecomputeAndSaveStatuses,
// RecomputeAndSaveStatusesByMilestone, and the daily-job UpdateAllStatuses.
// The interesting logic (which task is in_progress) lives in
// types.ComputeAllStatusesInLocation; here we pin down that its results are
// persisted correctly, that phase-owned milestones are not saved twice, and
// that the daily job only counts rows the status guard actually changed.

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dbcoreExpectTimezonePluck stubs resolveUserLocation's lookup.
func dbcoreExpectTimezonePluck(mock sqlmock.Sqlmock, tz string) {
	mock.ExpectQuery(`SELECT "timezone" FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID).
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow(tz))
}

// --- resolveUserLocation ---

func TestResolveUserLocation_EmptyUserID_UTCNoQuery(t *testing.T) {
	withMockDB(t) // zero queries: verified by ExpectationsWereMet in cleanup

	assert.Equal(t, time.UTC, resolveUserLocation(""))
}

func TestResolveUserLocation_PluckError_FallsBackToUTC(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT "timezone" FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID).
		WillReturnError(dbcoreErrDB)

	assert.Equal(t, time.UTC, resolveUserLocation(dbcoreUserID))
}

func TestResolveUserLocation_StoredTimezone_Loaded(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectTimezonePluck(mock, "America/New_York")

	loc := resolveUserLocation(dbcoreUserID)
	require.NotNil(t, loc)
	assert.Equal(t, "America/New_York", loc.String())
}

func TestResolveUserLocation_UnparseableTimezone_UTC(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectTimezonePluck(mock, "Not/AZone")

	assert.Equal(t, time.UTC, resolveUserLocation(dbcoreUserID))
}

// --- RecomputeAndSaveStatuses ---

var dbcoreStatusRoadmapCols = []string{"roadmap_id", "user_id", "goal", "completed", "start_date"}

// dbcoreExpectRecomputeLoad stubs the preloaded roadmap fetch that opens
// RecomputeAndSaveStatuses: one started roadmap, one top-level milestone,
// one incomplete task dated tomorrow (-> the current, in_progress task).
func dbcoreExpectRecomputeLoad(mock sqlmock.Sqlmock) {
	start := dbcoreMidnightUTC().AddDate(0, 0, -1)
	tomorrow := dbcoreMidnightUTC().AddDate(0, 0, 1)

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreStatusRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, start))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day", "reached", "phase_id"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 1, false, nil))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "milestone_id", "day", "completed", "date"}).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, 1, false, tomorrow))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
	dbcoreExpectTimezonePluck(mock, "UTC")
}

func TestRecomputeAndSaveStatuses_RoadmapFetchError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnError(dbcoreErrDB)

	require.ErrorIs(t, RecomputeAndSaveStatuses(dbcoreRoadmapID), dbcoreErrDB)
}

func TestRecomputeAndSaveStatuses_PersistsComputedStatuses(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRecomputeLoad(mock)

	// The first incomplete, not-overdue task becomes in_progress, and its
	// milestone follows; both computed statuses must be persisted. Writes are
	// grouped by target status, so each lands as an IN-list update.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id IN \(\$2\)`).
		WithArgs("in_progress", dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "milestones" SET "status"=\$1 WHERE milestone_id IN \(\$2\)`).
		WithArgs("in_progress", dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, RecomputeAndSaveStatuses(dbcoreRoadmapID))
}

// Rows whose computed status already matches what's stored must not be
// written at all — that's what keeps a step completion from costing one round
// trip per task in the roadmap. With no changes there is no transaction either.
func TestRecomputeAndSaveStatuses_UnchangedStatuses_NoWrites(t *testing.T) {
	mock := withMockDB(t)
	start := dbcoreMidnightUTC().AddDate(0, 0, -1)
	tomorrow := dbcoreMidnightUTC().AddDate(0, 0, 1)

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreStatusRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, start))
	// Stored statuses already equal what ComputeAllStatuses will produce.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day", "reached", "phase_id", "status"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 1, false, nil, "in_progress"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "milestone_id", "day", "completed", "date", "status"}).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, 1, false, tomorrow, "in_progress"))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
	dbcoreExpectTimezonePluck(mock, "UTC")

	// No ExpectBegin: any UPDATE (or even an empty transaction) fails the run.
	require.NoError(t, RecomputeAndSaveStatuses(dbcoreRoadmapID))
}

// Tasks landing on the same status share a single UPDATE rather than one per
// row, which is what bounds the statement count on a large roadmap.
func TestRecomputeAndSaveStatuses_SameStatusRowsShareOneUpdate(t *testing.T) {
	mock := withMockDB(t)
	start := dbcoreMidnightUTC().AddDate(0, 0, -1)
	tomorrow := dbcoreMidnightUTC().AddDate(0, 0, 1)
	const secondTaskID = "77777777-0000-0000-0000-000000000002"
	const thirdTaskID = "77777777-0000-0000-0000-000000000003"

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreStatusRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, start))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day", "reached", "phase_id"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 1, false, nil))
	// First task is the current one (in_progress); the two behind it are both
	// not_started and must collapse into one statement.
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "milestone_id", "day", "completed", "date"}).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, 1, false, tomorrow).
			AddRow(secondTaskID, dbcoreRoadmapID, dbcoreMilestoneID, 2, false, tomorrow).
			AddRow(thirdTaskID, dbcoreRoadmapID, dbcoreMilestoneID, 3, false, tomorrow))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
	dbcoreExpectTimezonePluck(mock, "UTC")

	// Two task statements for three tasks, ordered by status name
	// ("in_progress" before "not_started").
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id IN \(\$2\)`).
		WithArgs("in_progress", dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id IN \(\$2,\$3\)`).
		WithArgs("not_started", secondTaskID, thirdTaskID).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE "milestones" SET "status"=\$1 WHERE milestone_id IN \(\$2\)`).
		WithArgs("in_progress", dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, RecomputeAndSaveStatuses(dbcoreRoadmapID))
}

func TestRecomputeAndSaveStatuses_UpdateErrorRollsBack(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRecomputeLoad(mock)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id IN \(\$2\)`).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	require.ErrorIs(t, RecomputeAndSaveStatuses(dbcoreRoadmapID), dbcoreErrDB)
}

func TestRecomputeAndSaveStatuses_PhaseOwnedMilestoneNotSavedTwice(t *testing.T) {
	mock := withMockDB(t)
	start := dbcoreMidnightUTC().AddDate(0, 0, -1)

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreStatusRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, start))
	// The same milestone appears both in the top-level Milestones preload
	// (queried by roadmap_id) and under its phase.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day", "reached", "phase_id"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 1, false, dbcorePhaseID))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id", "roadmap_id", "order_index"}).
			AddRow(dbcorePhaseID, dbcoreRoadmapID, 1))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."phase_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcorePhaseID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day", "reached", "phase_id"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 1, false, dbcorePhaseID))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))
	dbcoreExpectTimezonePluck(mock, "UTC")

	// Exactly ONE milestone status UPDATE (under its phase) plus the phase
	// UPDATE, and the milestone id appears exactly once in the IN list. The
	// top-level loop must skip the phase-owned milestone; collecting it twice
	// would widen the IN list and fail the arg match.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "status"=\$1 WHERE milestone_id IN \(\$2\)`).
		WithArgs(sqlmock.AnyArg(), dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "phases" SET "status"=\$1 WHERE phase_id IN \(\$2\)`).
		WithArgs(sqlmock.AnyArg(), dbcorePhaseID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, RecomputeAndSaveStatuses(dbcoreRoadmapID))
}

// --- RecomputeAndSaveStatusesByMilestone ---

func TestRecomputeAndSaveStatusesByMilestone_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))

	require.Error(t, RecomputeAndSaveStatusesByMilestone(dbcoreMilestoneID))
}

func TestRecomputeAndSaveStatusesByMilestone_ChainsIntoRoadmapRecompute(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID))
	// Delegation proof: the roadmap SELECT fires with the milestone's
	// roadmap_id. Error it to terminate cheaply.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnError(dbcoreErrDB)

	require.ErrorIs(t, RecomputeAndSaveStatusesByMilestone(dbcoreMilestoneID), dbcoreErrDB)
}

// --- UpdateAllStatuses ---

func TestUpdateAllStatuses_FindError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps"`).
		WillReturnError(dbcoreErrDB)

	tasks, milestones, err := UpdateAllStatuses()
	require.ErrorIs(t, err, dbcoreErrDB)
	assert.Zero(t, tasks)
	assert.Zero(t, milestones)
}

func TestUpdateAllStatuses_NoRoadmaps_SingleSelect(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps"`).
		WillReturnRows(sqlmock.NewRows(dbcoreStatusRoadmapCols))

	tasks, milestones, err := UpdateAllStatuses()
	require.NoError(t, err)
	assert.Zero(t, tasks)
	assert.Zero(t, milestones)
}

// dbcoreExpectDailyJobLoad stubs the all-roadmaps load with one started
// roadmap and nTasks incomplete tasks (day i+1, dated i+1 days out) under
// one top-level milestone.
func dbcoreExpectDailyJobLoad(mock sqlmock.Sqlmock, nTasks int) {
	start := dbcoreMidnightUTC().AddDate(0, 0, -1)

	mock.ExpectQuery(`SELECT \* FROM "roadmaps"`).
		WillReturnRows(sqlmock.NewRows(dbcoreStatusRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, start))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day", "reached", "phase_id", "status"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 1, false, nil, "in_progress"))
	taskRows := sqlmock.NewRows([]string{"task_id", "roadmap_id", "milestone_id", "day", "completed", "date"})
	ids := []string{dbcoreTaskID, dbcoreTaskID2}
	for i := 0; i < nTasks; i++ {
		taskRows.AddRow(ids[i], dbcoreRoadmapID, dbcoreMilestoneID, i+1, false, dbcoreMidnightUTC().AddDate(0, 0, i+1))
	}
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(taskRows)
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
	dbcoreExpectTimezonePluck(mock, "UTC")
}

func TestUpdateAllStatuses_CountsOnlyChangedRows(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectDailyJobLoad(mock, 1)

	// Task UPDATE guarded by status != ?: RowsAffected 1 -> counted.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id = \$2 AND status != \$3`).
		WithArgs("in_progress", dbcoreTaskID, "in_progress").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Milestone already in_progress: guard matches no rows -> not counted.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "status"=\$1 WHERE milestone_id = \$2 AND status != \$3`).
		WithArgs("in_progress", dbcoreMilestoneID, "in_progress").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	tasksUpdated, milestonesUpdated, err := UpdateAllStatuses()
	require.NoError(t, err)
	assert.Equal(t, int64(1), tasksUpdated)
	assert.Equal(t, int64(0), milestonesUpdated)
}

func TestUpdateAllStatuses_TaskUpdateErrorMidLoop_PartialCounts(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectDailyJobLoad(mock, 2)

	// First task update succeeds and is counted...
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id = \$2 AND status != \$3`).
		WithArgs("in_progress", dbcoreTaskID, "in_progress").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// ...the second fails: partial counts plus the error come back.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "status"=\$1 WHERE task_id = \$2 AND status != \$3`).
		WithArgs("not_started", dbcoreTaskID2, "not_started").
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	tasksUpdated, milestonesUpdated, err := UpdateAllStatuses()
	require.ErrorIs(t, err, dbcoreErrDB)
	assert.Equal(t, int64(1), tasksUpdated)
	assert.Equal(t, int64(0), milestonesUpdated)
}
