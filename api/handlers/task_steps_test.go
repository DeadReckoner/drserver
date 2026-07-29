package handlers_test

// TaskHandler coverage for the direct-database endpoints (GetOverdueTasks,
// GetTasksByMilestone, CreateStep, DeleteStep), the CreateTask service-error
// branch (fake service, reusing newTaskRouter from task_test.go), and one
// full-router variant proving the DBTaskService.Get adapter wiring in
// services.go.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	hroadStepUserID    = "42424242-5353-6464-7575-868686868686"
	hroadStepRoadmapID = "24242424-3535-4646-5757-686868686868"
	hroadStepMsID      = "52525252-6363-7474-8585-969696969696"
	hroadStepTaskAID   = "62626262-7373-8484-9595-060606060606"
	hroadStepTaskBID   = "26262626-3737-4848-5959-606060606060"
	hroadStepID        = "72727272-8383-9494-0505-161616161616"
)

func hroadStepRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	return testutil.NewTestRouterAs(db, hroadStepUserID), mock
}

// --- GetOverdueTasks ---------------------------------------------------------

func TestOverdueTasks_AllForcedToOverdueStatus(t *testing.T) {
	router, mock := hroadStepRouter(t)

	// Join excludes archived roadmaps; date < today and incomplete only.
	// milestone_id left NULL so AnnotateTaskPaths short-circuits (no sibling
	// ranking queries) and the assertion stays about overdue forcing.
	mock.ExpectQuery(`SELECT .+ FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND tasks\.date < \$2 AND tasks\.completed = \$3 AND roadmaps\.archived = \$4 ORDER BY tasks\.date ASC`).
		WithArgs(hroadStepUserID, sqlmock.AnyArg(), false, false).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "title", "completed", "status"}).
			AddRow(hroadStepTaskAID, hroadStepRoadmapID, "Run 5k", false, "not_started").
			AddRow(hroadStepTaskBID, hroadStepRoadmapID, "Stretch", false, "in_progress"))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" IN \(\$1,\$2\) ORDER BY number ASC`).
		WithArgs(hroadStepTaskAID, hroadStepTaskBID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/tasks/overdue", hroadStepUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 2)
	for _, task := range got {
		assert.Equal(t, string(types.StatusOverdue), task.Status,
			"every returned task must be reported as overdue regardless of stored status")
	}
}

func TestOverdueTasks_RoadmapFilterBound(t *testing.T) {
	router, mock := hroadStepRouter(t)

	// ?roadmap_id adds the extra tasks.roadmap_id condition as arg 5.
	mock.ExpectQuery(`SELECT .+ FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND tasks\.date < \$2 AND tasks\.completed = \$3 AND roadmaps\.archived = \$4 AND tasks\.roadmap_id = \$5 ORDER BY tasks\.date ASC`).
		WithArgs(hroadStepUserID, sqlmock.AnyArg(), false, false, hroadStepRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "title"}).
			AddRow(hroadStepTaskAID, hroadStepRoadmapID, "Run 5k"))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(hroadStepTaskAID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/tasks/overdue?roadmap_id="+hroadStepRoadmapID, hroadStepUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "Run 5k", got[0].Title)
}

func TestOverdueTasks_NoneOverdue_ReturnsEmptyArray(t *testing.T) {
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT .+ FROM "tasks" INNER JOIN roadmaps`).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/tasks/overdue", hroadStepUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "[]", w.Body.String(), "clients iterate the list; must be [] not null")
}

// --- GetTasksByMilestone -----------------------------------------------------

func TestTasksByMilestone_OrderedWithStepsPreload(t *testing.T) {
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND milestone_id = \$2 ORDER BY day ASC`).
		WithArgs(hroadStepUserID, hroadStepMsID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "milestone_id", "title", "day"}).
			AddRow(hroadStepTaskAID, hroadStepMsID, "Day one", 1).
			AddRow(hroadStepTaskBID, hroadStepMsID, "Day two", 2))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" IN \(\$1,\$2\) ORDER BY number ASC`).
		WithArgs(hroadStepTaskAID, hroadStepTaskBID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id", "task_id", "description"}).
			AddRow(hroadStepID, hroadStepTaskAID, "lace shoes"))

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/milestones/"+hroadStepMsID+"/tasks", hroadStepUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "Day one", got[0].Title)
	require.Len(t, got[0].Steps, 1)
	assert.Equal(t, "lace shoes", got[0].Steps[0].Description)
}

