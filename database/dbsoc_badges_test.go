package database

// Tests for the unexported badge counters and CalculateBadgeCount. The
// counters compute today's UTC midnight bounds via time.Now() internally, so
// date arguments are matched with sqlmock.AnyArg() to keep the tests
// date-stable.

import (
	"context"
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

// dbsocHabitRows builds a habits result set with the scheduling columns the
// badge counter needs to evaluate IsDueOn.
func dbsocHabitRows(recurrence types.RecurrenceType, daysOfWeek int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"habit_id", "user_id", "title", "recurrence_type", "days_of_week", "start_date",
	}).AddRow(dbsocHabitID, dbsocMeID, "Read 20 minutes", string(recurrence), daysOfWeek,
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
}

func dbsocExpectHabitsAndCompletions(mock sqlmock.Sqlmock, habits *sqlmock.Rows, completions *sqlmock.Rows) {
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(dbsocMeID, false).
		WillReturnRows(habits)
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE user_id = \$1 AND date = \$2`).
		WithArgs(dbsocMeID, sqlmock.AnyArg()).
		WillReturnRows(completions)
}

func dbsocEmptyCompletionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"completion_id", "habit_id", "user_id", "date"})
}

func TestCountUnreadMessages_JoinsParticipantsAndExcludesOwnMessages(t *testing.T) {
	mock := withMockDB(t)
	// The join + filters are the correctness contract: only threads the user
	// still belongs to (left_at IS NULL), only OTHER users' messages, only
	// unread, only non-deleted.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages" INNER JOIN thread_participants tp ON messages\.thread_id = tp\.thread_id WHERE tp\.user_id = \$1 AND tp\.left_at IS NULL AND messages\.user_id != \$2 AND messages\.read = \$3 AND messages\.deleted_at IS NULL`).
		WithArgs(dbsocMeID, dbsocMeID, false).
		WillReturnRows(dbsocCountRow(4))

	assert.Equal(t, 4, countUnreadMessages(context.Background(), dbsocMeID))
}

func TestCountUnreadMessages_QueryError_ReturnsZero(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages"`).
		WillReturnError(errors.New("count failed"))

	assert.Equal(t, 0, countUnreadMessages(context.Background(), dbsocMeID),
		"badge counters degrade to 0 on error, never propagate")
}

func TestCountIncompleteHabitsToday_DailyHabitNotCompleted_CountsOne(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectHabitsAndCompletions(mock, dbsocHabitRows(types.RecurrenceDaily, 0), dbsocEmptyCompletionRows())

	assert.Equal(t, 1, countIncompleteHabitsToday(context.Background(), dbsocMeID))
}

func TestCountIncompleteHabitsToday_CompletedToday_CountsZero(t *testing.T) {
	mock := withMockDB(t)
	completions := dbsocEmptyCompletionRows().
		AddRow("f1", dbsocHabitID, dbsocMeID, time.Now().UTC().Truncate(24*time.Hour))
	dbsocExpectHabitsAndCompletions(mock, dbsocHabitRows(types.RecurrenceDaily, 0), completions)

	assert.Equal(t, 0, countIncompleteHabitsToday(context.Background(), dbsocMeID))
}

func TestCountIncompleteHabitsToday_HabitNotDueToday_CountsZero(t *testing.T) {
	mock := withMockDB(t)
	// Weekly habit whose day mask excludes today's weekday (computed
	// dynamically so the test holds on any day of the week).
	todayBit := int(types.DayOfWeekFromTime(time.Now().UTC().Weekday()))
	mask := int(types.EveryDay) &^ todayBit
	dbsocExpectHabitsAndCompletions(mock, dbsocHabitRows(types.RecurrenceWeekly, mask), dbsocEmptyCompletionRows())

	assert.Equal(t, 0, countIncompleteHabitsToday(context.Background(), dbsocMeID))
}

func TestCountIncompleteHabitsToday_HabitsQueryError_ReturnsZero(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits"`).
		WillReturnError(errors.New("habits failed"))

	assert.Equal(t, 0, countIncompleteHabitsToday(context.Background(), dbsocMeID))
}

