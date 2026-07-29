package database

import (
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// dbsocExpectAcceptedFriendship stubs the CheckAreFriends lookup with an
// accepted row between me and friend.
func dbsocExpectAcceptedFriendship(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))
}

// dbsocExpectOwnedRoadmap stubs the ownership-scoped roadmap fetch.
func dbsocExpectOwnedRoadmap(mock sqlmock.Sqlmock, anchorID any) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(dbsocRoadmapID, dbsocMeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "anchor_id"}).
			AddRow(dbsocRoadmapID, dbsocMeID, "Run a marathon", anchorID))
}

func TestCreateAnchorRequest_RoadmapNotOwned(t *testing.T) {
	mock := withMockDB(t)
	// Ownership is enforced in the query itself: someone else's roadmap looks
	// exactly like a missing one.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1 AND user_id = \$2`).
		WithArgs(dbsocRoadmapID, dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateAnchorRequest(dbsocCtx(dbsocMeID), dbsocRoadmapID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found or you don't have permission", err.Error())
}

func TestCreateAnchorRequest_NotFriends(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedRoadmap(mock, nil)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateAnchorRequest(dbsocCtx(dbsocMeID), dbsocRoadmapID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "you are not friends with this user", err.Error())
}

func TestCreateAnchorRequest_PendingAlreadyExists(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedRoadmap(mock, nil)
	dbsocExpectAcceptedFriendship(mock)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(dbsocRoadmapID, dbsocFriendID, types.TargetTypeRoadmap, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocMeID, dbsocFriendID, "pending"))

	_, err := CreateAnchorRequest(dbsocCtx(dbsocMeID), dbsocRoadmapID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "anchor request already exists for this roadmap", err.Error())
}

func TestCreateAnchorRequest_AnchorAlreadyAssigned(t *testing.T) {
	mock := withMockDB(t)
	// Roadmap already has the target user as its anchor.
	dbsocExpectOwnedRoadmap(mock, dbsocFriendID)
	dbsocExpectAcceptedFriendship(mock)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(dbsocRoadmapID, dbsocFriendID, types.TargetTypeRoadmap, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateAnchorRequest(dbsocCtx(dbsocMeID), dbsocRoadmapID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "this user is already an anchor for this roadmap", err.Error())
}

func TestCreateAnchorRequest_RejectedRow_DeletedThenReinserted(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedRoadmap(mock, nil)
	dbsocExpectAcceptedFriendship(mock)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(dbsocRoadmapID, dbsocFriendID, types.TargetTypeRoadmap, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocMeID, dbsocFriendID, "rejected"))
	// Stale rejected request is removed to allow a fresh ask.
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "anchor_requests" WHERE "anchor_requests"\."request_id" = \$1`).
		WithArgs(dbsocRequestID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "anchor_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id"}).AddRow(dbsocRequestID))
	mock.ExpectCommit()
	// Reload with preloads; a swallowed not-found keeps the in-memory struct.
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1 AND "anchor_requests"."request_id" = \$2`).
		WithArgs(dbsocRequestID, dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	request, err := CreateAnchorRequest(dbsocCtx(dbsocMeID), dbsocRoadmapID, dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, "pending", request.Status)
}

func TestCreateAnchorRequest_HappyPath_RoadmapTargetWithPreloads(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedRoadmap(mock, nil)
	dbsocExpectAcceptedFriendship(mock)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE roadmap_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(dbsocRoadmapID, dbsocFriendID, types.TargetTypeRoadmap, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "anchor_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id"}).AddRow(dbsocRequestID))
	mock.ExpectCommit()
	// Reload + preloads (alphabetical: Anchor, Requester, Roadmap; Habit is
	// not preloaded on the roadmap path).
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1 AND "anchor_requests"\."request_id" = \$2`).
		WithArgs(dbsocRequestID, dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocMeID, dbsocFriendID, "pending"))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocFriendID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE "roadmaps"\."roadmap_id" = \$1`).
		WithArgs(dbsocRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(dbsocRoadmapID, dbsocMeID, "Run a marathon"))

	request, err := CreateAnchorRequest(dbsocCtx(dbsocMeID), dbsocRoadmapID, dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, types.TargetTypeRoadmap, request.TargetType)
	assert.Equal(t, "pending", request.Status)
	assert.Equal(t, dbsocMeID, request.RequesterID)
	assert.Equal(t, dbsocFriendID, request.AnchorID)
	require.NotNil(t, request.RoadmapID)
	assert.Equal(t, dbsocRoadmapID, *request.RoadmapID)
	require.NotNil(t, request.Roadmap)
	assert.Equal(t, "Run a marathon", request.Roadmap.Goal)
}

