package database

// Tests for the completion cascades in task.go: UpdateTask, UpdateStep,
// CompleteAllSteps, CreateStep, DeleteStep. The forward cascade (last step
// done -> task -> milestone -> roadmap) and the reverse cascade (uncheck a
// step -> un-complete everything above) are the core product behavior of the
// Today tab, so the tests pin down exactly which UPDATEs fire and with which
// flag values.

import (
	"testing"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dbcoreTaskCols = []string{
	"task_id", "roadmap_id", "milestone_id", "user_id", "title", "description", "status", "day", "completed", "date",
}

// dbcoreTaskRow returns a single-task result set.
func dbcoreTaskRow(title string, day int, completed bool, date any) *sqlmock.Rows {
	return sqlmock.NewRows(dbcoreTaskCols).
		AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, dbcoreUserID, title, "", "not_started", day, completed, date)
}

// dbcoreExpectUpdateTaskFetch stubs UpdateTask's initial user-scoped fetch
// (task row + empty Steps preload).
func dbcoreExpectUpdateTaskFetch(mock sqlmock.Sqlmock, title string, day int, completed bool) {
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow(title, day, completed, nil))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))
}

// dbcoreExpectFinalTaskReload stubs UpdateTask's trailing unscoped reload.
// The dest struct already carries its primary key from the first fetch, so
// gorm appends a pk condition to the Where clause.
func dbcoreExpectFinalTaskReload(mock sqlmock.Sqlmock, title string, completed bool) {
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE task_id = \$1 AND "tasks"\."task_id" = \$2`).
		WithArgs(dbcoreTaskID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow(title, 1, completed, nil))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))
}

// dbcoreExpectStepCounts stubs the two shouldCompleteTask COUNT queries.
func dbcoreExpectStepCounts(mock sqlmock.Sqlmock, total, incomplete int64) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(dbcoreCountRows(total))
	if total > 0 {
		mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1 AND status = \$2`).
			WithArgs(dbcoreTaskID, "incomplete").
			WillReturnRows(dbcoreCountRows(incomplete))
	}
}

// --- UpdateTask ---

func TestUpdateTask_MissingUserID_ReturnsZeroTask(t *testing.T) {
	withMockDB(t) // no queries expected

	got := UpdateTask(dbcoreCtx(""), dbcoreTaskID, types.Task{Title: "x"})
	assert.Equal(t, types.Task{}, got)
}

func TestUpdateTask_FetchError_ReturnsZeroTask(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnError(dbcoreErrDB)

	got := UpdateTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Task{Title: "x"})
	assert.Equal(t, types.Task{}, got)
}

func TestUpdateTask_TitleAndDayOnly_NoMilestoneRecompute(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateTaskFetch(mock, "Old title", 1, false)

	// Map keys sorted alphabetically: day, title. No "completed" key -> no
	// RecomputeAndSaveStatusesByMilestone (no milestones SELECT expected).
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "day"=\$1,"title"=\$2 WHERE "task_id" = \$3`).
		WithArgs(4, "New title", dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectStepCounts(mock, 0, 0) // no steps -> no auto-complete
	dbcoreExpectFinalTaskReload(mock, "New title", false)

	got := UpdateTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Task{Title: "New title", Day: 4})
	assert.Equal(t, "New title", got.Title)
}

func TestUpdateTask_CompletedFlip_TriggersMilestoneRecompute(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateTaskFetch(mock, "T", 1, false)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "completed"=\$1 WHERE "task_id" = \$2`).
		WithArgs(true, dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// completed changed -> RecomputeAndSaveStatusesByMilestone runs. Its
	// milestone SELECT errors; UpdateTask only logs and continues.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnError(dbcoreErrDB)

	dbcoreExpectStepCounts(mock, 1, 1) // still-incomplete step -> no auto-complete
	dbcoreExpectFinalTaskReload(mock, "T", true)

	got := UpdateTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Task{Completed: true})
	assert.True(t, got.Completed)
}

func TestUpdateTask_NoOpPayload_IssuesNoUpdate(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateTaskFetch(mock, "T", 1, false)

	// All-zero payload with unchanged completed: no UPDATE at all — flow
	// falls through to shouldCompleteTask and the final reload.
	dbcoreExpectStepCounts(mock, 0, 0)
	dbcoreExpectFinalTaskReload(mock, "T", false)

	got := UpdateTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Task{})
	assert.Equal(t, "T", got.Title)
}

