package database

import (
	"errors"
	"testing"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreateUser_MissingUserID_NoQueries(t *testing.T) {
	withMockDB(t) // no expectations: the guard must fire before any SQL

	tx := CreateUser(dbsocCtx(""), types.User{Email: "a@example.com"})
	require.Error(t, tx.Error)
	assert.Equal(t, "missing or invalid user_id", tx.Error.Error())
}

func TestCreateUser_AlreadyExists_UpsertNoOp(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	// No INSERT expected: an existing user is a success, not a duplicate error.

	tx := CreateUser(dbsocCtx(dbsocMeID), types.User{Email: "a@example.com"})
	require.NoError(t, tx.Error)
}

func TestCreateUser_NewUser_InsertUsesCtxUserID(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectBegin()
	// The user_id column must come from the authenticated context, not from
	// the request body — the struct below carries a spoofed ID.
	mock.ExpectExec(`INSERT INTO "users"`).
		WithArgs(dbsocMeID, "Alice", "Smith", "alice@example.com", "", sqlmock.AnyArg(), "", "America/New_York", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx := CreateUser(dbsocCtx(dbsocMeID), types.User{
		UserID:              dbsocOtherID, // spoofed; must be overwritten
		FirstName:           "Alice",
		LastName:            "Smith",
		Email:               "alice@example.com",
		Timezone:            "America/New_York",
		OnboardingCompleted: true,
	})
	require.NoError(t, tx.Error)
}

func TestCreateUser_LookupError_Propagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnError(errors.New("connection reset"))

	tx := CreateUser(dbsocCtx(dbsocMeID), types.User{})
	require.Error(t, tx.Error)
	assert.Contains(t, tx.Error.Error(), "connection reset")
}

func TestGetUser_Found_ReturnsPopulatedUser(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))

	user := GetUser(dbsocCtx(dbsocMeID))
	assert.Equal(t, dbsocMeID, user.UserID)
	assert.Equal(t, "Test", user.FirstName)
}

func TestGetUser_NoRow_ReturnsZeroValueWithoutError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	// Documents the no-error contract handlers rely on: an unknown user is a
	// zero-value User, never a panic or error surface.
	user := GetUser(dbsocCtx(dbsocMeID))
	assert.Equal(t, types.User{}, user)
}

func TestUpdateUser_Existing_UpdatesNonZeroFieldsOnly(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectBegin()
	// Struct Updates: only the changed (non-zero) field appears in SET, and
	// the WHERE pins the fetched primary key.
	mock.ExpectExec(`UPDATE "users" SET "first_name"=\$1 WHERE "user_id" = \$2`).
		WithArgs("Bob", dbsocMeID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx := UpdateUser(dbsocMeID, types.User{FirstName: "Bob"})
	require.NoError(t, tx.Error)
}

func TestUpdateUser_Missing_ErrRecordNotFoundNoUpdate(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	tx := UpdateUser(dbsocMeID, types.User{FirstName: "Bob"})
	require.Error(t, tx.Error)
	assert.ErrorIs(t, tx.Error, gorm.ErrRecordNotFound)
}

func TestDeleteUser_EmptyID_ErrorBeforeAnyQuery(t *testing.T) {
	withMockDB(t)

	err := DeleteUser("")
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestDeleteUser_HappyPath_ScopedToUserID(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteUser(dbsocMeID))
}

func TestDeleteUser_DriverErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	err := DeleteUser(dbsocMeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

func TestSearchUsersByEmail_WrapsQueryInILIKEWildcards(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE email ILIKE \$1`).
		WithArgs("%alice%").
		WillReturnRows(dbsocUserRow(dbsocMeID))

	users := SearchUsersByEmail("alice")
	require.Len(t, users, 1)
	assert.Equal(t, dbsocMeID, users[0].UserID)
}

func TestSearchUsersByEmail_QueryError_EmptyNonNil(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE email ILIKE \$1`).
		WithArgs("%alice%").
		WillReturnError(errors.New("search failed"))

	users := SearchUsersByEmail("alice")
	require.NotNil(t, users, "error path must return an empty slice, not nil")
	assert.Empty(t, users)
}

func TestGetUserByID_Found(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnRows(dbsocUserRow(dbsocFriendID))

	user, err := GetUserByID(dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, dbsocFriendID, user.UserID)
}

func TestGetUserByID_NotFound_ErrorPassthrough(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	user, err := GetUserByID(dbsocFriendID)
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.Equal(t, types.User{}, user)
}