// dbsocExpectOwnedHabit stubs the ownership-scoped habit fetch.
func dbsocExpectOwnedHabit(mock sqlmock.Sqlmock, anchorID any) {
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbsocHabitID, dbsocMeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "title", "anchor_id"}).
			AddRow(dbsocHabitID, dbsocMeID, "Read 20 minutes", anchorID))
}

func TestCreateHabitAnchorRequest_HabitNotOwned(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbsocHabitID, dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateHabitAnchorRequest(dbsocCtx(dbsocMeID), dbsocHabitID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "habit not found or you don't have permission", err.Error())
}

func TestCreateHabitAnchorRequest_NotFriends(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedHabit(mock, nil)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateHabitAnchorRequest(dbsocCtx(dbsocMeID), dbsocHabitID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "you are not friends with this user", err.Error())
}

func TestCreateHabitAnchorRequest_AnchorAlreadyAssigned(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedHabit(mock, dbsocFriendID)
	dbsocExpectAcceptedFriendship(mock)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE habit_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(dbsocHabitID, dbsocFriendID, types.TargetTypeHabit, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateHabitAnchorRequest(dbsocCtx(dbsocMeID), dbsocHabitID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "this user is already an anchor for this habit", err.Error())
}

func TestCreateHabitAnchorRequest_HappyPath_HabitTarget(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectOwnedHabit(mock, nil)
	dbsocExpectAcceptedFriendship(mock)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE habit_id = \$1 AND anchor_id = \$2 AND target_type = \$3`).
		WithArgs(dbsocHabitID, dbsocFriendID, types.TargetTypeHabit, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "anchor_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"request_id"}).AddRow(dbsocRequestID))
	mock.ExpectCommit()
	// Reload errors are swallowed; the in-memory request is returned.
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1 AND "anchor_requests"."request_id" = \$2`).
		WithArgs(dbsocRequestID, dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	request, err := CreateHabitAnchorRequest(dbsocCtx(dbsocMeID), dbsocHabitID, dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, types.TargetTypeHabit, request.TargetType)
	require.NotNil(t, request.HabitID)
	assert.Equal(t, dbsocHabitID, *request.HabitID)
	assert.Nil(t, request.RoadmapID)
	assert.Equal(t, "pending", request.Status)
}

func TestGetAnchorRequests_MissingUserID_EmptyNoQueries(t *testing.T) {
	withMockDB(t)

	requests := GetAnchorRequests(dbsocCtx(""))
	require.NotNil(t, requests)
	assert.Empty(t, requests)
}

func TestGetAnchorRequests_PendingForAnchorOnly(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE anchor_id = \$1 AND status = \$2 ORDER BY created_at DESC`).
		WithArgs(dbsocMeID, "pending").
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocFriendID, dbsocMeID, "pending"))
	// Preloads (alphabetical): Habit is skipped because habit_id is NULL,
	// then Requester, then Roadmap.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocFriendID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE "roadmaps"\."roadmap_id" = \$1`).
		WithArgs(dbsocRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(dbsocRoadmapID, dbsocFriendID, "Run a marathon"))

	requests := GetAnchorRequests(dbsocCtx(dbsocMeID))
	require.Len(t, requests, 1)
	assert.Equal(t, dbsocMeID, requests[0].AnchorID)
	require.NotNil(t, requests[0].Requester)
	assert.Equal(t, dbsocFriendID, requests[0].Requester.UserID)
}

