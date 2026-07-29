package database

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockExistsBetween_RowInEitherDirection_True(t *testing.T) {
	mock := withMockDB(t)
	// The WHERE clause must probe BOTH (A,B) and (B,A): a block is mutual in
	// effect even though only the blocker owns the row.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "user_blocks" WHERE \(blocker_id = \$1 AND blocked_id = \$2\) OR \(blocker_id = \$3 AND blocked_id = \$4\)`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID).
		WillReturnRows(dbsocCountRow(1))

	assert.True(t, BlockExistsBetween(dbsocMeID, dbsocFriendID))
}

func TestBlockExistsBetween_NoRows_False(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "user_blocks"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID).
		WillReturnRows(dbsocCountRow(0))

	assert.False(t, BlockExistsBetween(dbsocMeID, dbsocFriendID))
}

func TestCreateBlock_SelfBlock_RejectedWithoutQuery(t *testing.T) {
	withMockDB(t) // no expectations: the guard must fire before any SQL

	err := CreateBlock(dbsocMeID, dbsocMeID)
	require.Error(t, err)
	assert.Equal(t, "cannot block yourself", err.Error())
}

func TestCreateBlock_EmitsOnConflictDoNothing(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// The ON CONFLICT (blocker_id,blocked_id) DO NOTHING clause is the
	// idempotency contract: re-blocking must be a silent no-op.
	mock.ExpectQuery(`INSERT INTO "user_blocks" \("blocker_id","blocked_id"\) VALUES \(\$1,\$2\) ON CONFLICT \("blocker_id","blocked_id"\) DO NOTHING RETURNING "block_id","created_at"`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnRows(sqlmock.NewRows([]string{"block_id", "created_at"}).
			AddRow("eeee1111-0000-0000-0000-000000000001", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)))
	mock.ExpectCommit()

	require.NoError(t, CreateBlock(dbsocMeID, dbsocFriendID))
}

func TestCreateBlock_DriverErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "user_blocks"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := CreateBlock(dbsocMeID, dbsocFriendID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert failed")
}

func TestRemoveBlock_DeletesOnlyBlockersOwnRow(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// Scoped to (blocker=me, blocked=target): the blocked user can never lift
	// a block they don't own.
	mock.ExpectExec(`DELETE FROM "user_blocks" WHERE blocker_id = \$1 AND blocked_id = \$2`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, RemoveBlock(dbsocMeID, dbsocFriendID))
}

func TestGetBlockedUsers_MissingUserID_Error(t *testing.T) {
	withMockDB(t)

	_, err := GetBlockedUsers(dbsocCtx(""))
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestGetBlockedUsers_NoBlocks_EmptyNonNilNoUsersQuery(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "user_blocks" WHERE blocker_id = \$1 ORDER BY created_at DESC`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"block_id", "blocker_id", "blocked_id", "created_at"}))

	resp, err := GetBlockedUsers(dbsocCtx(dbsocMeID))
	require.NoError(t, err)
	require.NotNil(t, resp, "handlers rely on an empty array, not null")
	assert.Empty(t, resp)
}

func TestGetBlockedUsers_HappyPath_PairsProfilesInBlockOrder(t *testing.T) {
	mock := withMockDB(t)
	newer := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM "user_blocks" WHERE blocker_id = \$1 ORDER BY created_at DESC`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"block_id", "blocker_id", "blocked_id", "created_at"}).
			AddRow("e1", dbsocMeID, dbsocFriendID, newer).
			AddRow("e2", dbsocMeID, dbsocOtherID, older))
	// One batched users query, IDs in block order.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id IN \(\$1,\$2\)`).
		WithArgs(dbsocFriendID, dbsocOtherID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name"}).
			AddRow(dbsocOtherID, "Other").
			AddRow(dbsocFriendID, "Friend"))

	resp, err := GetBlockedUsers(dbsocCtx(dbsocMeID))
	require.NoError(t, err)
	require.Len(t, resp, 2)
	// Response order follows the blocks (created_at DESC), not the users query.
	assert.Equal(t, dbsocFriendID, resp[0].User.UserID)
	assert.Equal(t, newer, resp[0].BlockedAt)
	assert.Equal(t, dbsocOtherID, resp[1].User.UserID)
	assert.Equal(t, older, resp[1].BlockedAt)
}

func TestGetBlockedUsers_DanglingBlockedUserSkipped(t *testing.T) {
	mock := withMockDB(t)
	blockedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM "user_blocks" WHERE blocker_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"block_id", "blocker_id", "blocked_id", "created_at"}).
			AddRow("e1", dbsocMeID, dbsocFriendID, blockedAt).
			AddRow("e2", dbsocMeID, dbsocOtherID, blockedAt))
	// The second blocked user's profile row no longer exists; the entry is
	// silently dropped rather than returned half-empty.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id IN \(\$1,\$2\)`).
		WithArgs(dbsocFriendID, dbsocOtherID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))

	resp, err := GetBlockedUsers(dbsocCtx(dbsocMeID))
	require.NoError(t, err)
	require.Len(t, resp, 1)
	assert.Equal(t, dbsocFriendID, resp[0].User.UserID)
}

func TestGetBlockedUsers_UsersQueryError_Propagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "user_blocks" WHERE blocker_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"block_id", "blocker_id", "blocked_id", "created_at"}).
			AddRow("e1", dbsocMeID, dbsocFriendID, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id IN \(\$1\)`).
		WithArgs(dbsocFriendID).
		WillReturnError(errors.New("users fetch failed"))

	resp, err := GetBlockedUsers(dbsocCtx(dbsocMeID))
	require.Error(t, err)
	assert.Nil(t, resp)
}
