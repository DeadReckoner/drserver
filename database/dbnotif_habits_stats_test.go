package database

// Tests for the habits.go statistics surface: GetHabitsForDate,
// GetHabitsForDateRange, GetStreak, GetCompletionRate, GetTotalCompletions.
//
// Determinism note: GetStreak and GetCompletionRate call time.Now() directly
// (not the database.Now clock seam), so their fixtures are built relative to
// wall-clock "today" truncated to local midnight. The assertions only depend
// on day OFFSETS from today, so they hold on any date; the one theoretical
// flake is a test that straddles local midnight between building the fixture
// and the function's own time.Now() call. Making these boundary-precise
// (e.g. streaks across DST) needs a refactor onto the clock seam.
// GetHabitsForDate/Range take the date as a parameter and use fixed dates.

import (
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dbnotifDailyHabitID  = "33331111-0000-0000-0000-000000000001"
	dbnotifWeeklyHabitID = "33331111-0000-0000-0000-000000000002"
)

// dbnotifStatsHabitCols is the column set for habit rows in this file —
// includes recurrence fields so IsDueOn gating is exercised.
func dbnotifStatsHabitCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"habit_id", "user_id", "title", "recurrence_type", "days_of_week", "start_date"})
}

// dbnotifLocalToday returns time.Now() truncated to local midnight — the same
// normalization GetStreak applies to its walking cursor.
func dbnotifLocalToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func TestHabitStats_MissingUserID_NoSQL(t *testing.T) {
	withMockDB(t) // zero expectations
	date := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	calls := []struct {
		name string
		call func() error
	}{
		{"GetHabitsForDate", func() error { _, err := GetHabitsForDate(dbnotifCtx(""), date); return err }},
		{"GetHabitsForDateRange", func() error { _, err := GetHabitsForDateRange(dbnotifCtx(""), date, date); return err }},
		{"GetStreak", func() error { _, err := GetStreak(dbnotifCtx(""), dbnotifHabitID); return err }},
		{"GetCompletionRate", func() error { _, err := GetCompletionRate(dbnotifCtx(""), dbnotifHabitID, 7); return err }},
		{"GetTotalCompletions", func() error { _, err := GetTotalCompletions(dbnotifCtx(""), dbnotifHabitID); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.call(), types.ErrHabitNotFound)
		})
	}
}

// =============================================================================
// GetHabitsForDate
// =============================================================================

func TestGetHabitsForDate_DueGatingAndCompletionStatus(t *testing.T) {
	// 2026-07-20 is a Monday; the weekly habit is Tuesday-only, so IsDueOn
	// must filter it out and only the daily habit gets a completion lookup.
	date := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	dateOnly := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 7, 20, 8, 30, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(testUserID, false).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, start).
			AddRow(dbnotifWeeklyHabitID, testUserID, "Tuesdays", "weekly", int(types.Tuesday), start))
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 AND date = \$2`).
		WithArgs(dbnotifDailyHabitID, dateOnly, 1).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id", "habit_id", "completed_at"}).
			AddRow(dbnotifRowID, dbnotifDailyHabitID, completedAt))

	results, err := GetHabitsForDate(dbnotifCtx(testUserID), date)
	require.NoError(t, err)
	require.Len(t, results, 1, "weekly habit not due on a Monday is filtered out")
	assert.Equal(t, dbnotifDailyHabitID, results[0].HabitID)
	assert.True(t, results[0].IsCompleted)
	require.NotNil(t, results[0].CompletedAt)
	assert.Equal(t, completedAt, *results[0].CompletedAt)
}

func TestGetHabitsForDate_NoCompletion_NotCompleted(t *testing.T) {
	date := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(testUserID, false).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, start))
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 AND date = \$2`).
		WithArgs(dbnotifDailyHabitID, date, 1).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id"})) // no completion row

	results, err := GetHabitsForDate(dbnotifCtx(testUserID), date)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].IsCompleted)
	assert.Nil(t, results[0].CompletedAt)
}

func TestGetHabitsForDate_HabitQueryError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits"`).
		WillReturnError(errors.New("select failed"))

	_, err := GetHabitsForDate(dbnotifCtx(testUserID), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	assert.ErrorContains(t, err, "select failed")
}

// =============================================================================
// GetHabitsForDateRange
// =============================================================================

