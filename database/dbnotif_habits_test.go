package database

// Tests for the habits.go CRUD + completion surface. Every function here
// reads c.Request.Context() before anything else, so all calls go through
// dbnotifCtx (which attaches a real *http.Request). The missing-user_id table
// pins the auth-guard contract across the whole surface: ErrHabitNotFound
// (or a zero-value Habit for GetHabit) with ZERO SQL issued.

import (
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dbnotifHabitRow is a partial habits result set.
func dbnotifHabitRow(habitID, title string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"habit_id", "user_id", "title", "recurrence_type", "start_date"}).
		AddRow(habitID, testUserID, title, "daily", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

func TestHabits_MissingUserID_NoSQL(t *testing.T) {
	withMockDB(t) // zero expectations: every guard must fire before any query
	date := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	calls := []struct {
		name string
		call func() error
	}{
		{"CreateHabit", func() error { _, err := CreateHabit(dbnotifCtx(""), types.Habit{Title: "x"}); return err }},
		{"GetAllHabits", func() error { _, err := GetAllHabits(dbnotifCtx(""), false); return err }},
		{"UpdateHabit", func() error { _, err := UpdateHabit(dbnotifCtx(""), dbnotifHabitID, types.Habit{}); return err }},
		{"DeleteHabit", func() error { return DeleteHabit(dbnotifCtx(""), dbnotifHabitID) }},
		{"ArchiveHabit", func() error { return ArchiveHabit(dbnotifCtx(""), dbnotifHabitID) }},
		{"CompleteHabit", func() error { _, err := CompleteHabit(dbnotifCtx(""), dbnotifHabitID, date, ""); return err }},
		{"UncompleteHabit", func() error { return UncompleteHabit(dbnotifCtx(""), dbnotifHabitID, date) }},
		{"GetCompletions", func() error { _, err := GetCompletions(dbnotifCtx(""), dbnotifHabitID, 10); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.call(), types.ErrHabitNotFound)
		})
	}

	t.Run("GetHabit returns zero-value Habit", func(t *testing.T) {
		// GetHabit swallows the guard into a zero struct instead of an error.
		habit := GetHabit(dbnotifCtx(""), dbnotifHabitID)
		assert.Zero(t, habit)
	})
}

// =============================================================================
// CreateHabit
// =============================================================================

func TestCreateHabit_StampsUserIDFromContext(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// user_id is the first insert column — it must come from the auth
	// context, not from whatever the request body claimed.
	mock.ExpectQuery(`INSERT INTO "habits"`).
		WithArgs(testUserID,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id"}).AddRow(dbnotifHabitID))
	mock.ExpectCommit()
	// Post-create re-fetch by the generated habit_id.
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1`).
		WithArgs(dbnotifHabitID, 1).
		WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "Read daily"))

	created, err := CreateHabit(dbnotifCtx(testUserID), types.Habit{
		Title:          "Read daily",
		RecurrenceType: types.RecurrenceDaily,
		StartDate:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Equal(t, dbnotifHabitID, created.HabitID)
	assert.Equal(t, "Read daily", created.Title)
}

func TestCreateHabit_InsertError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "habits"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	_, err := CreateHabit(dbnotifCtx(testUserID), types.Habit{Title: "x", RecurrenceType: types.RecurrenceDaily})
	assert.ErrorContains(t, err, "insert failed")
}

func TestCreateHabit_RefetchError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "habits"`).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id"}).AddRow(dbnotifHabitID))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1`).
		WillReturnError(errors.New("refetch failed"))

	_, err := CreateHabit(dbnotifCtx(testUserID), types.Habit{Title: "x", RecurrenceType: types.RecurrenceDaily})
	assert.ErrorContains(t, err, "refetch failed")
}

// =============================================================================
// GetHabit / GetAllHabits
// =============================================================================

func TestGetHabit_Found(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "Meditate"))

	habit := GetHabit(dbnotifCtx(testUserID), dbnotifHabitID)
	assert.Equal(t, "Meditate", habit.Title)
}

func TestGetHabit_NotFound_ReturnsZeroValue(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id"}))

	// Locks in the error-swallowing contract: handlers detect "not found" by
	// checking for an empty HabitID, not by an error return.
	habit := GetHabit(dbnotifCtx(testUserID), dbnotifHabitID)
	assert.Zero(t, habit)
}

