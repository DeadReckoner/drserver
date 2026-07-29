package database

// Shared fixtures for the db-social test suite (friends, anchor requests,
// messages, blocks, nudges, badges, user). Every package-level identifier here
// carries the dbsoc prefix so it cannot collide with helpers added by other
// test suites in this package. The sqlmock plumbing itself (withMockDB /
// newSqlmockDB) is reused from plan_check_test.go.

import (
	"database/sql/driver"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	dbsocMeID         = "aaaa1111-0000-0000-0000-000000000001"
	dbsocFriendID     = "aaaa1111-0000-0000-0000-000000000002"
	dbsocOtherID      = "aaaa1111-0000-0000-0000-000000000003"
	dbsocThreadID     = "bbbb1111-0000-0000-0000-000000000001"
	dbsocMessageID    = "bbbb1111-0000-0000-0000-000000000002"
	dbsocRoadmapID    = "cccc1111-0000-0000-0000-000000000001"
	dbsocHabitID      = "cccc1111-0000-0000-0000-000000000002"
	dbsocRequestID    = "dddd1111-0000-0000-0000-000000000001"
	dbsocFriendshipID = "dddd1111-0000-0000-0000-000000000002"
)

// dbsocJSONBConverter lets sqlmock accept map-typed jsonb columns (e.g.
// Friendship.Restrictions). The real pgx driver serializes maps to jsonb;
// sqlmock's default converter rejects them, so writes against models carrying
// a map field need this shim. Everything else falls through to the default.
type dbsocJSONBConverter struct{}

func (dbsocJSONBConverter) ConvertValue(v any) (driver.Value, error) {
	if m, ok := v.(map[string]any); ok {
		if m == nil {
			return nil, nil
		}
		return json.Marshal(m)
	}
	return driver.DefaultParameterConverter.ConvertValue(v)
}

// dbsocMockDB is withMockDB (plan_check_test.go) plus the jsonb converter
// above. Use it for tests that INSERT/UPDATE models with map fields.
func dbsocMockDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp),
		sqlmock.ValueConverterOption(dbsocJSONBConverter{}),
	)
	require.NoError(t, err, "sqlmock.New")
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open: %v", err)
	}
	restore := SetDBForTest(gormDB)
	t.Cleanup(func() {
		restore()
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = sqlDB.Close()
	})
	return mock
}

// dbsocCtx builds a gin context carrying user_id, the auth seam every
// ctx-taking database function reads. An empty userID skips the Set call so
// the missing-user_id guards can be exercised.
func dbsocCtx(userID string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if userID != "" {
		c.Set("user_id", userID)
	}
	return c
}

// dbsocUserRow is a single-user result set for SELECT * FROM "users".
func dbsocUserRow(id string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"user_id", "first_name", "last_name", "email"}).
		AddRow(id, "Test", "User", id+"@example.com")
}

// dbsocFriendshipRow is a single-friendship result set.
func dbsocFriendshipRow(requester, addressee, status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "requester_id", "addressee_id", "status"}).
		AddRow(dbsocFriendshipID, requester, addressee, status)
}

// dbsocAnchorRequestRow is a single-anchor-request result set. roadmapID and
// habitID accept nil for the NULL side of the roadmap/habit union.
func dbsocAnchorRequestRow(targetType string, roadmapID, habitID any, requester, anchor, status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"request_id", "target_type", "roadmap_id", "habit_id",
		"requester_id", "anchor_id", "status",
	}).AddRow(dbsocRequestID, targetType, roadmapID, habitID, requester, anchor, status)
}

// dbsocCountRow is a one-row count(*) result.
func dbsocCountRow(n int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count"}).AddRow(n)
}