func TestGetHabitsForDateRange_PerDayMembershipAndCompletionCells(t *testing.T) {
	// Mon 2026-07-20 .. Wed 2026-07-22 with one daily habit and one
	// Tuesday-only weekly habit. The single completion belongs to the weekly
	// habit on the Tuesday — it must land in exactly that (date, habit) cell.
	startDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tuesday := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	habitStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 7, 21, 7, 15, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(testUserID, false).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, habitStart).
			AddRow(dbnotifWeeklyHabitID, testUserID, "Tuesdays", "weekly", int(types.Tuesday), habitStart))
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE user_id = \$1 AND date >= \$2 AND date <= \$3`).
		WithArgs(testUserID, startDate, endDate).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id", "habit_id", "user_id", "date", "completed_at"}).
			AddRow(dbnotifRowID, dbnotifWeeklyHabitID, testUserID, tuesday, completedAt))

	results, err := GetHabitsForDateRange(dbnotifCtx(testUserID), startDate, endDate)
	require.NoError(t, err)
	require.Len(t, results, 3, "one map entry per day in the range")

	monday := results["2026-07-20"]
	require.Len(t, monday, 1, "only the daily habit is due Monday")
	assert.Equal(t, dbnotifDailyHabitID, monday[0].HabitID)
	assert.False(t, monday[0].IsCompleted)

	tue := results["2026-07-21"]
	require.Len(t, tue, 2, "both habits due Tuesday")
	assert.Equal(t, dbnotifDailyHabitID, tue[0].HabitID)
	assert.False(t, tue[0].IsCompleted, "completion must not bleed onto the daily habit")
	assert.Equal(t, dbnotifWeeklyHabitID, tue[1].HabitID)
	assert.True(t, tue[1].IsCompleted)
	require.NotNil(t, tue[1].CompletedAt)
	assert.Equal(t, completedAt, *tue[1].CompletedAt)

	wednesday := results["2026-07-22"]
	require.Len(t, wednesday, 1)
	assert.False(t, wednesday[0].IsCompleted)
}

func TestGetHabitsForDateRange_CompletionsQueryError(t *testing.T) {
	startDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(testUserID, false).
		WillReturnRows(dbnotifStatsHabitCols())
	mock.ExpectQuery(`SELECT \* FROM "habit_completions"`).
		WillReturnError(errors.New("completions failed"))

	_, err := GetHabitsForDateRange(dbnotifCtx(testUserID), startDate, startDate)
	assert.ErrorContains(t, err, "completions failed")
}

// =============================================================================
// GetStreak
// =============================================================================

func TestGetStreak_HabitNotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id"}))

	_, err := GetStreak(dbnotifCtx(testUserID), dbnotifHabitID)
	assert.ErrorIs(t, err, types.ErrHabitNotFound)
}

func TestGetStreak_NoCompletions_Zero(t *testing.T) {
	today := dbnotifLocalToday()
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifDailyHabitID, testUserID, 1).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, today.AddDate(0, 0, -10)))
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 ORDER BY date DESC`).
		WithArgs(dbnotifDailyHabitID).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id"}))

	streak, err := GetStreak(dbnotifCtx(testUserID), dbnotifDailyHabitID)
	require.NoError(t, err)
	assert.Zero(t, streak)
}