// --- UpdateStep ---

var dbcoreStepCols = []string{"step_id", "task_id", "description", "completed", "status", "number"}

// dbcoreExpectUpdateStepFetches stubs UpdateStep's task-ownership check and
// step fetch.
func dbcoreExpectUpdateStepFetches(mock sqlmock.Sqlmock, taskCompleted bool, stepCompleted bool, stepStatus string) {
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("T", 1, taskCompleted, nil))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE task_id = \$1 AND step_id = \$2`).
		WithArgs(dbcoreTaskID, dbcoreStepID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreStepCols).
			AddRow(dbcoreStepID, dbcoreTaskID, "step", stepCompleted, stepStatus, 1))
}

func TestUpdateStep_StatusCompleted_SyncsCompletedTrue(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateStepFetches(mock, false, false, "incomplete")

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE "step_id" = \$3`).
		WithArgs(true, "completed", dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectStepCounts(mock, 2, 1) // sibling still incomplete -> no cascade

	got := UpdateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID, types.Step{Status: "completed"})
	assert.True(t, got.Completed)
	assert.Equal(t, "completed", got.Status)
}

func TestUpdateStep_StatusSkipped_CountsAsDone(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateStepFetches(mock, false, false, "incomplete")

	// skipped syncs completed=false ...
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE "step_id" = \$3`).
		WithArgs(false, "skipped", dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// ... yet still counts as done: the forward-cascade check runs (these
	// COUNTs firing is the assertion), it just finds another incomplete step.
	dbcoreExpectStepCounts(mock, 2, 1)

	got := UpdateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID, types.Step{Status: "skipped"})
	assert.False(t, got.Completed)
	assert.Equal(t, "skipped", got.Status)
}

func TestUpdateStep_LegacyCompletedBool_SyncsStatus(t *testing.T) {
	mock := withMockDB(t)
	// Existing step is completed; legacy payload flips Completed to false.
	dbcoreExpectUpdateStepFetches(mock, false, true, "completed")

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE "step_id" = \$3`).
		WithArgs(false, "incomplete", dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Step not done and task not completed -> no reverse cascade writes.
	got := UpdateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID, types.Step{Completed: false})
	assert.False(t, got.Completed)
	assert.Equal(t, "incomplete", got.Status, "legacy bool flip syncs status")
}

func TestUpdateStep_LegacyCompletedTrue_SyncsStatusCompleted(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateStepFetches(mock, false, false, "incomplete")

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE "step_id" = \$3`).
		WithArgs(true, "completed", dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectStepCounts(mock, 2, 1)

	got := UpdateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID, types.Step{Completed: true})
	assert.Equal(t, "completed", got.Status)
}

func TestUpdateStep_ReverseCascade_UnchecksTaskMilestoneRoadmap(t *testing.T) {
	mock := withMockDB(t)
	// Task is completed; unchecking a completed step must un-complete the
	// whole chain above it.
	dbcoreExpectUpdateStepFetches(mock, true, true, "completed")

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE "step_id" = \$3`).
		WithArgs(false, "incomplete", dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "completed"=\$1 WHERE "task_id" = \$2`).
		WithArgs(false, dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "reached"=\$1 WHERE milestone_id = \$2`).
		WithArgs(false, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "completed"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(false, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Post-cascade recompute failure is logged, not returned.
	dbcoreExpectRecomputeSwallowed(mock)

	got := UpdateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID, types.Step{Status: "incomplete"})
	assert.False(t, got.Completed)
}

func TestUpdateStep_ForwardCascade_CompletesTaskMilestoneRoadmap(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateStepFetches(mock, false, false, "incomplete")

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE "step_id" = \$3`).
		WithArgs(true, "completed", dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Last step done -> task completes.
	dbcoreExpectStepCounts(mock, 1, 0)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "completed"=\$1 WHERE "task_id" = \$2`).
		WithArgs(true, dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Milestone completes when all its tasks are done.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1 AND completed = \$2`).
		WithArgs(dbcoreMilestoneID, false).
		WillReturnRows(dbcoreCountRows(0))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "reached"=\$1 WHERE milestone_id = \$2`).
		WithArgs(true, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Roadmap completes when no incomplete milestones remain.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "milestones" WHERE roadmap_id = \$1 AND reached = \$2`).
		WithArgs(dbcoreRoadmapID, false).
		WillReturnRows(dbcoreCountRows(0))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "completed"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(true, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRecomputeSwallowed(mock)

	got := UpdateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID, types.Step{Status: "completed"})
	assert.True(t, got.Completed)
	assert.Equal(t, "completed", got.Status)
}

// --- CreateStep ---

func TestCreateStep_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := CreateStep(dbcoreCtx(""), dbcoreTaskID, types.Step{})
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCreateStep_TaskNotOwned(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	_, err := CreateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Step{})
	require.Error(t, err)
	assert.Equal(t, "task not found or access denied", err.Error())
}

func TestCreateStep_InsertErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("T", 1, false, nil))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "steps"`).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	_, err := CreateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Step{StepID: dbcoreStepID, Description: "d"})
	require.ErrorIs(t, err, dbcoreErrDB)
}

