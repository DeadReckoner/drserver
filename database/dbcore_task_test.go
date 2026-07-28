package database

// Tests for the task query and CRUD functions in task.go: GetMissedTasks
// (the drserver#48 404 contract), GetOverdueTasks filters, the GetTasks
// date-filter matrix, and CreateTask/DeleteTask/GetTask/GetTasksByMilestone
// guards and error paths.

import (
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- GetMissedTasks ---

func TestGetMissedTasks_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := GetMissedTasks(dbcoreCtx(""), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestGetMissedTasks_RoadmapNotOwned_ErrRecordNotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT "roadmap_id" FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(dbcoreRoadmapID, dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	_, err := GetMissedTasks(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	// Passthrough of gorm.ErrRecordNotFound lets the handler 404 instead of
	// silently returning [] (drserver#48).
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestGetMissedTasks_OwnedNoOverdue_EmptyNonNilSlice(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT "roadmap_id" FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(dbcoreRoadmapID, dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}).AddRow(dbcoreRoadmapID))
	mock.ExpectQuery(`FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND tasks\.date < \$2 AND tasks\.completed = \$3 AND roadmaps\.archived = \$4 AND tasks\.roadmap_id = \$5`).
		WithArgs(dbcoreUserID, dbcoreMidnightUTC(), false, false, dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	tasks, err := GetMissedTasks(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.NoError(t, err)
	require.NotNil(t, tasks, "handler contract: [] not null")
	assert.Empty(t, tasks)
}

// --- GetOverdueTasks ---

func TestGetOverdueTasks_MissingUserID_NoQuery(t *testing.T) {
	withMockDB(t)

	assert.Nil(t, GetOverdueTasks(dbcoreCtx(""), ""))
}

func TestGetOverdueTasks_FiltersAndForcesOverdueStatus(t *testing.T) {
	mock := withMockDB(t)
	yesterday := dbcoreMidnightUTC().AddDate(0, 0, -1)

	// WHERE args pin the whole filter set: incomplete only, unarchived
	// roadmaps only, date strictly before today-midnight, plus the optional
	// roadmap filter because roadmapID was provided.
	mock.ExpectQuery(`FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND tasks\.date < \$2 AND tasks\.completed = \$3 AND roadmaps\.archived = \$4 AND tasks\.roadmap_id = \$5 ORDER BY tasks\.date ASC`).
		WithArgs(dbcoreUserID, dbcoreMidnightUTC(), false, false, dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, dbcoreUserID, "T", "", "not_started", 1, false, yesterday))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))

	// AnnotateTaskPaths sibling queries (annotation itself is cosmetic).
	mock.ExpectQuery(`SELECT milestone_id, roadmap_id, phase_id, day, order_index FROM "milestones" WHERE roadmap_id IN \(\$1\)`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "phase_id", "day", "order_index"}))
	mock.ExpectQuery(`SELECT phase_id, roadmap_id, order_index FROM "phases" WHERE roadmap_id IN \(\$1\)`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id", "roadmap_id", "order_index"}))
	mock.ExpectQuery(`SELECT task_id, milestone_id, day FROM "tasks" WHERE roadmap_id IN \(\$1\)`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "milestone_id", "day"}))

	tasks := GetOverdueTasks(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Len(t, tasks, 1)
	assert.Equal(t, "overdue", tasks[0].Status, "every returned task is forced to overdue")
}

// --- GetTasks date-filter matrix ---

// dbcoreGetTasksBaseRegex matches the GetTasks query without a date filter.
const dbcoreGetTasksBaseRegex = `FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND roadmaps\.archived = \$2 ORDER BY tasks\.day ASC`

// dbcoreGetTasksDateRegex matches the GetTasks query with the date range.
const dbcoreGetTasksDateRegex = `FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND roadmaps\.archived = \$2 AND \(tasks\.date >= \$3 AND tasks\.date < \$4\) ORDER BY tasks\.day ASC`

func TestGetTasks_NoDates_NoDatePredicate(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(dbcoreGetTasksBaseRegex).
		WithArgs(dbcoreUserID, false).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	assert.Empty(t, GetTasks(dbcoreCtx(dbcoreUserID), "", "", ""))
}

func TestGetTasks_StartOnly_SingleDayWindow(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(dbcoreGetTasksDateRegex).
		WithArgs(dbcoreUserID, false, start, start.Add(24*time.Hour)).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	assert.Empty(t, GetTasks(dbcoreCtx(dbcoreUserID), "2026-07-01", "", ""))
}

func TestGetTasks_StartAndEnd_EndInclusiveWindow(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	// end_date + 24h: the whole end day is included.
	mock.ExpectQuery(dbcoreGetTasksDateRegex).
		WithArgs(dbcoreUserID, false, start, end.Add(24*time.Hour)).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	assert.Empty(t, GetTasks(dbcoreCtx(dbcoreUserID), "2026-07-01", "2026-07-03", ""))
}

func TestGetTasks_InvalidStartDate_FilterSilentlyDropped(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(dbcoreGetTasksBaseRegex).
		WithArgs(dbcoreUserID, false).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	assert.Empty(t, GetTasks(dbcoreCtx(dbcoreUserID), "not-a-date", "", ""))
}