func TestGetAnchorRequests_QueryError_SilentlyEmpty(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE anchor_id = \$1 AND status = \$2`).
		WithArgs(dbsocMeID, "pending").
		WillReturnError(errors.New("find failed"))

	// The error is swallowed by design; callers get an empty list.
	assert.Empty(t, GetAnchorRequests(dbsocCtx(dbsocMeID)))
}

var (
	dbsocDayStart = time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	dbsocDayEnd   = time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
)

func TestGetAnchoring_MissingUserID_Error(t *testing.T) {
	withMockDB(t)

	_, _, _, _, err := GetAnchoring(dbsocCtx(""), dbsocDayStart, dbsocDayEnd)
	require.Error(t, err)
}

func TestGetAnchoring_NothingAnchored_EmptyNonNilNoFollowupQueries(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id"}))
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id"}))
	// No owners/tasks queries may fire.

	owners, roadmaps, tasks, habits, err := GetAnchoring(dbsocCtx(dbsocMeID), dbsocDayStart, dbsocDayEnd)
	require.NoError(t, err)
	require.NotNil(t, owners)
	require.NotNil(t, roadmaps)
	require.NotNil(t, tasks)
	require.NotNil(t, habits)
	assert.Empty(t, owners)
	assert.Empty(t, roadmaps)
	assert.Empty(t, tasks)
	assert.Empty(t, habits)
}

func TestGetAnchoring_RoadmapsQueryError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(dbsocMeID).
		WillReturnError(errors.New("roadmaps failed"))

	owners, roadmaps, tasks, habits, err := GetAnchoring(dbsocCtx(dbsocMeID), dbsocDayStart, dbsocDayEnd)
	require.Error(t, err)
	assert.Nil(t, owners)
	assert.Nil(t, roadmaps)
	assert.Nil(t, tasks)
	assert.Nil(t, habits)
}

func TestGetAnchoring_HappyPath_DedupedOwnersAndDayWindow(t *testing.T) {
	mock := withMockDB(t)
	const secondRoadmapID = "cccc1111-0000-0000-0000-000000000099"
	// Two roadmaps with overlapping ownership (friend owns one roadmap AND
	// the habit; other owns the second roadmap).
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(dbsocRoadmapID, dbsocFriendID, "Run a marathon").
			AddRow(secondRoadmapID, dbsocOtherID, "Learn Go"))
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE anchor_id = \$1 AND archived = false`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "title"}).
			AddRow(dbsocHabitID, dbsocFriendID, "Read 20 minutes"))
	// Completions preload restricted to the same day window.
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE \(date >= \$1 AND date < \$2\) AND "habit_completions"\."habit_id" = \$3`).
		WithArgs(dbsocDayStart, dbsocDayEnd, dbsocHabitID).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id", "habit_id", "user_id", "date"}).
			AddRow("comp1", dbsocHabitID, dbsocFriendID, dbsocDayStart))
	// Owner IDs are deduped and sorted before the IN query.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id IN \(\$1,\$2\)`).
		WithArgs(dbsocFriendID, dbsocOtherID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name"}).
			AddRow(dbsocFriendID, "Friend").
			AddRow(dbsocOtherID, "Other"))
	// Tasks bounded to [dayStart, dayEnd) across the anchored roadmaps.
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE roadmap_id IN \(\$1,\$2\) AND date >= \$3 AND date < \$4 ORDER BY day ASC`).
		WithArgs(dbsocRoadmapID, secondRoadmapID, dbsocDayStart, dbsocDayEnd).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "title"}))

	owners, roadmaps, tasks, habits, err := GetAnchoring(dbsocCtx(dbsocMeID), dbsocDayStart, dbsocDayEnd)
	require.NoError(t, err)
	assert.Len(t, owners, 2, "overlapping owner appears once")
	assert.Len(t, roadmaps, 2)
	assert.Empty(t, tasks)
	require.Len(t, habits, 1)
	require.Len(t, habits[0].Completions, 1, "same-day completion attached so the caller can tell the habit was done")
}

func TestAcceptAnchorRequest_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := AcceptAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Equal(t, "anchor request not found", err.Error())
}

func TestAcceptAnchorRequest_OnlyAnchorMayAccept(t *testing.T) {
	mock := withMockDB(t)
	// Request is addressed to friend; caller is me.
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocOtherID, dbsocFriendID, "pending"))

	_, err := AcceptAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Equal(t, "only the anchor can accept this request", err.Error())
}

func TestAcceptAnchorRequest_NotPending(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocFriendID, dbsocMeID, "accepted"))

	_, err := AcceptAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Equal(t, "anchor request is not pending", err.Error())
}

func TestAcceptAnchorRequest_RoadmapTarget_UpdatesRoadmapAnchorInTx(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocFriendID, dbsocMeID, "pending"))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "anchor_requests" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "roadmaps" SET "anchor_id"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(dbsocMeID, dbsocRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// Reload errors are swallowed; the accepted in-memory request is returned.
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1 AND "anchor_requests"."request_id" = \$2`).
		WithArgs(dbsocRequestID, dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	request, err := AcceptAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.NoError(t, err)
	assert.Equal(t, "accepted", request.Status)
	assert.NotNil(t, request.UpdatedAt)
}

