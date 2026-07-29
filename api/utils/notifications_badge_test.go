package utils

import (
	"context"
	"errors"
	"testing"

	"dr/server/database"
	"dr/server/log"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

// umwQuietLogs raises the minimum log level so the notification helpers'
// info/warn chatter stays out of test output; restored via t.Cleanup. Pass
// log.LevelError+1 to silence everything including expected error lines.
func umwQuietLogs(t *testing.T, min log.Level) {
	t.Helper()
	restore := log.SetMinLevel(min)
	t.Cleanup(restore)
}

// updateNotificationStatus uses a GORM map update, which orders SET columns
// alphabetically. status="sent" adds sent_at and omits error.
func TestUpdateNotificationStatus_SentIncludesSentAt(t *testing.T) {
	umwQuietLogs(t, log.LevelError)
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	mock.ExpectExec(`UPDATE "notifications" SET "sent_at"=\$1,"status"=\$2 WHERE notification_id = \$3`).
		WithArgs(sqlmock.AnyArg(), string(types.NotificationStatusSent), "notif-sent-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	updateNotificationStatus(context.Background(), "notif-sent-1", string(types.NotificationStatusSent), "")
}

// status="failed" with an error message writes error + status and no sent_at.
func TestUpdateNotificationStatus_FailedIncludesError(t *testing.T) {
	umwQuietLogs(t, log.LevelError)
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	mock.ExpectExec(`UPDATE "notifications" SET "error"=\$1,"status"=\$2 WHERE notification_id = \$3`).
		WithArgs("expo said no", string(types.NotificationStatusFailed), "notif-fail-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	updateNotificationStatus(context.Background(), "notif-fail-1", string(types.NotificationStatusFailed), "expo said no")
}

// A DB failure is logged and swallowed — the push pipeline must never panic
// over a status-bookkeeping write.
func TestUpdateNotificationStatus_DBErrorDoesNotPanic(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	mock.ExpectExec(`UPDATE "notifications"`).
		WillReturnError(errors.New("db down"))

	assert.NotPanics(t, func() {
		updateNotificationStatus(context.Background(), "notif-err-1", string(types.NotificationStatusSent), "")
	})
}

// With no active push tokens SendBadgeUpdate returns nil immediately; the
// cleanup's ExpectationsWereMet proves no badge-count queries were issued.
func TestSendBadgeUpdate_NoTokensSkipsBadgeQueries(t *testing.T) {
	umwQuietLogs(t, log.LevelError)
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	mock.ExpectQuery(`SELECT \* FROM "push_tokens"`).
		WillReturnRows(sqlmock.NewRows([]string{"token_id", "user_id", "expo_token", "is_active"}))

	err := SendBadgeUpdate("11111111-1111-1111-1111-111111111111")
	assert.NoError(t, err)
}

// GetBadgeCount sums five sources: unread messages, incomplete habits today,
// incomplete tasks today, pending friend requests, pending anchor requests.
// Habits are returned empty (the habit contribution is computed in-process
// from time.Now, so rows here would make the test date-dependent).
func TestGetBadgeCount_SumsAllSources(t *testing.T) {
	umwQuietLogs(t, log.LevelError)
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	// 1. Unread messages across threads → 2
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	// 2a. Active habits → none (keeps the in-memory due-today logic inert)
	mock.ExpectQuery(`SELECT \* FROM "habits"`).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "archived"}))
	// 2b. Today's completions → none
	mock.ExpectQuery(`SELECT \* FROM "habit_completions"`).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id", "habit_id", "user_id", "date"}))
	// 3. Incomplete tasks due today → 3
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	// 4. Pending friend requests → 1
	mock.ExpectQuery(`SELECT count\(\*\) FROM "friendships"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// 5. Pending anchor requests → 1
	mock.ExpectQuery(`SELECT count\(\*\) FROM "anchor_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	got := GetBadgeCount("11111111-1111-1111-1111-111111111111")
	assert.Equal(t, 7, got)
}

// Even when recording the in-app notification fails, SendPushNotification
// carries on to the token lookup and returns cleanly — push delivery is not
// held hostage by the bookkeeping INSERT.
func TestSendPushNotification_CreateFailureStillChecksTokens(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	// GORM issues the INSERT as a Query (INSERT ... RETURNING).
	mock.ExpectQuery(`INSERT INTO "notifications"`).
		WillReturnError(errors.New("insert exploded"))

	// The function proceeds to the token lookup regardless; zero tokens ends
	// the call before any goroutine can reach the network.
	mock.ExpectQuery(`SELECT \* FROM "push_tokens"`).
		WillReturnRows(sqlmock.NewRows([]string{"token_id", "user_id", "expo_token", "is_active"}))

	assert.NotPanics(t, func() {
		SendPushNotification(
			"22222222-2222-2222-2222-222222222222",
			"nudge",
			"Nudge",
			"Your anchor nudged you",
			map[string]any{"roadmap_id": "r1"},
		)
	})
}