func TestTasksByMilestone_Empty_ReturnsEmptyArray(t *testing.T) {
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND milestone_id = \$2 ORDER BY day ASC`).
		WithArgs(hroadStepUserID, hroadStepMsID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/milestones/"+hroadStepMsID+"/tasks", hroadStepUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "[]", w.Body.String())
}

// --- CreateStep --------------------------------------------------------------

func TestStepCreate_TaskNotOwned_Returns500(t *testing.T) {
	// Documents current behavior: a failed ownership check surfaces as a 500
	// ("task not found or access denied"), NOT a 404 — the handler maps every
	// CreateStep error to InternalErrorResponse.
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(hroadStepUserID, hroadStepTaskAID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/tasks/"+hroadStepTaskAID+"/steps", hroadStepUserID,
		map[string]any{"description": "new step", "number": 1})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "task not found or access denied")
}

func TestStepCreate_OwnershipOK_201WithCreatedStep(t *testing.T) {
	router, mock := hroadStepRouter(t)

	// Ownership check on the parent task.
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(hroadStepUserID, hroadStepTaskAID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "user_id"}).
			AddRow(hroadStepTaskAID, hroadStepUserID))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "steps"`).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}).AddRow(hroadStepID))
	mock.ExpectCommit()

	// Reload of the created step by the returned ID.
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE step_id = \$1`).
		WithArgs(hroadStepID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"step_id", "task_id", "description", "number"}).
			AddRow(hroadStepID, hroadStepTaskAID, "new step", 1))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/tasks/"+hroadStepTaskAID+"/steps", hroadStepUserID,
		map[string]any{"description": "new step", "number": 1})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var got types.Step
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, hroadStepID, got.StepID)
	assert.Equal(t, hroadStepTaskAID, got.TaskID, "step is attached to the path task")
	assert.Equal(t, "new step", got.Description)
}

// --- DeleteStep --------------------------------------------------------------

func TestStepDelete_OwnershipOK_200(t *testing.T) {
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(hroadStepUserID, hroadStepTaskAID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "user_id"}).
			AddRow(hroadStepTaskAID, hroadStepUserID))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "steps" WHERE task_id = \$1 AND step_id = \$2`).
		WithArgs(hroadStepTaskAID, hroadStepID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/tasks/"+hroadStepTaskAID+"/steps/"+hroadStepID, hroadStepUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Step deleted successfully")
}

func TestStepDelete_OwnershipFails_500(t *testing.T) {
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(hroadStepUserID, hroadStepTaskAID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/tasks/"+hroadStepTaskAID+"/steps/"+hroadStepID, hroadStepUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- CreateTask service-error branch (fake service) --------------------------

func TestTaskCreate_ServiceError_500(t *testing.T) {
	svc := &testutil.FakeTaskService{
		CreateFunc: func(c *gin.Context, _ types.Task) (types.Task, error) {
			return types.Task{}, errors.New("insert exploded")
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/tasks", testUser,
		types.Task{Title: "doomed"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- Full-router variant: DBTaskService.Get adapter --------------------------

func TestTaskGet_FullRouter_NotFound404_ViaDBService(t *testing.T) {
	// Proves the services.go DBTaskService.Get wiring: the real router routes
	// GET /tasks/:taskid through database.GetTask.
	router, mock := hroadStepRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE user_id = \$1 AND task_id = \$2`).
		WithArgs(hroadStepUserID, hroadStepTaskAID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/tasks/"+hroadStepTaskAID, hroadStepUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
