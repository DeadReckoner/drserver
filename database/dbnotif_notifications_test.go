package database

// Tests for notifications.go: push token upsert-by-expo-token, the in-app
// notification list/clear/dismiss flows, JSONB-predicate deletes, and the
// orphaned-request-notification cleanup branching.
//
// Note on GetActivePushTokens: the function deliberately swallows DB errors
// (no error return), so only the rows-returned path is assertable here.

import (
	"context"
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const dbnotifExpoToken = "ExponentPushToken[abc123]"

// =============================================================================
// Push tokens
// =============================================================================

func TestCreateOrUpdatePushToken_NewToken_Inserted(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "push_tokens" WHERE expo_token = \$1`).
		WithArgs(dbnotifExpoToken, 1).
		WillReturnRows(sqlmock.NewRows([]string{"token_id"})) // not found
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "push_tokens"`).
		WillReturnRows(sqlmock.NewRows([]string{"token_id"}).AddRow(dbnotifTokenID))
	mock.ExpectCommit()

	token := &types.PushToken{UserID: testUserID, ExpoToken: dbnotifExpoToken, Platform: "ios"}
	require.NoError(t, CreateOrUpdatePushToken(context.Background(), token))
	assert.True(t, token.IsActive, "new tokens are activated")
	assert.NotEmpty(t, token.TokenID, "a TokenID is generated before insert")
}

func TestCreateOrUpdatePushToken_ExistingToken_RefreshedAndSaved(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "push_tokens" WHERE expo_token = \$1`).
		WithArgs(dbnotifExpoToken, 1).
		WillReturnRows(sqlmock.NewRows([]string{"token_id", "user_id", "expo_token", "platform", "is_active"}).
			AddRow(dbnotifTokenID, dbnotifFriendID /* previous owner */, dbnotifExpoToken, "ios", false))
	mock.ExpectBegin()
	// Save writes every column; the args pin the refresh semantics: new
	// user_id (device changed hands), new platform/device_id, reactivated.
	mock.ExpectExec(`UPDATE "push_tokens" SET "user_id"=\$1,"expo_token"=\$2,"platform"=\$3,"device_id"=\$4,"is_active"=\$5,"last_used_at"=\$6,"created_at"=\$7 WHERE "token_id" = \$8`).
		WithArgs(testUserID, dbnotifExpoToken, "android", "device-2", true, dbnotifNonZeroTime{}, sqlmock.AnyArg(), dbnotifTokenID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	token := &types.PushToken{UserID: testUserID, ExpoToken: dbnotifExpoToken, Platform: "android", DeviceID: "device-2"}
	require.NoError(t, CreateOrUpdatePushToken(context.Background(), token))
	assert.Equal(t, dbnotifTokenID, token.TokenID, "existing TokenID is copied back onto the input token")
}

func TestCreateOrUpdatePushToken_LookupError_NoWrite(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "push_tokens"`).
		WillReturnError(errors.New("lookup failed"))

	err := CreateOrUpdatePushToken(context.Background(), &types.PushToken{UserID: testUserID, ExpoToken: dbnotifExpoToken})
	assert.ErrorContains(t, err, "lookup failed")
}

func TestGetActivePushTokens_ReturnsActiveRows(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "push_tokens" WHERE user_id = \$1 AND is_active = \$2`).
		WithArgs(testUserID, true).
		WillReturnRows(sqlmock.NewRows([]string{"token_id", "user_id", "expo_token"}).
			AddRow("t-1", testUserID, "ExponentPushToken[a]").
			AddRow("t-2", testUserID, "ExponentPushToken[b]"))

	tokens := GetActivePushTokens(context.Background(), testUserID)
	require.Len(t, tokens, 2)
	assert.Equal(t, "ExponentPushToken[a]", tokens[0].ExpoToken)
}

func TestDeactivatePushToken(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "push_tokens" SET "is_active"=\$1 WHERE expo_token = \$2`).
		WithArgs(false, dbnotifExpoToken).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeactivatePushToken(context.Background(), dbnotifExpoToken))
}

// =============================================================================
// Notifications: create / list / clear / read / status
// =============================================================================

func TestCreateNotification(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notifications"`).
		WillReturnRows(sqlmock.NewRows([]string{"notification_id"}).AddRow(dbnotifNotifID))
	mock.ExpectCommit()

	notif := &types.Notification{UserID: testUserID, Type: "friend_request", Title: "t", Body: "b"}
	require.NoError(t, CreateNotification(context.Background(), notif))
	assert.Equal(t, dbnotifNotifID, notif.NotificationID)
}

