package database

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// dbsocExpectFriendshipLookup stubs the bidirectional friendship First query
// used by CreateFriendRequest / RemoveFriend (no status filter).
func dbsocExpectFriendshipLookup(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE \(\(requester_id = \$1 AND addressee_id = \$2\) OR \(requester_id = \$3 AND addressee_id = \$4\)\)`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, 1)
}

func TestGetFriendshipStatus_ReturnsStoredStatus(t *testing.T) {
	mock := dbsocMockDB(t)
	// Bidirectional match: (A,B) and (B,A) are both probed, restricted to
	// pending/accepted.
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE \(\(requester_id = \$1 AND addressee_id = \$2\) OR \(requester_id = \$3 AND addressee_id = \$4\)\) AND status IN \(\$5, \$6\)`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "pending", "accepted", 1).
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "pending"))

	assert.Equal(t, "pending", GetFriendshipStatus(dbsocMeID, dbsocFriendID))
}

func TestGetFriendshipStatus_NoRow_None(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "pending", "accepted", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	assert.Equal(t, "none", GetFriendshipStatus(dbsocMeID, dbsocFriendID))
}

func TestCreateFriendRequest_MissingUserID_NoQueries(t *testing.T) {
	dbsocMockDB(t)

	_, err := CreateFriendRequest(dbsocCtx(""), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCreateFriendRequest_RequesterProfileMissing(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "requester user not found - please create your profile first", err.Error())
}

func TestCreateFriendRequest_AddresseeMissing(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "addressee user not found", err.Error())
}

func TestCreateFriendRequest_SelfRequest_Rejected(t *testing.T) {
	mock := dbsocMockDB(t)
	// Both existence lookups run before the self-check.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))

	_, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocMeID)
	require.Error(t, err)
	assert.Equal(t, "cannot send friend request to yourself", err.Error())
}

func TestCreateFriendRequest_BlockedEitherDirection_Refused(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "user_blocks"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID).
		WillReturnRows(dbsocCountRow(1))

	_, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "cannot send friend request to this user", err.Error())
}

// dbsocExpectFriendRequestPreamble stubs the requester/addressee/block checks
// shared by all the existing-friendship branches.
func dbsocExpectFriendRequestPreamble(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "user_blocks"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID).
		WillReturnRows(dbsocCountRow(0))
}

func dbsocExpectFriendshipInsert(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, "pending", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(dbsocFriendshipID))
	mock.ExpectCommit()
}

func TestCreateFriendRequest_HappyPath(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendRequestPreamble(mock)
	dbsocExpectFriendshipLookup(mock).WillReturnError(gorm.ErrRecordNotFound)
	dbsocExpectFriendshipInsert(mock)

	friendship, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, dbsocMeID, friendship.RequesterID)
	assert.Equal(t, dbsocFriendID, friendship.AddresseeID)
	assert.Equal(t, "pending", friendship.Status)
}

func TestCreateFriendRequest_AlreadyFriends(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendRequestPreamble(mock)
	dbsocExpectFriendshipLookup(mock).WillReturnRows(dbsocFriendshipRow(dbsocFriendID, dbsocMeID, "accepted"))

	_, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "users are already friends", err.Error())
}

func TestCreateFriendRequest_AlreadyPending(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendRequestPreamble(mock)
	dbsocExpectFriendshipLookup(mock).WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "pending"))

	_, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "friend request already exists", err.Error())
}

func TestCreateFriendRequest_RejectedRow_DeletedThenReinserted(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendRequestPreamble(mock)
	dbsocExpectFriendshipLookup(mock).WillReturnRows(dbsocFriendshipRow(dbsocFriendID, dbsocMeID, "rejected"))
	// The stale rejected row is removed so a fresh pending request can exist.
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "friendships" WHERE "friendships"\."id" = \$1`).
		WithArgs(dbsocFriendshipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	dbsocExpectFriendshipInsert(mock)

	friendship, err := CreateFriendRequest(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, "pending", friendship.Status)
}

func TestGetFriendRequests_MissingUserID_EmptyNoQueries(t *testing.T) {
	dbsocMockDB(t)

	requests := GetFriendRequests(dbsocCtx(""))
	require.NotNil(t, requests)
	assert.Empty(t, requests)
}

func TestGetFriendRequests_QueryError_EmptyNonNil(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE \(requester_id = \$1 OR addressee_id = \$2\) AND status = \$3`).
		WithArgs(dbsocMeID, dbsocMeID, "pending").
		WillReturnError(errors.New("find failed"))

	requests := GetFriendRequests(dbsocCtx(dbsocMeID))
	require.NotNil(t, requests, "error path must return an empty slice, not nil")
	assert.Empty(t, requests)
}

