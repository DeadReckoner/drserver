package handlers_test

// Milestone CRUD handlers on RoadmapHandler (GetMilestonesByRoadmap,
// GetMilestone, UpdateMilestone, CreateMilestone, DeleteMilestone) call the
// database package directly, so these tests drive the full router against a
// sqlmock DB, following the missed_tasks_test.go house pattern.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	hroadMsUserID    = "41414141-5252-6363-7474-858585858585"
	hroadMsRoadmapID = "14141414-2525-3636-4747-585858585858"
	hroadMsID        = "51515151-6262-7373-8484-959595959595"
	hroadMsSecondID  = "15151515-2626-3737-4848-595959595959"
	hroadMsTaskID    = "61616161-7272-8383-9494-050505050505"
)

// hroadMsRouter builds a full router + sqlmock pair authenticated as the
// milestone test user.
func hroadMsRouter(t *testing.T) (interface {
	ServeHTTP(w http.ResponseWriter, req *http.Request)
}, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	return testutil.NewTestRouterAs(db, hroadMsUserID), mock
}

// --- GetMilestonesByRoadmap --------------------------------------------------

func TestMilestonesByRoadmap_ReturnsOrderedListWithNestedTasks(t *testing.T) {
	router, mock := hroadMsRouter(t)

	// user_id + roadmap_id scoping, ordered by day.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND roadmap_id = \$2 ORDER BY day ASC`).
		WithArgs(hroadMsUserID, hroadMsRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal", "day"}).
			AddRow(hroadMsID, hroadMsRoadmapID, hroadMsUserID, "Base fitness", 3).
			AddRow(hroadMsSecondID, hroadMsRoadmapID, hroadMsUserID, "First 10k", 10))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" IN \(\$1,\$2\) ORDER BY day ASC`).
		WithArgs(hroadMsID, hroadMsSecondID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "milestone_id", "title"}).
			AddRow(hroadMsTaskID, hroadMsID, "Run 5k"))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(hroadMsTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/roadmaps/"+hroadMsRoadmapID+"/milestones", hroadMsUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.Milestone
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "Base fitness", got[0].SubGoal, "day ASC ordering preserved")
	require.Len(t, got[0].Tasks, 1)
	assert.Equal(t, "Run 5k", got[0].Tasks[0].Title)
	assert.Empty(t, got[1].Tasks)
}