func TestGetTasks_ValidStartInvalidEnd_NoDatePredicate(t *testing.T) {
	mock := withMockDB(t)
	// An invalid end_date silently drops the ENTIRE date filter — not even
	// the single-day start window is applied (current behavior).
	mock.ExpectQuery(dbcoreGetTasksBaseRegex).
		WithArgs(dbcoreUserID, false).
		WillReturnRows(sqlmock.NewRows(dbcoreTaskCols))

	assert.Empty(t, GetTasks(dbcoreCtx(dbcoreUserID), "2026-07-01", "garbage", ""))
}

func TestGetTasks_DBError_ReturnsEmpty(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(dbcoreGetTasksBaseRegex).
		WithArgs(dbcoreUserID, false).
		WillReturnError(dbcoreErrDB)

	assert.Empty(t, GetTasks(dbcoreCtx(dbcoreUserID), "", "", ""), "errors are logged, not returned")
}

// --- CreateTask ---

func TestCreateTask_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := CreateTask(dbcoreCtx(""), types.Task{Title: "T"})
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCreateTask_InheritsRoadmapIDFromMilestone(t *testing.T) {
	mock := withMockDB(t)
	// RoadmapID empty + MilestoneID set -> milestone looked up and its
	// RoadmapID inherited onto the insert.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID))

	mock.ExpectBegin()
	// Column order: roadmap_id, milestone_id, user_id, title, description,
	// status, rationale, est_minutes, dependencies, confidence, day,
	// completed, date, task_id (pk last, RETURNING).
	mock.ExpectQuery(`INSERT INTO "tasks"`).
		WithArgs(dbcoreRoadmapID, dbcoreMilestoneID, dbcoreUserID, "Paint the fence", "", "not_started",
			"", 0, nil, "", 1, false, nil, dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow(dbcoreTaskID))
	// Nested step wired to the parent task before insert.
	mock.ExpectQuery(`INSERT INTO "steps"`).
		WithArgs(dbcoreTaskID, "sand it first", false, "incomplete", 1, dbcoreStepID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}).AddRow(dbcoreStepID))
	mock.ExpectCommit()

	// Success path re-fetches with Steps preloaded.
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE task_id = \$1`).
		WithArgs(dbcoreTaskID, 1).
		WillReturnRows(dbcoreTaskRow("Paint the fence", 1, false, nil))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows(dbcoreStepCols).
			AddRow(dbcoreStepID, dbcoreTaskID, "sand it first", false, "incomplete", 1))

	got, err := CreateTask(dbcoreCtx(dbcoreUserID), types.Task{
		TaskID:      dbcoreTaskID,
		MilestoneID: dbcoreMilestoneID,
		Title:       "Paint the fence",
		Status:      "not_started",
		Day:         1,
		Steps: []types.Step{
			{StepID: dbcoreStepID, Description: "sand it first", Status: "incomplete", Number: 1},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, dbcoreRoadmapID, got.RoadmapID)
	require.Len(t, got.Steps, 1)
}

func TestCreateTask_InsertErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "tasks"`).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	_, err := CreateTask(dbcoreCtx(dbcoreUserID), types.Task{
		TaskID:    dbcoreTaskID,
		RoadmapID: dbcoreRoadmapID, // set -> no milestone lookup
		Title:     "T",
		Status:    "not_started",
	})
	require.ErrorIs(t, err, dbcoreErrDB)
}

// --- DeleteTask ---

func TestDeleteTask_MissingUserID(t *testing.T) {
	withMockDB(t)

	err := DeleteTask(dbcoreCtx(""), dbcoreTaskID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestDeleteTask_ScopedToUserAndTask(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID))
}

func TestDeleteTask_DBErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID).
		WillReturnError(dbcoreErrDB)
	mock.ExpectRollback()

	require.ErrorIs(t, DeleteTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID), dbcoreErrDB)
}

// --- GetTask / GetTasksByMilestone ---

func TestGetTask_MissingUserID_ZeroValueNoQuery(t *testing.T) {
	withMockDB(t)

	assert.Equal(t, types.Task{}, GetTask(dbcoreCtx(""), dbcoreTaskID))
}

func TestGetTask_FetchError_ZeroValue(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreTaskID, 1).
		WillReturnError(dbcoreErrDB)

	assert.Equal(t, types.Task{}, GetTask(dbcoreCtx(dbcoreUserID), dbcoreTaskID))
}

func TestGetTasksByMilestone_MissingUserID_EmptyNoQuery(t *testing.T) {
	withMockDB(t)

	assert.Empty(t, GetTasksByMilestone(dbcoreCtx(""), dbcoreMilestoneID))
}

func TestGetTasksByMilestone_FetchError_Empty(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND milestone_id = \$2 ORDER BY day ASC`).
		WithArgs(dbcoreUserID, dbcoreMilestoneID).
		WillReturnError(dbcoreErrDB)

	assert.Empty(t, GetTasksByMilestone(dbcoreCtx(dbcoreUserID), dbcoreMilestoneID))
}