func TestAcceptAnchorRequest_HabitTarget_UpdatesHabitAnchor(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeHabit, nil, dbsocHabitID, dbsocFriendID, dbsocMeID, "pending"))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "anchor_requests" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Habit target routes the anchor assignment to habits, not roadmaps.
	mock.ExpectExec(`UPDATE "habits" SET "anchor_id"=\$1,"updated_at"=\$2 WHERE habit_id = \$3`).
		WithArgs(dbsocMeID, sqlmock.AnyArg(), dbsocHabitID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1 AND "anchor_requests"."request_id" = \$2`).
		WithArgs(dbsocRequestID, dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	request, err := AcceptAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.NoError(t, err)
	assert.Equal(t, "accepted", request.Status)
}

func TestAcceptAnchorRequest_RoadmapUpdateFails_RollsBack(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocFriendID, dbsocMeID, "pending"))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "anchor_requests" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "roadmaps" SET "anchor_id"=\$1`).
		WillReturnError(errors.New("roadmap update failed"))
	mock.ExpectRollback()

	_, err := AcceptAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "roadmap update failed")
}

func TestRejectAnchorRequest_OnlyAnchorMayReject(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocOtherID, dbsocFriendID, "pending"))

	_, err := RejectAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Equal(t, "only the anchor can reject this request", err.Error())
}

func TestRejectAnchorRequest_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := RejectAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Equal(t, "anchor request not found", err.Error())
}

func TestRejectAnchorRequest_NotPending(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocFriendID, dbsocMeID, "rejected"))

	_, err := RejectAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.Error(t, err)
	assert.Equal(t, "anchor request is not pending", err.Error())
}

func TestRejectAnchorRequest_HappyPath_SavesOutsideExplicitTx(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs(dbsocRequestID, 1).
		WillReturnRows(dbsocAnchorRequestRow(types.TargetTypeRoadmap, dbsocRoadmapID, nil, dbsocFriendID, dbsocMeID, "pending"))
	// Unlike accept, rejection is a single Save (only GORM's implicit
	// per-statement transaction, no multi-statement tx).
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "anchor_requests" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1 AND "anchor_requests"."request_id" = \$2`).
		WithArgs(dbsocRequestID, dbsocRequestID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	request, err := RejectAnchorRequest(dbsocCtx(dbsocMeID), dbsocRequestID)
	require.NoError(t, err)
	assert.Equal(t, "rejected", request.Status)
	assert.NotNil(t, request.UpdatedAt)
}
