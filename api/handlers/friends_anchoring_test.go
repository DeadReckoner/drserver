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
)

const (
	anchorUserID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	ownerAliceID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	ownerBobID   = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	roadmapRunID = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	roadmapEspID = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	taskRunID    = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

func TestGetAnchoring_GroupsTasksByOwnerAndRoadmap(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(anchorUserID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(roadmapRunID, ownerAliceID, "Run a marathon").
			AddRow(roadmapEspID, ownerBobID, "Learn Spanish"))

	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id IN \(\$1,\$2\)`).
		WithArgs(ownerAliceID, ownerBobID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name"}).
			AddRow(ownerAliceID, "Alice").
			AddRow(ownerBobID, "Bob"))

	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE roadmap_id IN \(\$1,\$2\) AND date >= \$3 AND date < \$4 ORDER BY day ASC`).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "title", "day"}).
			AddRow(taskRunID, roadmapRunID, "Run 5k", 3))

	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1 ORDER BY number ASC`).
		WithArgs(taskRunID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id", "task_id"}))

	router := testutil.NewTestRouterAs(db, anchorUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/friends/anchoring?date=2026-07-01", anchorUserID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp types.AnchoringResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "2026-07-01", resp.Date)
	require.Len(t, resp.Users, 2)

	byUser := map[string]types.AnchoringUserResponse{}
	for _, u := range resp.Users {
		byUser[u.User.UserID] = u
	}

	alice := byUser[ownerAliceID]
	require.Len(t, alice.Roadmaps, 1)
	assert.Equal(t, "Run a marathon", alice.Roadmaps[0].Goal)
	require.Len(t, alice.Roadmaps[0].Tasks, 1)
	assert.Equal(t, "Run 5k", alice.Roadmaps[0].Tasks[0].Title)

	bob := byUser[ownerBobID]
	require.Len(t, bob.Roadmaps, 1)
	assert.Equal(t, "Learn Spanish", bob.Roadmaps[0].Goal)
	assert.Empty(t, bob.Roadmaps[0].Tasks, "roadmap with no tasks that day should have an empty (non-null) task list")
}

func TestGetAnchoring_NotAnchoringAnyone(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(anchorUserID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	router := testutil.NewTestRouterAs(db, anchorUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/friends/anchoring?date=2026-07-01", anchorUserID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp types.AnchoringResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Users)
}

func TestGetAnchoring_InvalidDate(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	router := testutil.NewTestRouterAs(db, anchorUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/friends/anchoring?date=07-01-2026", anchorUserID, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
