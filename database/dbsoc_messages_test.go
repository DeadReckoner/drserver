package database

// Tests for messages.go. GetThreadsByUser's N-thread happy path is
// deliberately not tested: it fires an unsynchronized background goroutine
// UPDATE per thread, which would race sqlmock's ExpectationsWereMet. The
// zero-thread and error paths are covered, and CalculateUnreadCount (the loop
// body's logic) is tested directly.

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// dbsocExpectVerifyParticipant stubs VerifyThreadParticipant's lookup for
// userID in dbsocThreadID.
func dbsocExpectVerifyParticipant(mock sqlmock.Sqlmock, userID string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`SELECT \* FROM "thread_participants" WHERE thread_id = \$1 AND user_id = \$2 AND left_at IS NULL`).
		WithArgs(dbsocThreadID, userID, 1)
}

func dbsocParticipantRow(userID string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"thread_p_id", "thread_id", "user_id"}).
		AddRow("p-"+userID, dbsocThreadID, userID)
}

func dbsocMessageRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"message_id", "thread_id", "user_id", "content", "read", "created_at"})
}

func TestCreateThread_User1Missing(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateThread(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "user1 not found", err.Error())
}

func TestCreateThread_User2Missing(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateThread(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID)
	require.Error(t, err)
	assert.Equal(t, "user2 not found", err.Error())
}

func dbsocExpectThreadUserChecks(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocMeID, 1).
		WillReturnRows(dbsocUserRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
}

func TestCreateThread_ExistingThreadReturnedWithoutInsert(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectThreadUserChecks(mock)
	mock.ExpectQuery(`SELECT t\.\* FROM threads t`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnRows(sqlmock.NewRows([]string{"thread_id", "name"}).AddRow(dbsocThreadID, ""))
	// No INSERTs: the existing thread is reused.

	thread, err := CreateThread(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID)
	require.NoError(t, err)
	assert.Equal(t, dbsocThreadID, thread.ThreadID)
}

func TestCreateThread_HappyPath_ThreadAndBothParticipants(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectThreadUserChecks(mock)
	mock.ExpectQuery(`SELECT t\.\* FROM threads t`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnRows(sqlmock.NewRows([]string{"thread_id"})) // no existing thread
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "threads"`).
		WillReturnRows(sqlmock.NewRows([]string{"thread_id"}).AddRow(dbsocThreadID))
	mock.ExpectCommit()
	// Both users are added as members in one batched insert.
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "thread_participants" .*VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\),\(\$7,\$8,\$9,\$10,\$11,\$12\)`).
		WithArgs(sqlmock.AnyArg(), dbsocMeID, sqlmock.AnyArg(), nil, "member", sqlmock.AnyArg(),
			sqlmock.AnyArg(), dbsocFriendID, sqlmock.AnyArg(), nil, "member", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"thread_p_id"}).AddRow("p1").AddRow("p2"))
	mock.ExpectCommit()

	thread, err := CreateThread(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID)
	require.NoError(t, err)
	assert.NotEmpty(t, thread.ThreadID)
}

func TestCreateThread_ParticipantsInsertFails_ThreadCompensated(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectThreadUserChecks(mock)
	mock.ExpectQuery(`SELECT t\.\* FROM threads t`).
		WithArgs(dbsocMeID, dbsocFriendID).
		WillReturnRows(sqlmock.NewRows([]string{"thread_id"}))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "threads"`).
		WillReturnRows(sqlmock.NewRows([]string{"thread_id"}).AddRow(dbsocThreadID))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "thread_participants"`).
		WillReturnError(errors.New("participants insert failed"))
	mock.ExpectRollback()
	// Compensating delete removes the orphaned thread.
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "threads" WHERE thread_id = \$1`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, err := CreateThread(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "participants insert failed")
}

func TestGetThreadBetweenUsers_Found_ReturnsThreadID(t *testing.T) {
	mock := withMockDB(t)
	// Join arg (user2) renders before the WHERE arg (user1).
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WithArgs(dbsocFriendID, dbsocMeID, 1).
		WillReturnRows(dbsocParticipantRow(dbsocMeID))

	assert.Equal(t, dbsocThreadID, GetThreadBetweenUsers(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID))
}

func TestGetThreadBetweenUsers_NotFound_EmptyString(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WithArgs(dbsocFriendID, dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	assert.Equal(t, "", GetThreadBetweenUsers(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID))
}

func TestGetThreadsByUser_NoThreads_EmptyWithoutUnreadQueries(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "thread_participants" WHERE user_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(dbsocCountRow(0))
	mock.ExpectQuery(`SELECT \* FROM "thread_participants" WHERE user_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"thread_p_id", "thread_id", "user_id"}))
	mock.ExpectQuery(`SELECT DISTINCT t\.\* FROM threads t`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"thread_id"}))

	threads, err := GetThreadsByUser(dbsocCtx(dbsocMeID), dbsocMeID)
	require.NoError(t, err)
	assert.Empty(t, threads)
}