func TestGetNotifications_FilterFlags(t *testing.T) {
	// The regex pins the full WHERE...ORDER BY span, so a predicate that
	// should be absent would break the match.
	tests := []struct {
		name           string
		unreadOnly     bool
		includeCleared bool
		pattern        string
	}{
		{"default view", false, false, `WHERE user_id = \$1 AND cleared_at IS NULL ORDER BY created_at DESC`},
		{"unread only", true, false, `WHERE user_id = \$1 AND read_at IS NULL AND cleared_at IS NULL ORDER BY created_at DESC`},
		{"include cleared", false, true, `WHERE user_id = \$1 ORDER BY created_at DESC`},
		{"unread incl cleared", true, true, `WHERE user_id = \$1 AND read_at IS NULL ORDER BY created_at DESC`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := withMockDB(t)
			mock.ExpectQuery(`SELECT \* FROM "notifications" ` + tc.pattern).
				WithArgs(testUserID).
				WillReturnRows(sqlmock.NewRows([]string{"notification_id", "user_id", "type"}).
					AddRow(dbnotifNotifID, testUserID, "nudge"))

			notifs := GetNotifications(dbnotifCtx(testUserID), testUserID, tc.unreadOnly, tc.includeCleared)
			require.Len(t, notifs, 1)
			assert.Equal(t, dbnotifNotifID, notifs[0].NotificationID)
		})
	}
}

func TestClearNotifications_ExcludesRequestTypes(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notifications" SET "cleared_at"=\$1 WHERE user_id = \$2 AND cleared_at IS NULL AND type NOT IN \(\$3,\$4\)`).
		WithArgs(dbnotifNonZeroTime{}, testUserID, "friend_request", "anchor_request").
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	err := ClearNotifications(context.Background(), testUserID, []string{"friend_request", "anchor_request"})
	require.NoError(t, err)
}

func TestMarkNotificationRead(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notifications" SET "read_at"=\$1 WHERE notification_id = \$2`).
		WithArgs(dbnotifNonZeroTime{}, dbnotifNotifID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, MarkNotificationRead(context.Background(), dbnotifNotifID))
}

