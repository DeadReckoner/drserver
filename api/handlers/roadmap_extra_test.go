package handlers_test

// Tests topping up RoadmapHandler coverage: list/update/create branches, the
// anchor-user enrichment on GetRoadmap, StartRoadmap/RealignDates error paths
// (both call database.* directly), the remaining lifecycle error mappings, and
// two full-router variants that exercise the DBRoadmapService adapters in
// services.go through real wiring.
//
// Reuses testUser / newTestRouter from roadmap_test.go (same package); all new
// package-level identifiers carry the hroad prefix.

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
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	hroadExtraRoadmapID = "12121212-3434-5656-7878-909090909090"
	hroadExtraAnchorID  = "21212121-4343-6565-8787-090909090909"
)

// --- GetRoadmapsByUser (fake service) ---------------------------------------

func TestRoadmapList_ReturnsServiceRoadmapsAsJSONArray(t *testing.T) {
	svc := &testutil.FakeRoadmapService{
		ListByUserFunc: func(c *gin.Context) []types.Roadmap {
			return []types.Roadmap{
				{RoadmapID: hroadExtraRoadmapID, UserID: testUser, Goal: "run"},
				{RoadmapID: hroadExtraAnchorID, UserID: testUser, Goal: "swim"},
			}
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps", testUser, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.Roadmap
	testutil.DecodeJSON(t, w, &got)
	require.Len(t, got, 2)
	assert.Equal(t, "run", got[0].Goal)
	assert.Equal(t, "swim", got[1].Goal)
}

// --- UpdateRoadmap (fake service) -------------------------------------------

func TestRoadmapUpdate_EchoesUpdatedFields(t *testing.T) {
	id := uuid.NewString()
	var receivedID string
	svc := &testutil.FakeRoadmapService{
		UpdateFunc: func(c *gin.Context, roadmapID string, r types.Roadmap) types.Roadmap {
			receivedID = roadmapID
			r.RoadmapID = roadmapID
			return r
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/roadmaps/"+id, testUser,
		types.Roadmap{Goal: "new goal", Days: 30})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, id, receivedID)
	var got types.Roadmap
	testutil.DecodeJSON(t, w, &got)
	assert.Equal(t, "new goal", got.Goal)
	assert.Equal(t, 30, got.Days)
}

func TestRoadmapUpdate_ServiceReturnsEmpty_404(t *testing.T) {
	svc := &testutil.FakeRoadmapService{
		UpdateFunc: func(c *gin.Context, _ string, _ types.Roadmap) types.Roadmap {
			return types.Roadmap{} // empty = not found / not owned
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/roadmaps/"+uuid.NewString(), testUser,
		types.Roadmap{Goal: "x"})

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestRoadmapUpdate_MalformedJSON_400(t *testing.T) {
	router := newTestRouter(&testutil.FakeRoadmapService{})

	// A JSON string is not an object -> BindJSONOrAbort rejects.
	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/roadmaps/"+uuid.NewString(), testUser,
		"not-an-object")

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// --- CreateRoadmap: default-type branch and service error --------------------

func TestRoadmapCreate_DefaultsTypeToCustomMade(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "false")

	var receivedType string
	svc := &testutil.FakeRoadmapService{
		CreateFunc: func(c *gin.Context, r types.Roadmap) (types.Roadmap, error) {
			receivedType = r.Type
			r.RoadmapID = hroadExtraRoadmapID
			return r, nil
		},
	}
	router := newTestRouter(svc)

	// No "type" field in the body -> the handler must stamp custom_made.
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps", testUser,
		map[string]any{"goal": "learn piano"})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, "custom_made", receivedType)
}

func TestRoadmapCreate_ExplicitTypePreserved(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "false")

	var receivedType string
	svc := &testutil.FakeRoadmapService{
		CreateFunc: func(c *gin.Context, r types.Roadmap) (types.Roadmap, error) {
			receivedType = r.Type
			r.RoadmapID = hroadExtraRoadmapID
			return r, nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps", testUser,
		map[string]any{"goal": "learn piano", "type": "ai_generated"})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, "ai_generated", receivedType)
}