func TestGetThreadsByUser_QueryError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "thread_participants" WHERE user_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(dbsocCountRow(0))
	mock.ExpectQuery(`SELECT \* FROM "thread_participants" WHERE user_id = \$1`).
		WithArgs(dbsocMeID).
		WillReturnRows(sqlmock.NewRows([]string{"thread_p_id", "thread_id", "user_id"}))
	mock.ExpectQuery(`SELECT DISTINCT t\.\* FROM threads t`).
		WithArgs(dbsocMeID).
		WillReturnError(errors.New("threads query failed"))

	threads, err := GetThreadsByUser(dbsocCtx(dbsocMeID), dbsocMeID)
	require.Error(t, err)
	assert.Nil(t, threads)
}

func TestVerifyThreadParticipant_Member_NoError(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))

	require.NoError(t, VerifyThreadParticipant(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID))
}

func TestVerifyThreadParticipant_NonMember_AccessDenied(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnError(gorm.ErrRecordNotFound)

	err := VerifyThreadParticipant(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.Error(t, err)
	assert.Equal(t, "access denied: user is not a participant of this thread", err.Error())
}

func TestVerifyThreadParticipant_DriverError_PropagatedVerbatim(t *testing.T) {
	mock := withMockDB(t)
	driverErr := errors.New("connection lost")
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnError(driverErr)

	err := VerifyThreadParticipant(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost", "non-not-found errors must not be masked as access denied")
}

func TestGetThreadMessages_NonParticipant_Denied(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnError(gorm.ErrRecordNotFound)
	// No message query may run for a non-participant.

	_, err := GetThreadMessages(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestGetThreadMessages_EmptyThread_NoMarkAsRead(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "messages" WHERE thread_id = \$1 AND deleted_at IS NULL ORDER BY created_at ASC`).
		WithArgs(dbsocThreadID).
		WillReturnRows(dbsocMessageRows())
	// Early return: no users batch query and no mark-as-read UPDATE.

	messages, err := GetThreadMessages(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.NoError(t, err)
	require.NotNil(t, messages)
	assert.Empty(t, messages)
}

func TestGetThreadMessages_HappyPath_BatchedSendersThenMarkRead(t *testing.T) {
	mock := withMockDB(t)
	created := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "messages" WHERE thread_id = \$1 AND deleted_at IS NULL ORDER BY created_at ASC`).
		WithArgs(dbsocThreadID).
		WillReturnRows(dbsocMessageRows().
			AddRow("m1", dbsocThreadID, dbsocFriendID, "hi", false, created).
			AddRow("m2", dbsocThreadID, dbsocOtherID, "yo", false, created))
	// One batched users query for both senders (sender-set iteration order is
	// nondeterministic, so args are not pinned). Only friend's profile
	// survives; the missing sender is tolerated with a nil User.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id IN \(\$1,\$2\)`).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	// Retrieval marks the OTHER users' messages read.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "messages" SET "read"=\$1,"updated_at"=\$2 WHERE thread_id = \$3 AND user_id != \$4 AND read = \$5`).
		WithArgs(true, sqlmock.AnyArg(), dbsocThreadID, dbsocMeID, false).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	messages, err := GetThreadMessages(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.NotNil(t, messages[0].User)
	assert.Equal(t, dbsocFriendID, messages[0].User.UserID)
	assert.Nil(t, messages[1].User, "sender missing from the batch fetch yields a nil User, not an error")
}

func TestMarkMessagesAsRead_ScopedToOthersUnread(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "messages" SET "read"=\$1,"updated_at"=\$2 WHERE thread_id = \$3 AND user_id != \$4 AND read = \$5`).
		WithArgs(true, sqlmock.AnyArg(), dbsocThreadID, dbsocMeID, false).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	require.NoError(t, MarkMessagesAsRead(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID))
}

func TestMarkMessagesAsRead_ExecError_Propagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "messages" SET "read"=\$1`).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	err := MarkMessagesAsRead(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestCalculateUnreadCount_CountsOthersUnread(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages" WHERE thread_id = \$1 AND user_id != \$2 AND read = \$3 AND deleted_at IS NULL`).
		WithArgs(dbsocThreadID, dbsocMeID, false).
		WillReturnRows(dbsocCountRow(5))

	count, err := CalculateUnreadCount(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestCalculateUnreadCount_QueryError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages"`).
		WillReturnError(errors.New("count failed"))

	count, err := CalculateUnreadCount(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID)
	require.Error(t, err)
	assert.Zero(t, count)
}

func TestCalculateTotalUnreadCount_JoinsActiveParticipations(t *testing.T) {
	mock := withMockDB(t)
	// Regex pins the join plus the left_at / deleted_at / not-my-messages
	// filters: threads the user left and deleted messages never count.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages" INNER JOIN thread_participants tp ON messages\.thread_id = tp\.thread_id WHERE tp\.user_id = \$1 AND tp\.left_at IS NULL AND messages\.user_id != \$2 AND messages\.read = \$3 AND messages\.deleted_at IS NULL`).
		WithArgs(dbsocMeID, dbsocMeID, false).
		WillReturnRows(dbsocCountRow(7))

	count, err := CalculateTotalUnreadCount(dbsocCtx(dbsocMeID), dbsocMeID)
	require.NoError(t, err)
	assert.Equal(t, 7, count)
}

func TestCalculateTotalUnreadCount_QueryError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages" INNER JOIN thread_participants tp`).
		WillReturnError(errors.New("count failed"))

	count, err := CalculateTotalUnreadCount(dbsocCtx(dbsocMeID), dbsocMeID)
	require.Error(t, err)
	assert.Zero(t, count)
}

func TestCreateMessage_NonParticipant_NothingInserted(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnError(gorm.ErrRecordNotFound)

	_, err := CreateMessage(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID, "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestCreateMessage_BlockedParticipant_RefusedBeforeInsert(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectQuery(`SELECT u\.\* FROM users u`).
		WithArgs(dbsocThreadID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(dbsocMeID).AddRow(dbsocFriendID))
	// A block in either direction between sender and any other participant
	// refuses the send (defense-in-depth after Remove & Block).
	mock.ExpectQuery(`SELECT count\(\*\) FROM "user_blocks"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID).
		WillReturnRows(dbsocCountRow(1))

	_, err := CreateMessage(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID, "hello")
	require.Error(t, err)
	assert.Equal(t, "messaging is blocked between these users", err.Error())
}

func dbsocExpectMessageInsertPreamble(mock sqlmock.Sqlmock) {
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectQuery(`SELECT u\.\* FROM users u`).
		WithArgs(dbsocThreadID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(dbsocMeID).AddRow(dbsocFriendID))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "user_blocks"`).
		WithArgs(dbsocMeID, dbsocFriendID, dbsocFriendID, dbsocMeID).
		WillReturnRows(dbsocCountRow(0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"message_id"}).AddRow(dbsocMessageID))
	mock.ExpectCommit()
	// Thread bookkeeping: bump updated_at...
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "threads" SET "updated_at"=\$1 WHERE thread_id = \$2`).
		WithArgs(sqlmock.AnyArg(), dbsocThreadID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func TestCreateMessage_HappyPath_InsertsAndUpdatesThread(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectMessageInsertPreamble(mock)
	// ...then record the last sent message (updateLastSent).
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "threads" SET "last_sent_message"=\$1,"updated_at"=\$2 WHERE thread_id = \$3`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), dbsocThreadID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	message, err := CreateMessage(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID, "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", message.Content)
	assert.Equal(t, dbsocMeID, message.UserID)
	assert.Equal(t, dbsocThreadID, message.ThreadID)
}

func TestCreateMessage_LastSentUpdateFailure_NonFatal(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectMessageInsertPreamble(mock)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "threads" SET "last_sent_message"=\$1`).
		WillReturnError(errors.New("last_sent failed"))
	mock.ExpectRollback()

	// The message is already persisted; the pointer update is best-effort.
	message, err := CreateMessage(dbsocCtx(dbsocMeID), dbsocThreadID, dbsocMeID, "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", message.Content)
}

func TestUpdateLastSent_WritesPointer(t *testing.T) {
	mock := withMockDB(t)
	messageID := dbsocMessageID
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "threads" SET "last_sent_message"=\$1,"updated_at"=\$2 WHERE thread_id = \$3`).
		WithArgs(&messageID, sqlmock.AnyArg(), dbsocThreadID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, updateLastSent(dbsocThreadID, &messageID))
}

func TestGetMessageByID_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "messages" WHERE message_id = \$1 AND deleted_at IS NULL`).
		WithArgs(dbsocMessageID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := GetMessageByID(dbsocCtx(dbsocMeID), dbsocMessageID, dbsocMeID)
	require.Error(t, err)
	assert.Equal(t, "message not found", err.Error())
}

func dbsocExpectMessageByID(mock sqlmock.Sqlmock, senderID string) {
	mock.ExpectQuery(`SELECT \* FROM "messages" WHERE message_id = \$1 AND deleted_at IS NULL`).
		WithArgs(dbsocMessageID, 1).
		WillReturnRows(dbsocMessageRows().
			AddRow(dbsocMessageID, dbsocThreadID, senderID, "hi", false, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)))
}

func TestGetMessageByID_NonParticipant_Denied(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectMessageByID(mock, dbsocFriendID)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnError(gorm.ErrRecordNotFound)

	_, err := GetMessageByID(dbsocCtx(dbsocMeID), dbsocMessageID, dbsocMeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestGetMessageByID_HappyPath_WithSenderProfile(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectMessageByID(mock, dbsocFriendID)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnRows(dbsocUserRow(dbsocFriendID))

	resp, err := GetMessageByID(dbsocCtx(dbsocMeID), dbsocMessageID, dbsocMeID)
	require.NoError(t, err)
	assert.Equal(t, "hi", resp.Content)
	require.NotNil(t, resp.User)
	assert.Equal(t, dbsocFriendID, resp.User.UserID)
}

func TestGetMessageByID_SenderProfileMissing_NilUser(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectMessageByID(mock, dbsocFriendID)
	dbsocExpectVerifyParticipant(mock, dbsocMeID).WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbsocFriendID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	resp, err := GetMessageByID(dbsocCtx(dbsocMeID), dbsocMessageID, dbsocMeID)
	require.NoError(t, err, "a deleted sender must not hide the message")
	assert.Equal(t, "hi", resp.Content)
	assert.Nil(t, resp.User)
}

func TestDeleteThreadBetweenUsers_NoThread_NoTransaction(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WithArgs(dbsocFriendID, dbsocMeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	require.NoError(t, DeleteThreadBetweenUsers(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID))
}

func TestDeleteThreadBetweenUsers_HappyPath_AllThreeDeletesInOneTx(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WithArgs(dbsocFriendID, dbsocMeID, 1).
		WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "messages" WHERE thread_id = \$1`).
		WithArgs(dbsocThreadID).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectExec(`DELETE FROM "thread_participants" WHERE thread_id = \$1`).
		WithArgs(dbsocThreadID).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`DELETE FROM "threads" WHERE thread_id = \$1`).
		WithArgs(dbsocThreadID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteThreadBetweenUsers(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID))
}

func TestDeleteThreadBetweenUsers_MessagesDeleteFails_RollsBack(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT .* FROM "thread_participants" JOIN thread_participants tp2`).
		WithArgs(dbsocFriendID, dbsocMeID, 1).
		WillReturnRows(dbsocParticipantRow(dbsocMeID))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "messages" WHERE thread_id = \$1`).
		WithArgs(dbsocThreadID).
		WillReturnError(errors.New("messages delete failed"))
	mock.ExpectRollback()

	err := DeleteThreadBetweenUsers(dbsocCtx(dbsocMeID), dbsocMeID, dbsocFriendID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "messages delete failed")
}