func TestGetAllHabits_ArchiveFilter(t *testing.T) {
	t.Run("excludes archived by default", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 AND archived = \$2 ORDER BY created_at DESC`).
			WithArgs(testUserID, false).
			WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "Meditate"))

		habits, err := GetAllHabits(dbnotifCtx(testUserID), false)
		require.NoError(t, err)
		assert.Len(t, habits, 1)
	})

	t.Run("includeArchived drops the predicate", func(t *testing.T) {
		mock := withMockDB(t)
		// The WHERE..ORDER BY span leaves no room for an archived clause.
		mock.ExpectQuery(`SELECT \* FROM "habits" WHERE user_id = \$1 ORDER BY created_at DESC`).
			WithArgs(testUserID).
			WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "Meditate"))

		habits, err := GetAllHabits(dbnotifCtx(testUserID), true)
		require.NoError(t, err)
		assert.Len(t, habits, 1)
	})

	t.Run("query error", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "habits"`).
			WillReturnError(errors.New("select failed"))

		_, err := GetAllHabits(dbnotifCtx(testUserID), false)
		assert.ErrorContains(t, err, "select failed")
	})
}

// =============================================================================
// UpdateHabit
// =============================================================================

func TestUpdateHabit_NotFound_NoWrite(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id"}))

	_, err := UpdateHabit(dbnotifCtx(testUserID), dbnotifHabitID, types.Habit{Title: "New"})
	assert.ErrorIs(t, err, types.ErrHabitNotFound)
}

func TestUpdateHabit_HappyPath(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "Old title"))
	mock.ExpectBegin()
	// Struct-based Updates: only the non-zero patch fields plus updated_at,
	// keyed on the fetched row's primary key.
	mock.ExpectExec(`UPDATE "habits" SET "title"=\$1,"updated_at"=\$2 WHERE "habit_id" = \$3`).
		WithArgs("New title", dbnotifNonZeroTime{}, dbnotifHabitID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// The re-fetch reuses the fetched struct, so gorm adds its primary key as
	// an extra predicate alongside the explicit habit_id condition.
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND "habits"\."habit_id" = \$2`).
		WithArgs(dbnotifHabitID, dbnotifHabitID, 1).
		WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "New title"))

	updated, err := UpdateHabit(dbnotifCtx(testUserID), dbnotifHabitID, types.Habit{Title: "New title"})
	require.NoError(t, err)
	assert.Equal(t, "New title", updated.Title)
}

func TestUpdateHabit_UpdateError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifHabitID, testUserID, 1).
		WillReturnRows(dbnotifHabitRow(dbnotifHabitID, "Old title"))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "habits"`).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	_, err := UpdateHabit(dbnotifCtx(testUserID), dbnotifHabitID, types.Habit{Title: "New title"})
	assert.ErrorContains(t, err, "update failed")
}

// =============================================================================
// DeleteHabit / ArchiveHabit
// =============================================================================

func TestDeleteHabit(t *testing.T) {
	t.Run("happy path scoped to user", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
			WithArgs(dbnotifHabitID, testUserID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		require.NoError(t, DeleteHabit(dbnotifCtx(testUserID), dbnotifHabitID))
	})

	t.Run("zero rows -> not found", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "habits" WHERE habit_id = \$1 AND user_id = \$2`).
			WithArgs(dbnotifHabitID, testUserID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		assert.ErrorIs(t, DeleteHabit(dbnotifCtx(testUserID), dbnotifHabitID), types.ErrHabitNotFound)
	})

	t.Run("db error propagates", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "habits"`).
			WillReturnError(errors.New("delete failed"))
		mock.ExpectRollback()

		assert.ErrorContains(t, DeleteHabit(dbnotifCtx(testUserID), dbnotifHabitID), "delete failed")
	})
}