// dbnotifExpectStreakFixture wires the habit row and a completions result set
// whose dates are the given day-offsets back from local-midnight today.
func dbnotifExpectStreakFixture(mock sqlmock.Sqlmock, recurrence string, daysOfWeek int, offsets ...int) {
	today := dbnotifLocalToday()
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifDailyHabitID, testUserID, 1).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Habit", recurrence, daysOfWeek, today.AddDate(0, 0, -10)))
	completions := sqlmock.NewRows([]string{"completion_id", "habit_id", "date"})
	for i, off := range offsets {
		completions.AddRow(dbnotifRowID[:35]+string(rune('a'+i)), dbnotifDailyHabitID, today.AddDate(0, 0, -off))
	}
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 ORDER BY date DESC`).
		WithArgs(dbnotifDailyHabitID).
		WillReturnRows(completions)
}

func TestGetStreak_DailyConsecutive(t *testing.T) {
	mock := withMockDB(t)
	dbnotifExpectStreakFixture(mock, "daily", 0, 0, 1, 2) // today, yesterday, 2 days ago

	streak, err := GetStreak(dbnotifCtx(testUserID), dbnotifDailyHabitID)
	require.NoError(t, err)
	assert.Equal(t, 3, streak)
}

func TestGetStreak_DailyGap_StopsAtGap(t *testing.T) {
	mock := withMockDB(t)
	dbnotifExpectStreakFixture(mock, "daily", 0, 0, 3) // today, then a 2-day hole

	streak, err := GetStreak(dbnotifCtx(testUserID), dbnotifDailyHabitID)
	require.NoError(t, err)
	assert.Equal(t, 1, streak, "the missed day breaks the streak")
}

func TestGetStreak_Weekly_WalksOverNonDueDays(t *testing.T) {
	// Due only on today's weekday and the weekday two days back, so the
	// walker must skip yesterday (non-due) without breaking the streak.
	today := dbnotifLocalToday()
	mask := int(types.DayOfWeekFromTime(today.Weekday()) | types.DayOfWeekFromTime(today.AddDate(0, 0, -2).Weekday()))

	mock := withMockDB(t)
	dbnotifExpectStreakFixture(mock, "weekly", mask, 0, 2) // completions on both due days

	streak, err := GetStreak(dbnotifCtx(testUserID), dbnotifDailyHabitID)
	require.NoError(t, err)
	assert.Equal(t, 2, streak)
}

func TestGetStreak_CompletionsQueryError(t *testing.T) {
	today := dbnotifLocalToday()
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifDailyHabitID, testUserID, 1).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, today.AddDate(0, 0, -10)))
	mock.ExpectQuery(`SELECT \* FROM "habit_completions"`).
		WillReturnError(errors.New("completions failed"))

	_, err := GetStreak(dbnotifCtx(testUserID), dbnotifDailyHabitID)
	assert.ErrorContains(t, err, "completions failed")
}

// =============================================================================
// GetCompletionRate
// =============================================================================

func TestGetCompletionRate_Daily(t *testing.T) {
	today := dbnotifLocalToday()
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifDailyHabitID, testUserID, 1).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, today.AddDate(0, 0, -30)))
	// A 7-day window walked inclusively is 8 due days for a daily habit.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habit_completions" WHERE habit_id = \$1 AND date >= \$2 AND date <= \$3`).
		WithArgs(dbnotifDailyHabitID, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(dbnotifCountRow(4))

	rate, err := GetCompletionRate(dbnotifCtx(testUserID), dbnotifDailyHabitID, 7)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, rate, 1e-9, "4 completions over 8 expected due days")
}

func TestGetCompletionRate_NoExpectedDueDays_ZeroWithoutCount(t *testing.T) {
	// StartDate in the future: IsDueOn is never true in the window, so the
	// function must return 0 WITHOUT running the completion-count query
	// (which would divide by zero otherwise).
	today := dbnotifLocalToday()
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifDailyHabitID, testUserID, 1).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Future", "daily", 0, today.AddDate(0, 0, 10)))

	rate, err := GetCompletionRate(dbnotifCtx(testUserID), dbnotifDailyHabitID, 7)
	require.NoError(t, err)
	assert.Zero(t, rate)
}

func TestGetCompletionRate_HabitNotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id"}))

	_, err := GetCompletionRate(dbnotifCtx(testUserID), dbnotifHabitID, 7)
	assert.ErrorIs(t, err, types.ErrHabitNotFound)
}

func TestGetCompletionRate_CountError(t *testing.T) {
	today := dbnotifLocalToday()
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifDailyHabitID, testUserID, 1).
		WillReturnRows(dbnotifStatsHabitCols().
			AddRow(dbnotifDailyHabitID, testUserID, "Daily", "daily", 0, today.AddDate(0, 0, -30)))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habit_completions"`).
		WillReturnError(errors.New("count failed"))

	_, err := GetCompletionRate(dbnotifCtx(testUserID), dbnotifDailyHabitID, 7)
	assert.ErrorContains(t, err, "count failed")
}

// =============================================================================
// GetTotalCompletions
// =============================================================================

func TestGetTotalCompletions(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habit_completions" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID).
		WillReturnRows(dbnotifCountRow(12))

	n, err := GetTotalCompletions(dbnotifCtx(testUserID), dbnotifHabitID)
	require.NoError(t, err)
	assert.Equal(t, int64(12), n)
}

func TestGetTotalCompletions_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habit_completions"`).
		WillReturnError(errors.New("count failed"))

	n, err := GetTotalCompletions(dbnotifCtx(testUserID), dbnotifHabitID)
	assert.ErrorContains(t, err, "count failed")
	assert.Zero(t, n)
}
