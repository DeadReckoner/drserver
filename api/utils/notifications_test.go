package utils

import (
	"testing"
	"time"

	"dr/server/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err, "sqlmock.New")
	gdb, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		// Drop the implicit BEGIN/COMMIT around a single Create so the
		// expectations below stay focused on the statements under test.
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "gorm.Open")
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet(), "unmet sqlmock expectations")
		_ = sqlDB.Close()
	})
	return gdb, mock
}

// Regression guard for the frontend-reported bug: a recipient with no active
// push token got the underlying event row (e.g. a pending friendship) but
// nothing on their Notifications screen, because the in-app `notifications`
// record was only written AFTER the "no tokens → return" early exit. The fix
// records it first; this test asserts the INSERT happens even when
// GetActivePushTokens returns nothing.
func TestSendPushNotification_RecordsInAppEvenWithoutTokens(t *testing.T) {
	gdb, mock := newMockDB(t)
	defer database.SetDBForTest(gdb)()

	// In-app notification is recorded first, unconditionally. GORM uses
	// INSERT ... RETURNING for the generated columns, so this is a Query.
	mock.ExpectQuery(`INSERT INTO "notifications"`).
		WillReturnRows(sqlmock.NewRows([]string{"notification_id", "created_at"}).
			AddRow("305d23c6-8865-4cd2-b602-cd6742e40bea", time.Now()))

	// Then the token lookup returns zero rows → push is skipped, function returns.
	mock.ExpectQuery(`SELECT \* FROM "push_tokens"`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "expo_token", "is_active"}))

	SendPushNotification(
		"11111111-1111-1111-1111-111111111111",
		"friend_request",
		"New Friend Request",
		"Pat wants to connect with you",
		map[string]any{"friendship_id": "f1"},
	)
}