func TestArchiveHabit(t *testing.T) {
	t.Run("happy path scoped to user", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "habits" SET "archived"=\$1,"updated_at"=\$2 WHERE habit_id = \$3 AND user_id = \$4`).
			WithArgs(true, dbnotifNonZeroTime{}, dbnotifHabitID, testUserID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		require.NoError(t, ArchiveHabit(dbnotifCtx(testUserID), dbnotifHabitID))
	})

	t.Run("zero rows -> not found", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "habits" SET "archived"=\$1,"updated_at"=\$2 WHERE habit_id = \$3 AND user_id = \$4`).
			WithArgs(true, dbnotifNonZeroTime{}, dbnotifHabitID, testUserID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		assert.ErrorIs(t, ArchiveHabit(dbnotifCtx(testUserID), dbnotifHabitID), types.ErrHabitNotFound)
	})

	t.Run("db error propagates", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "habits"`).
			WillReturnError(errors.New("update failed"))
		mock.ExpectRollback()

		assert.ErrorContains(t, ArchiveHabit(dbnotifCtx(testUserID), dbnotifHabitID), "update failed")
	})
}

// =============================================================================
// CompleteHabit / UncompleteHabit / GetCompletions
// =============================================================================

func TestCompleteHabit_NormalizesDateAndCreates(t *testing.T) {
	// An afternoon local-ish timestamp must be normalized to UTC midnight in
	// BOTH the dedup lookup and the insert.
	input := time.Date(2026, 7, 20, 15, 42, 7, 0, time.UTC)
	utcMidnight := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 AND date = \$2`).
		WithArgs(dbnotifHabitID, utcMidnight, 1).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id"})) // not yet completed
	mock.ExpectBegin()
	// Insert columns: habit_id, user_id, completed_at, date, notes, created_at.
	mock.ExpectQuery(`INSERT INTO "habit_completions"`).
		WithArgs(dbnotifHabitID, testUserID, dbnotifNonZeroTime{}, utcMidnight, "felt great", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id"}).AddRow(dbnotifRowID))
	mock.ExpectCommit()

	completion, err := CompleteHabit(dbnotifCtx(testUserID), dbnotifHabitID, input, "felt great")
	require.NoError(t, err)
	require.NotNil(t, completion)
	assert.Equal(t, dbnotifRowID, completion.CompletionID)
	assert.Equal(t, utcMidnight, completion.Date)
}

func TestCompleteHabit_AlreadyCompleted_NoInsert(t *testing.T) {
	utcMidnight := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 AND date = \$2`).
		WithArgs(dbnotifHabitID, utcMidnight, 1).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id", "habit_id"}).AddRow(dbnotifRowID, dbnotifHabitID))

	completion, err := CompleteHabit(dbnotifCtx(testUserID), dbnotifHabitID, utcMidnight, "")
	assert.ErrorIs(t, err, types.ErrAlreadyCompleted)
	assert.Nil(t, completion)
}

func TestCompleteHabit_InsertError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habit_completions"`).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id"}))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "habit_completions"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	_, err := CompleteHabit(dbnotifCtx(testUserID), dbnotifHabitID, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "")
	assert.ErrorContains(t, err, "insert failed")
}

func TestUncompleteHabit_IdempotentDelete(t *testing.T) {
	input := time.Date(2026, 7, 20, 23, 59, 0, 0, time.UTC)
	utcMidnight := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	t.Run("deletes by normalized date", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "habit_completions" WHERE habit_id = \$1 AND user_id = \$2 AND date = \$3`).
			WithArgs(dbnotifHabitID, testUserID, utcMidnight).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		require.NoError(t, UncompleteHabit(dbnotifCtx(testUserID), dbnotifHabitID, input))
	})

	t.Run("zero rows is still success", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "habit_completions" WHERE habit_id = \$1 AND user_id = \$2 AND date = \$3`).
			WithArgs(dbnotifHabitID, testUserID, utcMidnight).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		// Idempotent contract: un-completing an un-completed day is a no-op.
		require.NoError(t, UncompleteHabit(dbnotifCtx(testUserID), dbnotifHabitID, input))
	})

	t.Run("db error propagates", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "habit_completions"`).
			WillReturnError(errors.New("delete failed"))
		mock.ExpectRollback()

		assert.ErrorContains(t, UncompleteHabit(dbnotifCtx(testUserID), dbnotifHabitID, input), "delete failed")
	})
}

func TestGetCompletions_LimitAndOrder(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habit_completions" WHERE habit_id = \$1 AND user_id = \$2 ORDER BY date DESC LIMIT \$3`).
		WithArgs(dbnotifHabitID, testUserID, 5).
		WillReturnRows(sqlmock.NewRows([]string{"completion_id", "habit_id"}).
			AddRow("c-2", dbnotifHabitID).
			AddRow("c-1", dbnotifHabitID))

	completions, err := GetCompletions(dbnotifCtx(testUserID), dbnotifHabitID, 5)
	require.NoError(t, err)
	require.Len(t, completions, 2)
	assert.Equal(t, "c-2", completions[0].CompletionID)
}

func TestGetCompletions_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habit_completions"`).
		WillReturnError(errors.New("select failed"))

	_, err := GetCompletions(dbnotifCtx(testUserID), dbnotifHabitID, 5)
	assert.ErrorContains(t, err, "select failed")
}
