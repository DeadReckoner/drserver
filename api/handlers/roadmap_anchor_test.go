package handlers_test

// Anchor flow on RoadmapHandler: CreateAnchorRequest, GetAnchorRequests,
// AcceptAnchorRequest, RejectAnchorRequest, RemoveAnchor. These handlers call
// database.* directly, so the tests drive the full router against sqlmock
// (friends_anchoring_test.go is the closest style reference).
//
// Success paths fire notification goroutines (sendAnchorRequestNotification
// etc.); those hit the mock as unexpected queries and fail safe inside
// notifications.Dispatch, which doesn't affect ExpectationsWereMet. Reload
// rows keep FK columns NULL to suppress relationship preloads, and assertions
// stay on HTTP status/body only.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

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
	hroadAnchOwnerID   = "43434343-5454-6565-7676-878787878787"
	hroadAnchFriendID  = "34343434-4545-5656-6767-787878787878"
	hroadAnchStranger  = "43214321-5432-6543-7654-876587658765"
	hroadAnchRoadmapID = "44444444-5555-6666-7777-888888888881"
	hroadAnchReqID     = "44444444-5555-6666-7777-888888888882"
)

var hroadAnchCreatedAt = time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)

func hroadAnchRouter(t *testing.T, asUser string) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	t.Setenv("PRO_GATING_ENABLED", "false")
	db, mock := testutil.NewSqlmockDBRegex(t)
	// Deliberately NOT restored on cleanup: the anchor handlers fire
	// fire-and-forget notification goroutines that can outlive the test. If
	// the global DB were restored to nil before one runs, its GetUserByID
	// call would nil-panic and kill the whole test binary. Leaving the
	// (soon-closed) mock in place turns stray queries into harmless errors.
	database.SetDBForTest(db)
	return testutil.NewTestRouterAs(db, asUser), mock
}

// hroadAnchExpectRoadmapFetch stubs CreateAnchorRequest's owner-scoped fetch.
func hroadAnchExpectOwnedRoadmap(mock sqlmock.Sqlmock, anchorID *string) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(hroadAnchRoadmapID, hroadAnchOwnerID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "anchor_id"}).
			AddRow(hroadAnchRoadmapID, hroadAnchOwnerID, "Run a marathon", anchorID))
}

