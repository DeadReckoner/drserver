package database

import (
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	removalOwnerID   = "11111111-1111-1111-1111-111111111111"
	removalAnchorID  = "22222222-2222-2222-2222-222222222222"
	removalOtherID   = "33333333-3333-3333-3333-333333333333"
	removalRoadmapID = "44444444-4444-4444-4444-444444444444"
	removalHabitID   = "55555555-5555-5555-5555-555555555555"
)

func anchorRemovalCtx(userID string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", userID)
	return c
}

func expectRoadmapFetch(mock sqlmock.Sqlmock, anchorID any) {
	rows := sqlmock.NewRows([]string{"roadmap_id", "user_id", "anchor_id", "goal"}).
		AddRow(removalRoadmapID, removalOwnerID, anchorID, "Run a marathon")
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(removalRoadmapID, 1).
		WillReturnRows(rows)
}

func expectRoadmapAnchorCleared(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "anchor_id"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(nil, removalRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func TestRemoveAnchor_OwnerCanRemove(t *testing.T) {
	mock := withMockDB(t)
	expectRoadmapFetch(mock, removalAnchorID)
	expectRoadmapAnchorCleared(mock)

	roadmap, err := RemoveAnchor(anchorRemovalCtx(removalOwnerID), removalRoadmapID)
	require.NoError(t, err)
	assert.Equal(t, removalOwnerID, roadmap.UserID)
	require.NotNil(t, roadmap.AnchorID)
	assert.Equal(t, removalAnchorID, *roadmap.AnchorID, "returned roadmap carries the pre-removal anchor for notifications")
}

func TestRemoveAnchor_AnchorCanRemoveSelf(t *testing.T) {
	mock := withMockDB(t)
	expectRoadmapFetch(mock, removalAnchorID)
	expectRoadmapAnchorCleared(mock)

	roadmap, err := RemoveAnchor(anchorRemovalCtx(removalAnchorID), removalRoadmapID)
	require.NoError(t, err)
	assert.Equal(t, removalOwnerID, roadmap.UserID)
}

func TestRemoveAnchor_StrangerDenied(t *testing.T) {
	mock := withMockDB(t)
	expectRoadmapFetch(mock, removalAnchorID)
	// No UPDATE expected — the fetch is the only query.

	_, err := RemoveAnchor(anchorRemovalCtx(removalOtherID), removalRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found or you don't have permission", err.Error(),
		"stranger gets the same message as not-found so roadmap IDs can't be probed")
}

func TestRemoveAnchor_NoAnchorAssigned(t *testing.T) {
	mock := withMockDB(t)
	expectRoadmapFetch(mock, nil)

	_, err := RemoveAnchor(anchorRemovalCtx(removalOwnerID), removalRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "no anchor assigned to this roadmap", err.Error())
}

func TestRemoveHabitAnchor_AnchorCanRemoveSelf(t *testing.T) {
	mock := withMockDB(t)

	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1`).
		WithArgs(removalHabitID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "anchor_id", "title"}).
			AddRow(removalHabitID, removalOwnerID, removalAnchorID, "Read 20 minutes"))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "habits" SET "anchor_id"=\$1`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	habit, err := RemoveHabitAnchor(anchorRemovalCtx(removalAnchorID), removalHabitID)
	require.NoError(t, err)
	assert.Equal(t, "Read 20 minutes", habit.Title)
	require.NotNil(t, habit.AnchorID)
	assert.Equal(t, removalAnchorID, *habit.AnchorID)
}

func TestRemoveHabitAnchor_StrangerDenied(t *testing.T) {
	mock := withMockDB(t)

	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1`).
		WithArgs(removalHabitID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "anchor_id", "title"}).
			AddRow(removalHabitID, removalOwnerID, removalAnchorID, "Read 20 minutes"))

	_, err := RemoveHabitAnchor(anchorRemovalCtx(removalOtherID), removalHabitID)
	require.Error(t, err)
	assert.Equal(t, "habit not found or you don't have permission", err.Error())
}
