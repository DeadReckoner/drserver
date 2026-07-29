package database

// Shared fixtures for the db-notif test suite (notification settings/overrides,
// delivery log, notifications, push tokens, habits, pending/digest queues,
// background jobs, AI acknowledgments, and the email/entitlement/report
// ledgers). Every package-level identifier here carries the dbnotif prefix so
// it cannot collide with helpers added by other test suites in this package.
// The sqlmock plumbing itself (withMockDB / newSqlmockDB / testUserID) is
// reused from plan_check_test.go.

import (
	"database/sql/driver"
	"net/http/httptest"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

const (
	dbnotifFriendID  = "22221111-0000-0000-0000-000000000001"
	dbnotifRoadmapID = "22221111-0000-0000-0000-000000000002"
	dbnotifHabitID   = "22221111-0000-0000-0000-000000000003"
	dbnotifNotifID   = "22221111-0000-0000-0000-000000000004"
	dbnotifJobID     = "22221111-0000-0000-0000-000000000005"
	dbnotifTokenID   = "22221111-0000-0000-0000-000000000006"
	dbnotifRowID     = "22221111-0000-0000-0000-000000000007"
)

// dbnotifCtx builds a gin context carrying user_id AND a real *http.Request.
// The habits.go functions dereference c.Request.Context() before anything
// else, so unlike the plain gin.CreateTestContext pattern used elsewhere in
// this package, the Request field must be populated or every call would
// nil-panic. An empty userID skips the Set call so the missing-user_id guards
// can be exercised.
func dbnotifCtx(userID string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	if userID != "" {
		c.Set("user_id", userID)
	}
	return c
}

// dbnotifCountRow is a one-row count(*) result.
func dbnotifCountRow(n int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count"}).AddRow(n)
}

// dbnotifNonZeroTime matches any non-zero time.Time argument. Used where
// production code stamps time.Now() (updated_at injection, ReceivedAt
// backfill) so the assertion is "a real timestamp was set" without pinning
// the wall clock.
type dbnotifNonZeroTime struct{}

func (dbnotifNonZeroTime) Match(v driver.Value) bool {
	t, ok := v.(time.Time)
	return ok && !t.IsZero()
}

// dbnotifJSONArg matches a string or []byte argument byte-equal to its value.
// Used to pin serialized jsonb payloads (e.g. the nil-Data -> '{}'
// normalization contract) regardless of whether the serializer hands the
// driver a string or a byte slice.
type dbnotifJSONArg string

func (w dbnotifJSONArg) Match(v driver.Value) bool {
	switch s := v.(type) {
	case string:
		return s == string(w)
	case []byte:
		return string(s) == string(w)
	}
	return false
}