// hroadAnchExpectFriendship stubs CheckAreFriends (found = accepted friends).
func hroadAnchExpectFriendship(mock sqlmock.Sqlmock, found bool) {
	q := mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE \(\(requester_id = \$1 AND addressee_id = \$2\) OR \(requester_id = \$3 AND addressee_id = \$4\)\) AND status = \$5`)
	if found {
		q.WillReturnRows(sqlmock.NewRows([]string{"id", "requester_id", "addressee_id", "status"}).
			AddRow("55555555-6666-7777-8888-999999999990", hroadAnchOwnerID, hroadAnchFriendID, "accepted"))
	} else {
		q.WillReturnError(gorm.ErrRecordNotFound)
	}
}

// --- CreateAnchorRequest -----------------------------------------------------

func TestAnchorRequestCreate_MissingAnchorID_400(t *testing.T) {
	router, _ := hroadAnchRouter(t, hroadAnchOwnerID)

	// anchor_id is binding:"required" -> rejected before any DB access.
	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor-request", hroadAnchOwnerID,
		map[string]any{})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAnchorRequestCreate_RoadmapNotFound_404(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(hroadAnchRoadmapID, hroadAnchOwnerID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor-request", hroadAnchOwnerID,
		map[string]string{"anchor_id": hroadAnchFriendID})

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestAnchorRequestCreate_NotFriends_403(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	hroadAnchExpectOwnedRoadmap(mock, nil)
	hroadAnchExpectFriendship(mock, false)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor-request", hroadAnchOwnerID,
		map[string]string{"anchor_id": hroadAnchFriendID})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "you are not friends with this user")
}

func TestAnchorRequestCreate_PendingRequestExists_409(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	hroadAnchExpectOwnedRoadmap(mock, nil)
	hroadAnchExpectFriendship(mock, true)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(hroadAnchRoadmapID, hroadAnchFriendID, types.TargetTypeRoadmap, 1).
		WillReturnRows(sqlmock.NewRows([]string{"request_id", "status"}).
			AddRow(hroadAnchReqID, "pending"))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor-request", hroadAnchOwnerID,
		map[string]string{"anchor_id": hroadAnchFriendID})

	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "anchor request already exists")
}

func TestAnchorRequestCreate_AlreadyAnchor_409(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	friendID := hroadAnchFriendID
	hroadAnchExpectOwnedRoadmap(mock, &friendID) // already anchored by the friend
	hroadAnchExpectFriendship(mock, true)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(hroadAnchRoadmapID, hroadAnchFriendID, types.TargetTypeRoadmap, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor-request", hroadAnchOwnerID,
		map[string]string{"anchor_id": hroadAnchFriendID})

	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "already an anchor")
}

func TestAnchorRequestCreate_HappyPath_201WithRequestID(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	hroadAnchExpectOwnedRoadmap(mock, nil)
	hroadAnchExpectFriendship(mock, true)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(hroadAnchRoadmapID, hroadAnchFriendID, types.TargetTypeRoadmap, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "anchor_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id"}).AddRow(hroadAnchReqID))
	mock.ExpectCommit()

	// Reload with NULL FKs so Roadmap/Requester/Anchor preloads don't fire.
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id", "target_type", "status", "created_at"}).
			AddRow(hroadAnchReqID, "roadmap", "pending", hroadAnchCreatedAt))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor-request", hroadAnchOwnerID,
		map[string]string{"anchor_id": hroadAnchFriendID})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var got types.AnchorRequest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, hroadAnchReqID, got.RequestID)
	assert.Equal(t, "pending", got.Status)
}

// --- GetAnchorRequests -------------------------------------------------------

func TestAnchorRequestsList_PendingOnly(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE anchor_id = \$1 AND status = \$2 ORDER BY created_at DESC`).
		WithArgs(hroadAnchFriendID, "pending").
		WillReturnRows(sqlmock.NewRows([]string{"request_id", "target_type", "status", "created_at"}).
			AddRow(hroadAnchReqID, "roadmap", "pending", hroadAnchCreatedAt))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/anchor-requests", hroadAnchFriendID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []types.AnchorRequest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, hroadAnchReqID, got[0].RequestID)
}

