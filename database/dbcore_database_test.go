package database

// Tests for the completion predicates in database.go: shouldCompleteTask,
// shouldCompleteMilestone, shouldCompleteRoadmap. These three cheap COUNT
// queries drive every auto-completion cascade (task -> milestone -> roadmap),
// so their edge cases (empty children, skipped steps) carry a lot of
// regression value.
//
// This file also hosts the shared helpers for the dbcore-* test files.
// All package-level identifiers are prefixed "dbcore" to avoid collisions
// with helpers added by other test files in this package.

import (
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const (
	dbcoreUserID      = "dddddddd-0000-0000-0000-000000000001"
	dbcoreRoadmapID   = "dddddddd-0000-0000-0000-000000000002"
	dbcoreMilestoneID = "dddddddd-0000-0000-0000-000000000003"
	dbcoreTaskID      = "dddddddd-0000-0000-0000-000000000004"
	dbcoreStepID      = "dddddddd-0000-0000-0000-000000000005"
	dbcorePhaseID     = "dddddddd-0000-0000-0000-000000000006"
	dbcoreTaskID2     = "dddddddd-0000-0000-0000-000000000007"
)

// dbcoreErrDB is the sentinel used to terminate flows early ("trim the tail"):
// several functions log-and-continue on recompute failures but surface reload
// failures, so tests error a SELECT with this and assert accordingly.
var dbcoreErrDB = errors.New("dbcore: simulated db failure")

// dbcoreCtx returns a gin context carrying user_id (empty userID leaves it
// unset, exercising the missing-user guard). A Request is always attached
// because some functions (GetRoadmapsByUser) read query params.
func dbcoreCtx(userID string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if userID != "" {
		c.Set("user_id", userID)
	}
	return c
}

// dbcoreCtxWithQuery is dbcoreCtx with a raw query string (e.g.
// "include_archived=true").
func dbcoreCtxWithQuery(userID, rawQuery string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	if userID != "" {
		c.Set("user_id", userID)
	}
	return c
}

// dbcoreMidnightUTC mirrors the "today at midnight UTC" normalization used by
// GetOverdueTasks. It calls time.Now() directly, so the test recomputes the
// same expression; only a run spanning midnight UTC could disagree.
func dbcoreMidnightUTC() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// dbcoreScheduleAnchor is today's scheduling anchor for the mock user, who
// resolves to UTC: dbcoreExpectUserTimezone stubs an empty timezone, and tests
// that don't stub it at all get the same fallback from the failed lookup.
func dbcoreScheduleAnchor() time.Time {
	return types.ScheduleDayIn(time.Now(), time.UTC)
}

// dbcoreExpectUserTimezone stubs the users.timezone lookup that the scheduling
// paths make to anchor dates on the owner's calendar day. An empty value keeps
// the mock user in UTC.
func dbcoreExpectUserTimezone(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT "timezone" FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID).
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow(""))
}

// dbcoreCountRows builds the single-column result for a COUNT query.
func dbcoreCountRows(n int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count"}).AddRow(n)
}

// dbcoreArgCapture is a sqlmock argument matcher that accepts anything and
// records the value, so a test can assert on generated values (share tokens)
// after the call.
type dbcoreArgCapture struct{ v *driver.Value }

func (c dbcoreArgCapture) Match(v driver.Value) bool {
	*c.v = v
	return true
}

// --- shouldCompleteTask ---

func TestShouldCompleteTask_NoSteps_ReturnsFalse(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(dbcoreCountRows(0))
	// No second (incomplete-count) query expected: cleanup's
	// ExpectationsWereMet proves the early return.

	assert.False(t, shouldCompleteTask(dbcoreTaskID),
		"a task with no steps must never auto-complete")
}

func TestShouldCompleteTask_IncompleteStepsRemain_ReturnsFalse(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(dbcoreCountRows(3))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1 AND status = \$2`).
		WithArgs(dbcoreTaskID, "incomplete").
		WillReturnRows(dbcoreCountRows(1))

	assert.False(t, shouldCompleteTask(dbcoreTaskID))
}

func TestShouldCompleteTask_AllStepsDoneOrSkipped_ReturnsTrue(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(dbcoreCountRows(2))
	// The second count filters status = 'incomplete' only, so a 'skipped'
	// step does NOT block completion — skipped counts as done.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "steps" WHERE task_id = \$1 AND status = \$2`).
		WithArgs(dbcoreTaskID, "incomplete").
		WillReturnRows(dbcoreCountRows(0))

	assert.True(t, shouldCompleteTask(dbcoreTaskID))
}

// --- shouldCompleteMilestone ---

func TestShouldCompleteMilestone_NoTasks_ReturnsFalse(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(0))

	assert.False(t, shouldCompleteMilestone(dbcoreMilestoneID),
		"an empty milestone must never auto-complete")
}

func TestShouldCompleteMilestone_IncompleteTasksRemain_ReturnsFalse(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(4))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1 AND completed = \$2`).
		WithArgs(dbcoreMilestoneID, false).
		WillReturnRows(dbcoreCountRows(2))

	assert.False(t, shouldCompleteMilestone(dbcoreMilestoneID))
}

func TestShouldCompleteMilestone_AllTasksCompleted_ReturnsTrue(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(4))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1 AND completed = \$2`).
		WithArgs(dbcoreMilestoneID, false).
		WillReturnRows(dbcoreCountRows(0))

	assert.True(t, shouldCompleteMilestone(dbcoreMilestoneID))
}

// --- shouldCompleteRoadmap ---

func TestShouldCompleteRoadmap_IncompleteMilestonesRemain_ReturnsFalse(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "milestones" WHERE roadmap_id = \$1 AND reached = \$2`).
		WithArgs(dbcoreRoadmapID, false).
		WillReturnRows(dbcoreCountRows(1))

	assert.False(t, shouldCompleteRoadmap(dbcoreRoadmapID))
}

func TestShouldCompleteRoadmap_ZeroIncomplete_ReturnsTrue(t *testing.T) {
	// Unlike tasks and milestones there is NO total-count guard here: a
	// roadmap with zero milestones also reports "complete". UpdateRoadmap
	// relies on this current behavior (empty roadmaps auto-complete).
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "milestones" WHERE roadmap_id = \$1 AND reached = \$2`).
		WithArgs(dbcoreRoadmapID, false).
		WillReturnRows(dbcoreCountRows(0))

	assert.True(t, shouldCompleteRoadmap(dbcoreRoadmapID),
		"zero incomplete milestones — including the 0-milestones case — reports complete")
}