func TestCreateStep_Success_ForcesPathTaskID(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("T", 1, false, nil))

	// The step arrives claiming a different task_id; CreateStep must force
	// the path taskID onto the insert (2nd arg).
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "steps" \("task_id","description","completed","status","number","step_id"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\)`).
		WithArgs(dbcoreTaskID, "wash brushes", false, "incomplete", 2, dbcoreStepID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}).AddRow(dbcoreStepID))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE step_id = \$1`).
		WithArgs(dbcoreStepID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreStepCols).
			AddRow(dbcoreStepID, dbcoreTaskID, "wash brushes", false, "incomplete", 2))

	got, err := CreateStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, types.Step{
		StepID:      dbcoreStepID,
		TaskID:      "some-other-task",
		Description: "wash brushes",
		Status:      "incomplete",
		Number:      2,
	})
	require.NoError(t, err)
	assert.Equal(t, dbcoreTaskID, got.TaskID)
	assert.Equal(t, "wash brushes", got.Description)
}

// --- DeleteStep ---

func TestDeleteStep_TaskOwnershipFails(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	err := DeleteStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID)
	require.Error(t, err)
	assert.Equal(t, "task not found or access denied", err.Error())
}

func TestDeleteStep_DeleteErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("T", 1, false, nil))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "steps" WHERE task_id = \$1 AND step_id = \$2`).
		WithArgs(dbcoreTaskID, dbcoreStepID).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	err := DeleteStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID)
	require.ErrorIs(t, err, dbcoreErrDB)
}

func TestDeleteStep_Success(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("T", 1, false, nil))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "steps" WHERE task_id = \$1 AND step_id = \$2`).
		WithArgs(dbcoreTaskID, dbcoreStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteStep(dbcoreCtx(dbcoreUserID), dbcoreTaskID, dbcoreStepID))
}

// --- CompleteAllSteps ---

func TestCompleteAllSteps_TaskNotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	_, err := CompleteAllSteps(dbcoreCtx(dbcoreUserID), dbcoreTaskID)
	require.Error(t, err)
	assert.Equal(t, "task not found", err.Error())
}

func TestCompleteAllSteps_FetchErrorPassthrough(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnError(dbcoreErrDB)

	_, err := CompleteAllSteps(dbcoreCtx(dbcoreUserID), dbcoreTaskID)
	require.ErrorIs(t, err, dbcoreErrDB, "non-NotFound fetch errors pass through unmapped")
}

func TestCompleteAllSteps_HappyPath_ReturnsCountAndCascades(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("T", 1, false, nil))

	// Bulk-complete only steps still incomplete; RowsAffected becomes the
	// returned count.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1 WHERE task_id = \$2 AND completed = \$3`).
		WithArgs(true, dbcoreTaskID, false).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	// Task marked complete unconditionally.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "completed"=\$1 WHERE "task_id" = \$2`).
		WithArgs(true, dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Milestone cascade fires (counts say all tasks done)...
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1 AND completed = \$2`).
		WithArgs(dbcoreMilestoneID, false).
		WillReturnRows(dbcoreCountRows(0))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "reached"=\$1 WHERE milestone_id = \$2`).
		WithArgs(true, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// ...but the roadmap cascade does not (another milestone incomplete).
	mock.ExpectQuery(`SELECT count\(\*\) FROM "milestones" WHERE roadmap_id = \$1 AND reached = \$2`).
		WithArgs(dbcoreRoadmapID, false).
		WillReturnRows(dbcoreCountRows(1))

	// Post-cascade recompute failure is logged; (2, nil) still returned.
	dbcoreExpectRecomputeSwallowed(mock)

	count, err := CompleteAllSteps(dbcoreCtx(dbcoreUserID), dbcoreTaskID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}
