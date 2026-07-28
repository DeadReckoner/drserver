package handlers_test

// Share-link flow on RoadmapHandler: ShareRoadmap, RevokeShare,
// GetSharedRoadmap (public), CopySharedRoadmap. These handlers call
// database.* directly, so the tests drive the full router against sqlmock
// (roadmap_anchor_test.go is the closest style reference).
//
// Ownership is enforced inside the database layer via user-scoped queries, so
// "not owner" and "not found" are deliberately the same 404. Reload rows keep
// child collections empty so nested preloads collapse to two cheap queries.
//
// All package-level identifiers carry the hfin prefix to avoid collisions
// with concurrently-added test files.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"dr/server/database"
	"dr/server/internal/testutil"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	hfinShareUserID    = "51515151-6262-7373-8484-959595959595"
	hfinShareOwnerID   = "15151515-2626-3737-4848-595959595959"
	hfinShareRoadmapID = "66666666-7777-8888-9999-000000000001"
	hfinShareToken     = "hfin-share-token-abc123"
)

func hfinShareRouter(t *testing.T, asUser string) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	t.Setenv("PRO_GATING_ENABLED", "false")
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	return testutil.NewTestRouterAs(db, asUser), mock
}

// hfinShareExpectOwnedFetch stubs ShareRoadmap's owner-scoped roadmap fetch.
func hfinShareExpectOwnedFetch(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(hfinShareUserID, hfinShareRoadmapID, 1)
}

// hfinShareExpectTokenFetch stubs the public share-token lookup used by
// GetSharedRoadmap and CopySharedRoadmap.
func hfinShareExpectTokenFetch(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE share_token = \$1`).
		WithArgs(hfinShareToken, 1)
}

// hfinShareExpectEmptyChildren stubs the Milestones + Phases preloads (GORM
// runs top-level preloads in sorted order) with zero rows so no deeper
// preloads fire.
func hfinShareExpectEmptyChildren(mock sqlmock.Sqlmock, roadmapID string) {
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(roadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(roadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
}

// --- ShareRoadmap ------------------------------------------------------------

func TestShareRoadmap_NotFoundOrNotOwner_404(t *testing.T) {
	// The fetch is user-scoped, so a stranger's roadmap and a nonexistent one
	// are indistinguishable: both 404.
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectOwnedFetch(mock).WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Roadmap not found")
}

func TestShareRoadmap_FetchError_500(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectOwnedFetch(mock).WillReturnError(errors.New("connection reset"))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestShareRoadmap_HappyPath_ReturnsToken(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectOwnedFetch(mock).WillReturnRows(
		sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(hfinShareRoadmapID, hfinShareUserID, "Learn to sail"))

	// Token is crypto/rand generated, so only assert it is persisted and
	// echoed back non-empty.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(sqlmock.AnyArg(), hfinShareRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["share_token"])
}

func TestShareRoadmap_TokenPersistFails_500(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectOwnedFetch(mock).WillReturnRows(
		sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(hfinShareRoadmapID, hfinShareUserID, "Learn to sail"))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE roadmap_id = \$2`).
		WillReturnError(errors.New("disk full"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- RevokeShare -------------------------------------------------------------

func TestRevokeShare_HappyPath_200(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WithArgs(nil, hfinShareUserID, hfinShareRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Share link revoked successfully")
}

func TestRevokeShare_NotFoundOrNotOwner_404(t *testing.T) {
	// RowsAffected == 0 (wrong owner or no such roadmap) -> 404.
	router, mock := hfinShareRouter(t, hfinShareUserID)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WithArgs(nil, hfinShareUserID, hfinShareRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Roadmap not found")
}

func TestRevokeShare_UpdateError_500(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/roadmaps/"+hfinShareRoadmapID+"/share", hfinShareUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- GetSharedRoadmap (public) -----------------------------------------------

func TestGetSharedRoadmap_InvalidToken_404(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectTokenFetch(mock).WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/roadmaps/shared/"+hfinShareToken, hfinShareUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Shared roadmap not found")
}

func TestGetSharedRoadmap_DBError_500(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectTokenFetch(mock).WillReturnError(errors.New("connection reset"))

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/roadmaps/shared/"+hfinShareToken, hfinShareUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestGetSharedRoadmap_HappyPath_StripsPrivateFields(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	anchorID := hfinShareOwnerID
	hfinShareExpectTokenFetch(mock).WillReturnRows(
		sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "share_token", "anchor_id"}).
			AddRow(hfinShareRoadmapID, hfinShareOwnerID, "Learn to sail", hfinShareToken, anchorID))
	hfinShareExpectEmptyChildren(mock, hfinShareRoadmapID)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/roadmaps/shared/"+hfinShareToken, hfinShareUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Learn to sail", resp["goal"])
	// Privacy: owner and anchor identity plus the token itself are cleared.
	assert.Equal(t, "", resp["user_id"], "owner identity must be stripped")
	assert.NotContains(t, resp, "anchor_id")
	assert.NotContains(t, resp, "share_token")
}

// --- CopySharedRoadmap -------------------------------------------------------

func TestCopySharedRoadmap_InvalidToken_404(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectTokenFetch(mock).WillReturnError(gorm.ErrRecordNotFound)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/shared/"+hfinShareToken+"/copy", hfinShareUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Shared roadmap not found")
}

func TestCopySharedRoadmap_InsertFails_500(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	hfinShareExpectTokenFetch(mock).WillReturnRows(
		sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(hfinShareRoadmapID, hfinShareOwnerID, "Learn to sail"))
	hfinShareExpectEmptyChildren(mock, hfinShareRoadmapID)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WillReturnError(errors.New("insert exploded"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/shared/"+hfinShareToken+"/copy", hfinShareUserID, nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestCopySharedRoadmap_HappyPath_201OwnedByCaller(t *testing.T) {
	router, mock := hfinShareRouter(t, hfinShareUserID)

	// Original owned by someone else; the copy must belong to the caller.
	hfinShareExpectTokenFetch(mock).WillReturnRows(
		sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "days"}).
			AddRow(hfinShareRoadmapID, hfinShareOwnerID, "Learn to sail", 30))
	hfinShareExpectEmptyChildren(mock, hfinShareRoadmapID)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WillReturnRows(sqlmock.NewRows([]string{"archived"}).AddRow(false))
	mock.ExpectCommit()

	// Reload of the fresh copy (ID generated inside the handler -> AnyArg).
	copyID := "77777777-8888-9999-0000-111111111112"
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(sqlmock.AnyArg(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "days"}).
			AddRow(copyID, hfinShareUserID, "Learn to sail", 30))
	hfinShareExpectEmptyChildren(mock, copyID)
	// resolveUserLocation's timezone lookup then fails safe (unexpected query
	// against the mock -> UTC), which is fine for an empty roadmap.

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/shared/"+hfinShareToken+"/copy", hfinShareUserID, nil)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, hfinShareUserID, resp["user_id"], "copy must be owned by the caller")
	assert.Equal(t, copyID, resp["roadmap_id"])
	assert.NotEqual(t, hfinShareRoadmapID, resp["roadmap_id"], "copy must get a fresh ID")
	assert.Equal(t, "Learn to sail", resp["goal"])
}