func TestAnchorRequestsList_Empty_ReturnsEmptyArray(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE anchor_id = \$1 AND status = \$2 ORDER BY created_at DESC`).
		WithArgs(hroadAnchFriendID, "pending").
		WillReturnRows(sqlmock.NewRows([]string{"request_id"}))

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/anchor-requests", hroadAnchFriendID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "[]", w.Body.String())
}

// --- Accept / Reject ---------------------------------------------------------

// hroadAnchExpectRequestFetch stubs the request lookup by ID.
func hroadAnchExpectRequestFetch(mock sqlmock.Sqlmock, anchorID, status string) {
	rid := hroadAnchRoadmapID
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(hroadAnchReqID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"request_id", "target_type", "roadmap_id", "requester_id", "anchor_id", "status", "created_at"}).
			AddRow(hroadAnchReqID, "roadmap", rid, hroadAnchOwnerID, anchorID, status, hroadAnchCreatedAt))
}

func TestAnchorAccept_NotFound_404(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(hroadAnchReqID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/anchor-requests/"+hroadAnchReqID+"/accept", hroadAnchFriendID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestAnchorAccept_CallerNotAnchor_403(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchStranger)

	hroadAnchExpectRequestFetch(mock, hroadAnchFriendID, "pending")

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/anchor-requests/"+hroadAnchReqID+"/accept", hroadAnchStranger, nil)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "only the anchor can accept")
}

func TestAnchorAccept_NotPending_400(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	hroadAnchExpectRequestFetch(mock, hroadAnchFriendID, "accepted")

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/anchor-requests/"+hroadAnchReqID+"/accept", hroadAnchFriendID, nil)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "anchor request is not pending")
}

func TestAnchorAccept_HappyPath_SetsRoadmapAnchorAndReturnsAccepted(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	hroadAnchExpectRequestFetch(mock, hroadAnchFriendID, "pending")

	// One transaction: flip the request, then stamp the roadmap's anchor_id.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "anchor_requests" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "roadmaps" SET "anchor_id"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(hroadAnchFriendID, hroadAnchRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Reload with NULL FKs to suppress preloads.
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id", "target_type", "status", "created_at"}).
			AddRow(hroadAnchReqID, "roadmap", "accepted", hroadAnchCreatedAt))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/anchor-requests/"+hroadAnchReqID+"/accept", hroadAnchFriendID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got types.AnchorRequest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "accepted", got.Status)
}

func TestAnchorReject_ErrorTrio(t *testing.T) {
	cases := []struct {
		name       string
		caller     string
		stub       func(mock sqlmock.Sqlmock)
		wantStatus int
	}{
		{
			name: "not found", caller: hroadAnchFriendID,
			stub: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
					WithArgs(hroadAnchReqID, 1).
					WillReturnError(gorm.ErrRecordNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "caller not anchor", caller: hroadAnchStranger,
			stub: func(mock sqlmock.Sqlmock) {
				hroadAnchExpectRequestFetch(mock, hroadAnchFriendID, "pending")
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "not pending", caller: hroadAnchFriendID,
			stub: func(mock sqlmock.Sqlmock) {
				hroadAnchExpectRequestFetch(mock, hroadAnchFriendID, "rejected")
			},
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, mock := hroadAnchRouter(t, tc.caller)
			tc.stub(mock)

			w := testutil.Do(t, router, http.MethodPost,
				"/api/v1/anchor-requests/"+hroadAnchReqID+"/reject", tc.caller, nil)

			assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
		})
	}
}

func TestAnchorReject_HappyPath_ReturnsRejected(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	hroadAnchExpectRequestFetch(mock, hroadAnchFriendID, "pending")

	// Save (full-row update) in its own default transaction.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "anchor_requests" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id", "target_type", "status", "created_at"}).
			AddRow(hroadAnchReqID, "roadmap", "rejected", hroadAnchCreatedAt))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/anchor-requests/"+hroadAnchReqID+"/reject", hroadAnchFriendID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got types.AnchorRequest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "rejected", got.Status)
}

// --- RemoveAnchor ------------------------------------------------------------

// hroadAnchExpectRoadmapByID stubs RemoveAnchor's unscoped roadmap fetch.
func hroadAnchExpectRoadmapByID(mock sqlmock.Sqlmock, ownerID string, anchorID *string) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(hroadAnchRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "anchor_id"}).
			AddRow(hroadAnchRoadmapID, ownerID, "Run a marathon", anchorID))
}

func TestAnchorRemove_RoadmapNotFound_404(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(hroadAnchRoadmapID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor", hroadAnchOwnerID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestAnchorRemove_StrangerGetsSame404_AntiProbing(t *testing.T) {
	// Deliberate: a caller who is neither owner nor anchor gets the SAME 404
	// as a nonexistent roadmap, so roadmap IDs can't be probed for existence.
	router, mock := hroadAnchRouter(t, hroadAnchStranger)

	friendID := hroadAnchFriendID
	hroadAnchExpectRoadmapByID(mock, hroadAnchOwnerID, &friendID)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor", hroadAnchStranger, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Roadmap not found",
		"stranger must see the identical not-found response, not a permission error")
}

func TestAnchorRemove_NoAnchorAssigned_400(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	hroadAnchExpectRoadmapByID(mock, hroadAnchOwnerID, nil)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor", hroadAnchOwnerID, nil)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "no anchor assigned")
}

func TestAnchorRemove_ByOwner_200(t *testing.T) {
	router, mock := hroadAnchRouter(t, hroadAnchOwnerID)

	friendID := hroadAnchFriendID
	hroadAnchExpectRoadmapByID(mock, hroadAnchOwnerID, &friendID)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "anchor_id"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(nil, hroadAnchRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor", hroadAnchOwnerID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Anchor removed successfully")
}

func TestAnchorRemove_AnchorRemovesThemselves_200(t *testing.T) {
	// The isAnchor branch (drserver#93): the anchor may step down.
	router, mock := hroadAnchRouter(t, hroadAnchFriendID)

	friendID := hroadAnchFriendID
	hroadAnchExpectRoadmapByID(mock, hroadAnchOwnerID, &friendID)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "anchor_id"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(nil, hroadAnchRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hroadAnchRoadmapID+"/anchor", hroadAnchFriendID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Anchor removed successfully")
}

// hroadAnchIgnore keeps the errors import used if assertions above change.
var _ = errors.New