func TestCountIncompleteHabitsToday_CompletionsQueryError_ReturnsZero(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(dbsocMeID, false).
		WillReturnRows(dbsocHabitRows(types.RecurrenceDaily, 0))
	mock.ExpectQuery(`SELECT \* FROM "habit_completions"`).
		WillReturnError(errors.New("completions failed"))

	assert.Equal(t, 0, countIncompleteHabitsToday(context.Background(), dbsocMeID))
}

func TestCountIncompleteTasksToday_CountsWithinDayWindow(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE user_id = \$1 AND completed = \$2 AND date >= \$3 AND date < \$4`).
		WithArgs(dbsocMeID, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(dbsocCountRow(3))

	assert.Equal(t, 3, countIncompleteTasksToday(context.Background(), dbsocMeID))
}

func TestCountIncompleteTasksToday_QueryError_ReturnsZero(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks"`).
		WillReturnError(errors.New("tasks failed"))

	assert.Equal(t, 0, countIncompleteTasksToday(context.Background(), dbsocMeID))
}

func TestCountPendingFriendRequests_IncomingOnly(t *testing.T) {
	mock := withMockDB(t)
	// Filters on addressee_id: only requests SENT TO the user badge, not the
	// ones they sent.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "friendships" WHERE addressee_id = \$1 AND status = \$2`).
		WithArgs(dbsocMeID, "pending").
		WillReturnRows(dbsocCountRow(2))

	assert.Equal(t, 2, countPendingFriendRequests(context.Background(), dbsocMeID))
}

func TestCountPendingFriendRequests_QueryError_ReturnsZero(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "friendships"`).
		WillReturnError(errors.New("friendships failed"))

	assert.Equal(t, 0, countPendingFriendRequests(context.Background(), dbsocMeID))
}

func TestCountPendingAnchorRequests_FiltersOnAnchorAndPending(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "anchor_requests" WHERE anchor_id = \$1 AND status = \$2`).
		WithArgs(dbsocMeID, "pending").
		WillReturnRows(dbsocCountRow(1))

	assert.Equal(t, 1, countPendingAnchorRequests(context.Background(), dbsocMeID))
}

func TestCountPendingAnchorRequests_QueryError_ReturnsZero(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "anchor_requests"`).
		WillReturnError(errors.New("anchor requests failed"))

	assert.Equal(t, 0, countPendingAnchorRequests(context.Background(), dbsocMeID))
}

func TestCalculateBadgeCount_SumsAllFiveCounters(t *testing.T) {
	mock := withMockDB(t)
	// messages=2, habits=1 (daily habit, no completion), tasks=0,
	// friend requests=3, anchor requests=1 => total 7.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages"`).
		WillReturnRows(dbsocCountRow(2))
	dbsocExpectHabitsAndCompletions(mock, dbsocHabitRows(types.RecurrenceDaily, 0), dbsocEmptyCompletionRows())
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks"`).
		WillReturnRows(dbsocCountRow(0))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "friendships"`).
		WillReturnRows(dbsocCountRow(3))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "anchor_requests"`).
		WillReturnRows(dbsocCountRow(1))

	assert.Equal(t, 7, CalculateBadgeCount(context.Background(), dbsocMeID))
}

func TestCalculateBadgeCount_OneCounterFailing_OthersStillSum(t *testing.T) {
	mock := withMockDB(t)
	// The messages counter errors and contributes 0; the remaining counters
	// still sum (partial-failure resilience: a bad join never zeroes the
	// whole badge).
	mock.ExpectQuery(`SELECT count\(\*\) FROM "messages"`).
		WillReturnError(errors.New("messages failed"))
	dbsocExpectHabitsAndCompletions(mock, dbsocHabitRows(types.RecurrenceDaily, 0), dbsocEmptyCompletionRows())
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks"`).
		WillReturnRows(dbsocCountRow(2))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "friendships"`).
		WillReturnRows(dbsocCountRow(1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "anchor_requests"`).
		WillReturnRows(dbsocCountRow(1))

	assert.Equal(t, 5, CalculateBadgeCount(context.Background(), dbsocMeID))
}