func TestGetFriendRequests_FiltersOnPending(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE \(requester_id = \$1 OR addressee_id = \$2\) AND status = \$3 ORDER BY created_at DESC`).
		WithArgs(dbsocMeID, dbsocMeID, "pending").
		WillReturnRows(dbsocFriendshipRow(dbsocFriendID, dbsocMeID, "pending"))
	// Preloads run alphabetically: Addressee then Requester.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocFriendID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))

	requests := GetFriendRequests(dbsocCtx(dbsocMeID))
	require.Len(t, requests, 1)
	assert.Equal(t, "pending", requests[0].Status)
	require.NotNil(t, requests[0].Requester)
	assert.Equal(t, dbsocFriendID, requests[0].Requester.UserID)
	require.NotNil(t, requests[0].Addressee)
	assert.Equal(t, dbsocMeID, requests[0].Addressee.UserID)
}

func TestGetFriends_FiltersOnAccepted(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE \(requester_id = \$1 OR addressee_id = \$2\) AND status = \$3 ORDER BY created_at DESC`).
		WithArgs(dbsocMeID, dbsocMeID, "accepted").
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocFriendID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(dbsocUserRow(dbsocMeID))

	friends := GetFriends(dbsocCtx(dbsocMeID))
	require.Len(t, friends, 1)
	assert.Equal(t, "accepted", friends[0].Status)
}

func TestGetFriends_MissingUserID_EmptyNoQueries(t *testing.T) {
	dbsocMockDB(t)

	friends := GetFriends(dbsocCtx(""))
	require.NotNil(t, friends)
	assert.Empty(t, friends)
}

func TestGetFriendProfile_MissingUserID_Error(t *testing.T) {
	dbsocMockDB(t)

	_, _, _, err := GetFriendProfile(dbsocCtx(""), dbsocFriendID)
	require.Error(t, err)
}

func TestGetFriendProfile_NotFriends_Denied(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, _, _, err := GetFriendProfile(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "you are not friends with this user", err.Error())
}

func TestGetFriendProfile_FriendUserFetchError_Propagated(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnError(errors.New("user fetch failed"))

	_, _, _, err := GetFriendProfile(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user fetch failed")
}

func TestGetFriendProfile_AnchoringOnlyVisibility(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	// Roadmap visibility is anchoring-only: the query must pin BOTH the
	// friend's ownership and the caller as anchor.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND anchor_id = \$2`).
		WithArgs(dbsocFriendID, dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}))
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND anchor_id = \$2 AND archived = false`).
		WithArgs(dbsocFriendID, dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "title"}))

	friendUser, roadmaps, habits, err := GetFriendProfile(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, dbsocFriendID, friendUser.UserID)
	assert.Empty(t, roadmaps)
	assert.Empty(t, habits)
}

func TestRemoveFriend_FriendshipNotFound(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendshipLookup(mock).WillReturnError(gorm.ErrRecordNotFound)

	err := RemoveFriend(dbsocCtx(dbsocMeID), dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "friendship not found", err.Error())
}

func TestRemoveFriend_HappyPath_NoThreadToDelete(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendshipLookup(mock).WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "friendships" WHERE "friendships"\."id" = \$1`).
		WithArgs(dbsocFriendshipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// Thread lookup finds nothing -> nothing more to tear down.
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WillReturnError(gorm.ErrRecordNotFound)

	require.NoError(t, RemoveFriend(dbsocCtx(dbsocMeID), dbsocFriendID))
}

func TestRemoveFriend_ThreadTeardownFailure_StillSucceeds(t *testing.T) {
	mock := dbsocMockDB(t)
	dbsocExpectFriendshipLookup(mock).WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "friendships"`).
		WithArgs(dbsocFriendshipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// A thread exists, but its teardown transaction fails and rolls back.
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WillReturnRows(sqlmock.NewRows([]string{"thread_p_id", "thread_id", "user_id"}).
			AddRow("p1", dbsocThreadID, dbsocMeID))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "messages" WHERE thread_id = \$1`).
		WithArgs(dbsocThreadID).
		WillReturnError(errors.New("messages delete failed"))
	mock.ExpectRollback()

	// The friendship is already gone; thread cleanup is best-effort.
	require.NoError(t, RemoveFriend(dbsocCtx(dbsocMeID), dbsocFriendID))
}

func TestBlockUser_SelfBlock_Rejected(t *testing.T) {
	dbsocMockDB(t)

	err := BlockUser(dbsocCtx(dbsocMeID), dbsocMeID)
	require.Error(t, err)
	assert.Equal(t, "cannot block yourself", err.Error())
}