func TestMilestonesByRoadmap_Empty_ReturnsEmptyArrayNotNull(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND roadmap_id = \$2 ORDER BY day ASC`).
		WithArgs(hroadMsUserID, hroadMsRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/roadmaps/"+hroadMsRoadmapID+"/milestones", hroadMsUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "[]", w.Body.String(), "clients iterate the list; must be [] not null")
}

// --- GetMilestone ------------------------------------------------------------

func TestMilestoneGet_NotFound_404(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hroadMsUserID, hroadMsID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/milestones/"+hroadMsID, hroadMsUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestMilestoneGet_Found_IncludesNestedTasks(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hroadMsUserID, hroadMsID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal"}).
			AddRow(hroadMsID, hroadMsRoadmapID, hroadMsUserID, "Base fitness"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(hroadMsID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "milestone_id", "title"}).
			AddRow(hroadMsTaskID, hroadMsID, "Run 5k"))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(hroadMsTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id", "task_id", "description"}).
			AddRow("71717171-8282-9393-0404-151515151515", hroadMsTaskID, "warm up"))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/milestones/"+hroadMsID, hroadMsUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got types.Milestone
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "Base fitness", got.SubGoal)
	require.Len(t, got.Tasks, 1)
	require.Len(t, got.Tasks[0].Steps, 1)
	assert.Equal(t, "warm up", got.Tasks[0].Steps[0].Description)
}

func TestMilestoneGet_BadUUID_400(t *testing.T) {
	router, _ := hroadMsRouter(t)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/milestones/not-a-uuid", hroadMsUserID, nil)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// --- UpdateMilestone ---------------------------------------------------------

func TestMilestoneUpdate_MalformedJSON_400(t *testing.T) {
	// BindJSONOrAbort runs before any DB access: no queries expected.
	router, _ := hroadMsRouter(t)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/milestones/"+hroadMsID, hroadMsUserID, "not-an-object")

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestMilestoneUpdate_NotFound_404(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hroadMsUserID, hroadMsID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/milestones/"+hroadMsID, hroadMsUserID,
		map[string]any{"sub_goal": "New"})

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestMilestoneUpdate_SubGoal_PersistsAndReturnsUpdatedBody(t *testing.T) {
	router, mock := hroadMsRouter(t)

	// 1. User-scoped fetch (with empty Tasks preload).
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hroadMsUserID, hroadMsID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal", "day", "reached", "date"}).
			AddRow(hroadMsID, hroadMsRoadmapID, hroadMsUserID, "Old", 1, false, nil))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(hroadMsID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	// 2. Partial update of the changed field only.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "sub_goal"=\$1 WHERE "milestone_id" = \$2`).
		WithArgs("New", hroadMsID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// 3. Auto-complete probe: zero tasks -> no completion cascade.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(hroadMsID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// 4. Trailing reload (gorm appends the pk condition to the populated dest).
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1 AND "milestones"\."milestone_id" = \$2`).
		WithArgs(hroadMsID, hroadMsID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "sub_goal"}).
			AddRow(hroadMsID, hroadMsRoadmapID, "New"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(hroadMsID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/milestones/"+hroadMsID, hroadMsUserID,
		map[string]any{"sub_goal": "New"})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got types.Milestone
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "New", got.SubGoal)
}

// --- CreateMilestone ---------------------------------------------------------

func TestMilestoneCreate_StampsOwnershipFromAuthAndPath(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectBegin()
	// The behavioral point: roadmap_id comes from the path and user_id from
	// the authenticated context, regardless of what the body claims.
	mock.ExpectQuery(`INSERT INTO "milestones"`).
		WithArgs(hroadMsRoadmapID, hroadMsUserID, "Base miles", "", 5, false, nil, "not_started",
			nil, "", 0, 0, 0, nil, hroadMsID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}).AddRow(hroadMsID))
	mock.ExpectCommit()

	// Reload with Tasks.Steps preload.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(hroadMsID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal"}).
			AddRow(hroadMsID, hroadMsRoadmapID, hroadMsUserID, "Base miles"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1`).
		WithArgs(hroadMsID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadMsRoadmapID+"/milestones", hroadMsUserID,
		types.Milestone{
			MilestoneID: hroadMsID,
			// Body claims a different owner/roadmap; the server must override.
			UserID:    "99999999-9999-9999-9999-999999999999",
			RoadmapID: "88888888-8888-8888-8888-888888888888",
			SubGoal:   "Base miles",
			Day:       5,
			Status:    "not_started",
		})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var got types.Milestone
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, hroadMsID, got.MilestoneID)
}

func TestMilestoneCreate_InsertError_500(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "milestones"`).
		WillReturnError(errors.New("insert exploded"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadMsRoadmapID+"/milestones", hroadMsUserID,
		types.Milestone{MilestoneID: hroadMsID, SubGoal: "x", Status: "not_started"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- DeleteMilestone ---------------------------------------------------------

func TestMilestoneDelete_ScopedToOwner_200(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hroadMsUserID, hroadMsID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodDelete, "/api/v1/milestones/"+hroadMsID, hroadMsUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Milestone deleted successfully")
}

func TestMilestoneDelete_DBError_500(t *testing.T) {
	router, mock := hroadMsRouter(t)

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hroadMsUserID, hroadMsID).
		WillReturnError(errors.New("delete exploded"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodDelete, "/api/v1/milestones/"+hroadMsID, hroadMsUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