func TestRoadmapCreate_ServiceError_500(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "false")

	svc := &testutil.FakeRoadmapService{
		CreateFunc: func(c *gin.Context, _ types.Roadmap) (types.Roadmap, error) {
			return types.Roadmap{}, errors.New("insert exploded")
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps", testUser,
		map[string]any{"goal": "x"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- GetRoadmap anchor enrichment (fake service + sqlmock for users lookup) --

func TestRoadmapGet_EnrichesAnchorUser(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	anchorID := hroadExtraAnchorID
	svc := &testutil.FakeRoadmapService{
		GetFullFunc: func(c *gin.Context, roadmapID string) types.Roadmap {
			return types.Roadmap{RoadmapID: roadmapID, UserID: testUser, AnchorID: &anchorID}
		},
	}
	router := newTestRouter(svc)

	// getAnchorUserResponse fetches the anchor's user row directly.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(anchorID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name", "last_name", "email"}).
			AddRow(anchorID, "Ada", "Lovelace", "ada@example.com"))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+uuid.NewString(), testUser, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	testutil.DecodeJSON(t, w, &got)
	anchor, ok := got["anchor"].(map[string]any)
	require.True(t, ok, "response must embed an anchor object: %s", w.Body.String())
	assert.Equal(t, anchorID, anchor["id"])
	assert.Equal(t, "Ada Lovelace", anchor["name"], "name is trimmed First+Last")
	assert.Equal(t, "ada@example.com", anchor["email"])
}

func TestRoadmapGet_AnchorLookupFailure_OmitsAnchorButStill200(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	anchorID := hroadExtraAnchorID
	svc := &testutil.FakeRoadmapService{
		GetFullFunc: func(c *gin.Context, roadmapID string) types.Roadmap {
			return types.Roadmap{RoadmapID: roadmapID, UserID: testUser, AnchorID: &anchorID}
		},
	}
	router := newTestRouter(svc)

	// Anchor user lookup fails -> log-and-continue: roadmap is still served.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(anchorID, 1).
		WillReturnError(errors.New("users table on fire"))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+uuid.NewString(), testUser, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	testutil.DecodeJSON(t, w, &got)
	_, hasAnchor := got["anchor"]
	assert.False(t, hasAnchor, "anchor must be omitted when the user lookup fails")
}

// --- StartRoadmap not-found (direct database path) ---------------------------

func TestRoadmapStart_NotFound_404(t *testing.T) {
	// StartRoadmap bypasses the service and calls database.GetRoadmapFull, so
	// the not-found branch is driven through sqlmock.
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	id := uuid.NewString()
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(testUser, id, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	router := newTestRouter(&testutil.FakeRoadmapService{})
	w := testutil.Do(t, router, http.MethodPut, "/api/v1/roadmaps/"+id+"/start", testUser, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// --- RealignDates error mapping (direct database path) -----------------------

func TestRoadmapRealign_NotFound_404(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	id := uuid.NewString()
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(testUser, id, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	router := newTestRouter(&testutil.FakeRoadmapService{})
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/realign", testUser, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestRoadmapRealign_NotStarted_400(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	id := uuid.NewString()
	// Roadmap exists but start_date is NULL -> "roadmap has not been started".
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(testUser, id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "start_date"}).
			AddRow(id, testUser, nil))

	router := newTestRouter(&testutil.FakeRoadmapService{})
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/realign", testUser, nil)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "roadmap has not been started")
}

// --- Lifecycle error mapping top-up (fake service) ---------------------------

func TestRoadmapLifecycle_ServiceErrorsMapToCorrectStatus(t *testing.T) {
	// Mirrors TestRoadmap_Restart_ServiceErrors_MapToCorrectStatus for the
	// branches the existing tests don't cover.
	notFound := errors.New("roadmap not found")
	boom := errors.New("connection refused")

	cases := []struct {
		name       string
		method     string
		pathSuffix string
		body       any
		svc        *testutil.FakeRoadmapService
		wantStatus int
	}{
		{
			name: "shift not found", method: http.MethodPost, pathSuffix: "/shift",
			body: types.ShiftDatesRequest{Days: 3},
			svc: &testutil.FakeRoadmapService{ShiftDatesFunc: func(c *gin.Context, _ string, _ int) (types.Roadmap, error) {
				return types.Roadmap{}, notFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "shift unknown error", method: http.MethodPost, pathSuffix: "/shift",
			body: types.ShiftDatesRequest{Days: 3},
			svc: &testutil.FakeRoadmapService{ShiftDatesFunc: func(c *gin.Context, _ string, _ int) (types.Roadmap, error) {
				return types.Roadmap{}, boom
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "resume not found", method: http.MethodPost, pathSuffix: "/resume",
			svc: &testutil.FakeRoadmapService{ResumeFunc: func(c *gin.Context, _ string) (types.Roadmap, error) {
				return types.Roadmap{}, notFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "resume unknown error", method: http.MethodPost, pathSuffix: "/resume",
			svc: &testutil.FakeRoadmapService{ResumeFunc: func(c *gin.Context, _ string) (types.Roadmap, error) {
				return types.Roadmap{}, boom
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "archive unknown error", method: http.MethodPost, pathSuffix: "/archive",
			svc: &testutil.FakeRoadmapService{ArchiveFunc: func(c *gin.Context, _ string) error {
				return boom
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "restore not found", method: http.MethodPost, pathSuffix: "/restore",
			svc: &testutil.FakeRoadmapService{RestoreFunc: func(c *gin.Context, _ string, _ bool) (types.Roadmap, error) {
				return types.Roadmap{}, notFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "restore unknown error", method: http.MethodPost, pathSuffix: "/restore",
			svc: &testutil.FakeRoadmapService{RestoreFunc: func(c *gin.Context, _ string, _ bool) (types.Roadmap, error) {
				return types.Roadmap{}, boom
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PRO_GATING_ENABLED", "false") // restore passes EnforceLimit first
			router := newTestRouter(tc.svc)

			w := testutil.Do(t, router, tc.method,
				"/api/v1/roadmaps/"+uuid.NewString()+tc.pathSuffix, testUser, tc.body)

			assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
		})
	}
}

// --- Full-router variants: DBRoadmapService adapters in services.go ----------

func TestRoadmapGet_FullRouter_NotFound404_ViaDBService(t *testing.T) {
	// Exercises DBRoadmapService.GetFull through real wiring: the router built
	// from a *gorm.DB routes GET /roadmaps/:id to database.GetRoadmapFull.
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	id := uuid.NewString()
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(hroadExtraAnchorID, id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	router := testutil.NewTestRouterAs(db, hroadExtraAnchorID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+id, hroadExtraAnchorID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestRoadmapList_FullRouter_ViaDBService(t *testing.T) {
	// Exercises DBRoadmapService.ListByUser: archived roadmaps are filtered by
	// default and the milestone/phase preloads run.
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(hroadExtraAnchorID, false).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(hroadExtraRoadmapID, hroadExtraAnchorID, "Ship it"))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(hroadExtraRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(hroadExtraRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))

	router := testutil.NewTestRouterAs(db, hroadExtraAnchorID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps", hroadExtraAnchorID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.Roadmap
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "Ship it", got[0].Goal)
}
