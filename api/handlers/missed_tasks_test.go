package handlers_test

import (
	"encoding/json"
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
	missedOwnerID   = "11111111-2222-3333-4444-555555555555"
	missedRoadmapID = "66666666-7777-8888-9999-000000000000"
	missedTaskAID   = "aaaa1111-aaaa-1111-aaaa-111111111111"
	missedTaskBID   = "bbbb2222-bbbb-2222-bbbb-222222222222"
)

func TestGetMissedTasks_ReturnsOverdueIncompleteTasks(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	// Ownership check.
	mock.ExpectQuery(`SELECT "roadmap_id" FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(missedRoadmapID, missedOwnerID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}).AddRow(missedRoadmapID))

	// Overdue-task query (joins roadmaps to exclude archived).
	mock.ExpectQuery(`SELECT .+ FROM "tasks" INNER JOIN roadmaps ON tasks\.roadmap_id = roadmaps\.roadmap_id WHERE tasks\.user_id = \$1 AND tasks\.date < \$2 AND tasks\.completed = \$3 AND roadmaps\.archived = \$4 AND tasks\.roadmap_id = \$5 ORDER BY tasks\.date ASC`).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "title", "completed"}).
			AddRow(missedTaskAID, missedRoadmapID, "Run 5k", false).
			AddRow(missedTaskBID, missedRoadmapID, "Stretch", false))

	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" IN \(\$1,\$2\) ORDER BY number ASC`).
		WithArgs(missedTaskAID, missedTaskBID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id", "task_id"}))

	router := testutil.NewTestRouterAs(db, missedOwnerID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+missedRoadmapID+"/missed-tasks", missedOwnerID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp types.MissedTasksResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, missedRoadmapID, resp.RoadmapID)
	require.Len(t, resp.MissedTasks, 2)
	assert.Equal(t, "Run 5k", resp.MissedTasks[0].Title)
	for _, task := range resp.MissedTasks {
		assert.Equal(t, string(types.StatusOverdue), task.Status, "missed tasks are reported with overdue status")
	}
}

func TestGetMissedTasks_NoneMissed(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectQuery(`SELECT "roadmap_id" FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(missedRoadmapID, missedOwnerID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}).AddRow(missedRoadmapID))

	mock.ExpectQuery(`SELECT .+ FROM "tasks" INNER JOIN roadmaps`).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	router := testutil.NewTestRouterAs(db, missedOwnerID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+missedRoadmapID+"/missed-tasks", missedOwnerID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// missed_tasks must be [] rather than null so the client can iterate.
	assert.Contains(t, w.Body.String(), `"missed_tasks":[]`)
}

func TestGetMissedTasks_NotOwnerIs404(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	// Roadmap exists but belongs to someone else → the ownership query finds
	// nothing and the endpoint must 404 without leaking task data.
	mock.ExpectQuery(`SELECT "roadmap_id" FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(missedRoadmapID, missedOwnerID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	router := testutil.NewTestRouterAs(db, missedOwnerID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+missedRoadmapID+"/missed-tasks", missedOwnerID, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