func TestBlockUser_WithoutPriorFriendship_Succeeds(t *testing.T) {
	mock := dbsocMockDB(t)
	// Block row is recorded first (idempotent upsert)...
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "user_blocks" .*ON CONFLICT \("blocker_id","blocked_id"\) DO NOTHING`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnRows(sqlmock.NewRows([]string{"block_id", "created_at"}).AddRow("b1", nil))
	mock.ExpectCommit()
	// ...then the friendship teardown finds nothing, which is tolerated:
	// blocking never required being friends.
	dbsocExpectFriendshipLookup(mock).WillReturnError(gorm.ErrRecordNotFound)

	require.NoError(t, BlockUser(dbsocCtx(dbsocMeID), dbsocFriendID))
}

func TestUnblockUser_MissingUserID_Error(t *testing.T) {
	dbsocMockDB(t)

	require.Error(t, UnblockUser(dbsocCtx(""), dbsocFriendID))
}

func TestUnblockUser_DeletesOnlyOwnBlockRow(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectBegin()
	// (blocker=me, blocked=target) only: the blocked user can never unblock
	// themselves through this path.
	mock.ExpectExec(`DELETE FROM "user_blocks" WHERE blocker_id = \$1 AND blocked_id = \$2`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, UnblockUser(dbsocCtx(dbsocMeID), dbsocFriendID))
}

func TestUpdateFriendshipStatus_InvalidStatus_NoQuery(t *testing.T) {
	dbsocMockDB(t)

	_, err := UpdateFriendshipStatus(dbsocCtx(dbsocMeID), dbsocFriendshipID, "blocked")
	require.Error(t, err)
	assert.Equal(t, "invalid status: must be 'accepted' or 'rejected'", err.Error())
}

func TestUpdateFriendshipStatus_NotFound(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs(dbsocFriendshipID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := UpdateFriendshipStatus(dbsocCtx(dbsocMeID), dbsocFriendshipID, "accepted")
	require.Error(t, err)
	assert.Equal(t, "friendship not found", err.Error())
}

func TestUpdateFriendshipStatus_RequesterCannotRespond(t *testing.T) {
	mock := dbsocMockDB(t)
	// The caller is the requester, not the addressee.
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs(dbsocFriendshipID, 1).
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "pending"))

	_, err := UpdateFriendshipStatus(dbsocCtx(dbsocMeID), dbsocFriendshipID, "accepted")
	require.Error(t, err)
	assert.Equal(t, "only the addressee can respond to a friend request", err.Error())
}

func TestUpdateFriendshipStatus_NotPending(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs(dbsocFriendshipID, 1).
		WillReturnRows(dbsocFriendshipRow(dbsocFriendID, dbsocMeID, "accepted"))

	_, err := UpdateFriendshipStatus(dbsocCtx(dbsocMeID), dbsocFriendshipID, "accepted")
	require.Error(t, err)
	assert.Equal(t, "friend request is not pending", err.Error())
}

func TestUpdateFriendshipStatus_Rejected_KeepsRecord(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs(dbsocFriendshipID, 1).
		WillReturnRows(dbsocFriendshipRow(dbsocFriendID, dbsocMeID, "pending"))
	mock.ExpectBegin()
	// Rejection is an UPDATE, not a DELETE - the record stays for audit.
	mock.ExpectExec(`UPDATE "friendships" SET .*"status"=\$3.* WHERE "id" = \$7`).
		WithArgs(dbsocFriendID, dbsocMeID, "rejected", nil, sqlmock.AnyArg(), sqlmock.AnyArg(), dbsocFriendshipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	friendship, err := UpdateFriendshipStatus(dbsocCtx(dbsocMeID), dbsocFriendshipID, "rejected")
	require.NoError(t, err)
	assert.Equal(t, "rejected", friendship.Status)
	assert.NotNil(t, friendship.UpdatedAt)
}

func TestUpdateFriendshipStatus_Accepted_ThreadFailureNonFatal(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs(dbsocFriendshipID, 1).
		WillReturnRows(dbsocFriendshipRow(dbsocFriendID, dbsocMeID, "pending"))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "friendships" SET .*"status"=\$3`).
		WithArgs(dbsocFriendID, dbsocMeID, "accepted", nil, sqlmock.AnyArg(), sqlmock.AnyArg(), dbsocFriendshipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// CreateThread's user1 lookup fails; the acceptance must survive anyway
	// (log-and-continue contract - the thread can be created later).
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnError(errors.New("user lookup failed"))

	friendship, err := UpdateFriendshipStatus(dbsocCtx(dbsocMeID), dbsocFriendshipID, "accepted")
	require.NoError(t, err)
	assert.Equal(t, "accepted", friendship.Status)
}

func TestCheckAreFriends_AcceptedRow_True(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnRows(dbsocFriendshipRow(dbsocMeID, dbsocFriendID, "accepted"))

	assert.True(t, CheckAreFriends(dbsocMeID, dbsocFriendID))
}

func TestCheckAreFriends_NoRow_False(t *testing.T) {
	mock := dbsocMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "friendships"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID, "accepted", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	assert.False(t, CheckAreFriends(dbsocMeID, dbsocFriendID))
}