func TestUpdateNotificationStatus(t *testing.T) {
	sentAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectBegin()
	// Map keys sorted by GORM: sent_at before status.
	mock.ExpectExec(`UPDATE "notifications" SET "sent_at"=\$1,"status"=\$2 WHERE notification_id = \$3`).
		WithArgs(sentAt, string(types.NotificationStatusSent), dbnotifNotifID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := UpdateNotificationStatus(context.Background(),
		&types.Notification{NotificationID: dbnotifNotifID},
		map[string]any{"status": string(types.NotificationStatusSent), "sent_at": sentAt})
	require.NoError(t, err)
}

// =============================================================================
// JSONB-predicate deletes
// =============================================================================

func TestDeleteNotificationsByTypeAndData_RawJSONBArgs(t *testing.T) {
	mock := withMockDB(t)
	// Raw db.Exec — no surrounding transaction. Args must arrive in SQL order.
	mock.ExpectExec(`DELETE FROM notifications WHERE user_id = \$1 AND type = \$2 AND data->>\$3 = \$4`).
		WithArgs(testUserID, "friend_request", "friendship_id", dbnotifRowID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := DeleteNotificationsByTypeAndData(context.Background(), testUserID, "friend_request", "friendship_id", dbnotifRowID)
	require.NoError(t, err)
}

func TestDeleteNotificationsByTypeAndMultipleDataKeys(t *testing.T) {
	// A single-entry map keeps the generated predicate order deterministic
	// (map iteration order would otherwise shuffle the data->>? clauses).
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notifications" WHERE \(?user_id = \$1 AND type = \$2\)? AND data->>\$3 = \$4`).
		WithArgs(testUserID, "anchor_request", "request_id", dbnotifRowID).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	err := DeleteNotificationsByTypeAndMultipleDataKeys(context.Background(), testUserID, "anchor_request",
		map[string]string{"request_id": dbnotifRowID})
	require.NoError(t, err)
}

// =============================================================================
// DismissNotification
// =============================================================================

func TestDismissNotification_ScopedToUser(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notifications" SET "cleared_at"=\$1 WHERE notification_id = \$2 AND user_id = \$3`).
		WithArgs(dbnotifNonZeroTime{}, dbnotifNotifID, testUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DismissNotification(context.Background(), testUserID, dbnotifNotifID))
}

func TestDismissNotification_NoRows_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notifications" SET "cleared_at"=\$1 WHERE notification_id = \$2 AND user_id = \$3`).
		WithArgs(dbnotifNonZeroTime{}, dbnotifNotifID, testUserID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := DismissNotification(context.Background(), testUserID, dbnotifNotifID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDismissNotification_DBError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notifications"`).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	err := DismissNotification(context.Background(), testUserID, dbnotifNotifID)
	assert.ErrorContains(t, err, "update failed")
}

// =============================================================================
// CleanupOrphanedRequestNotifications
// =============================================================================

// dbnotifRequestNotifRows builds the initial uncleared-request-notification
// result set. data is raw jsonb bytes (the serializer:json field scans them).
func dbnotifRequestNotifRows(rows ...[3]string) *sqlmock.Rows {
	out := sqlmock.NewRows([]string{"notification_id", "user_id", "type", "data"})
	for _, r := range rows {
		out.AddRow(r[0], testUserID, r[1], []byte(r[2]))
	}
	return out
}

func TestCleanupOrphanedRequestNotifications_NoNotifications(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notifications" WHERE user_id = \$1 AND type IN \(\$2,\$3,\$4\) AND cleared_at IS NULL`).
		WithArgs(testUserID, "friend_request", "anchor_request", "habit_anchor_request").
		WillReturnRows(dbnotifRequestNotifRows())

	n, err := CleanupOrphanedRequestNotifications(context.Background(), testUserID)
	require.NoError(t, err)
	assert.Zero(t, n, "no rows means no cleanup UPDATE")
}

func TestCleanupOrphanedRequestNotifications_PendingRequestsKept(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notifications" WHERE user_id = \$1 AND type IN \(\$2,\$3,\$4\) AND cleared_at IS NULL`).
		WithArgs(testUserID, "friend_request", "anchor_request", "habit_anchor_request").
		WillReturnRows(dbnotifRequestNotifRows(
			[3]string{"n-1", "friend_request", `{"friendship_id":"f-1"}`},
		))
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs("f-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("f-1", "pending"))

	n, err := CleanupOrphanedRequestNotifications(context.Background(), testUserID)
	require.NoError(t, err)
	assert.Zero(t, n, "a still-pending friendship is not orphaned — no UPDATE runs")
}

func TestCleanupOrphanedRequestNotifications_MixedBatch_ClearsOnlyOrphans(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notifications" WHERE user_id = \$1 AND type IN \(\$2,\$3,\$4\) AND cleared_at IS NULL`).
		WithArgs(testUserID, "friend_request", "anchor_request", "habit_anchor_request").
		WillReturnRows(dbnotifRequestNotifRows(
			[3]string{"n-1", "friend_request", `{}`},                      // missing friendship_id -> orphaned, no lookup
			[3]string{"n-2", "friend_request", `{"friendship_id":"f-2"}`}, // accepted -> orphaned
			[3]string{"n-3", "friend_request", `{"friendship_id":"f-3"}`}, // pending -> kept
			[3]string{"n-4", "anchor_request", `{"request_id":"r-4"}`},    // row gone -> orphaned
		))
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs("f-2", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("f-2", "accepted"))
	mock.ExpectQuery(`SELECT \* FROM "friendships" WHERE id = \$1`).
		WithArgs("f-3", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("f-3", "pending"))
	mock.ExpectQuery(`SELECT \* FROM "anchor_requests" WHERE request_id = \$1`).
		WithArgs("r-4", 1).
		WillReturnRows(sqlmock.NewRows([]string{"request_id"})) // deleted
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notifications" SET "cleared_at"=\$1 WHERE notification_id IN \(\$2,\$3,\$4\)`).
		WithArgs(dbnotifNonZeroTime{}, "n-1", "n-2", "n-4").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	n, err := CleanupOrphanedRequestNotifications(context.Background(), testUserID)
	require.NoError(t, err)
	assert.Equal(t, 3, n, "only the three orphans are cleared; the pending one survives")
}

func TestCleanupOrphanedRequestNotifications_SelectError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notifications"`).
		WillReturnError(errors.New("select failed"))

	n, err := CleanupOrphanedRequestNotifications(context.Background(), testUserID)
	assert.ErrorContains(t, err, "select failed")
	assert.Zero(t, n)
}
