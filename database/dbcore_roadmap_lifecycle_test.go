package database

// Tests for roadmap lifecycle operations (roadmap_lifecycle.go): pause,
// resume, archive, restore, restart, shift, and realign. Focus is on the
// guard chain (ownership, started, paused state) and on the date math that
// the mobile app depends on when a user falls behind schedule.
//
// "Trim the tail" pattern: RestartRoadmap/ShiftRoadmapDates/RealignRoadmapDates
// run RecomputeAndSaveStatuses (failure logged, not returned) and then
// GetRoadmapFullByID (failure returned). Tests error both SELECTs with
// errDBCore so the flow ends cheaply while ExpectationsWereMet still proves
// all the writes happened.

import (
	"strconv"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dbcoreLifecycleRoadmapCols is the column subset the lifecycle tests scan.
var dbcoreLifecycleRoadmapCols = []string{
	"roadmap_id", "user_id", "goal", "completed", "start_date", "archived", "paused_at",
}

// dbcoreExpectRoadmapOwnedFetch stubs the user-scoped roadmap First() every
// lifecycle function starts with, returning a single row.
func dbcoreExpectRoadmapOwnedFetch(mock sqlmock.Sqlmock, startDate, pausedAt any, archived bool) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreLifecycleRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "Learn Go", false, startDate, archived, pausedAt))
}

// dbcoreExpectRoadmapOwnedNotFound stubs the same fetch with zero rows.
func dbcoreExpectRoadmapOwnedNotFound(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreLifecycleRoadmapCols))
}

// dbcoreExpectRecomputeSwallowed errors the roadmap reload that opens
// RecomputeAndSaveStatuses; callers in this file log-and-ignore that error.
func dbcoreExpectRecomputeSwallowed(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnError(errDBCore)
}

// dbcoreExpectFullReloadFails errors the GetRoadmapFullByID reload; its error
// IS surfaced by Restart/Shift/Realign, so tests assert errDBCore.
func dbcoreExpectFullReloadFails(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnError(errDBCore)
}

// --- PauseRoadmap ---

func TestPauseRoadmap_MissingUserID(t *testing.T) {
	withMockDB(t) // no queries expected

	err := PauseRoadmap(dbcoreCtx(""), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestPauseRoadmap_NotFound(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedNotFound(mock)

	err := PauseRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestPauseRoadmap_NotStarted(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, false)

	err := PauseRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap has not been started", err.Error())
}

func TestPauseRoadmap_AlreadyPaused(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	paused := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, paused, false)

	err := PauseRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap is already paused", err.Error())
}

func TestPauseRoadmap_HappyPath_SetsPausedAt(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "paused_at"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(sqlmock.AnyArg(), dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, PauseRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID))
}

// --- ArchiveRoadmap ---

func TestArchiveRoadmap_RowsAffectedZero_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "archived"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WithArgs(true, dbcoreUserID, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := ArchiveRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestArchiveRoadmap_HappyPath(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "archived"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WithArgs(true, dbcoreUserID, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, ArchiveRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID))
}

func TestArchiveRoadmap_DBErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "archived"=\$1`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	err := ArchiveRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.ErrorIs(t, err, errDBCore)
}

// --- ResumeRoadmap ---

func TestResumeRoadmap_NotPaused(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)

	_, err := ResumeRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap is not paused", err.Error())
}

// dbcoreExpectShiftWrites stubs the transactional writes ShiftRoadmapDates
// issues: start_date update plus the two raw INTERVAL execs, asserting the
// shift amount reaches both raw statements.
func dbcoreExpectShiftWrites(mock sqlmock.Sqlmock, newStart time.Time, days int) {
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "start_date"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(newStart, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE milestones SET date = date \+ INTERVAL '1 day' \* \$1 WHERE roadmap_id = \$2 AND date IS NOT NULL`).
		WithArgs(days, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE tasks SET date = date \+ INTERVAL '1 day' \* \$1 WHERE roadmap_id = \$2 AND date IS NOT NULL`).
		WithArgs(days, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()
}

func TestResumeRoadmap_PausedUnderADay_ShiftsByOneDayMinimum(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pausedAt := time.Now().Add(-1 * time.Hour) // < 24h paused -> clamped to 1 day
	dbcoreExpectRoadmapOwnedFetch(mock, start, pausedAt, false)

	// Clear paused_at.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "paused_at"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(nil, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Delegates to ShiftRoadmapDates(days=1): it re-fetches the roadmap.
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)
	dbcoreExpectShiftWrites(mock, start.AddDate(0, 0, 1), 1)

	dbcoreExpectRecomputeSwallowed(mock)
	dbcoreExpectFullReloadFails(mock)

	_, err := ResumeRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.ErrorIs(t, err, errDBCore, "only the trimmed reload fails; all shift writes matched")
}

func TestResumeRoadmap_PausedThreeDays_ShiftsByThree(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pausedAt := time.Now().Add(-73 * time.Hour) // ~3 days -> int(73/24) = 3
	dbcoreExpectRoadmapOwnedFetch(mock, start, pausedAt, false)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "paused_at"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(nil, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)
	dbcoreExpectShiftWrites(mock, start.AddDate(0, 0, 3), 3)

	dbcoreExpectRecomputeSwallowed(mock)
	dbcoreExpectFullReloadFails(mock)

	_, err := ResumeRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.ErrorIs(t, err, errDBCore)
}

// --- ShiftRoadmapDates ---

func TestShiftRoadmapDates_NotFound(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedNotFound(mock)

	_, err := ShiftRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 2)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestShiftRoadmapDates_NotStarted(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, false)

	_, err := ShiftRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 2)
	require.Error(t, err)
	assert.Equal(t, "roadmap has not been started", err.Error())
}

func TestShiftRoadmapDates_HappyPath_ShiftsStartAndIntervalUpdates(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)
	dbcoreExpectShiftWrites(mock, start.AddDate(0, 0, 3), 3)
	dbcoreExpectRecomputeSwallowed(mock)
	dbcoreExpectFullReloadFails(mock)

	_, err := ShiftRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 3)
	require.ErrorIs(t, err, errDBCore)
}

func TestShiftRoadmapDates_CommitErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "start_date"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(start.AddDate(0, 0, 2), dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE milestones SET date = date \+ INTERVAL '1 day' \* \$1`).
		WithArgs(2, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE tasks SET date = date \+ INTERVAL '1 day' \* \$1`).
		WithArgs(2, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errDBCore)

	_, err := ShiftRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 2)
	require.ErrorIs(t, err, errDBCore)
}

// --- RestartRoadmap ---

func TestRestartRoadmap_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := RestartRoadmap(dbcoreCtx(""), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestRestartRoadmap_NotFound(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedNotFound(mock)

	_, err := RestartRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestRestartRoadmap_NotStarted(t *testing.T) {
	mock := withMockDB(t)
	// The restart fetch preloads Milestones.Tasks.Steps and
	// Phases.Milestones.Tasks.Steps; return an unstarted row with no children.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreLifecycleRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, nil, false, nil))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))

	_, err := RestartRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap has not been started", err.Error())
}

func TestRestartRoadmap_ResetsStateAndRecalculatesDates(t *testing.T) {
	mock := withMockDB(t)
	oldStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	today := dbcoreScheduleAnchor()

	// Initial preloaded fetch: one milestone (Day=5) with one task (Day=2).
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreLifecycleRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "g", false, oldStart, false, nil))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "day"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, 5))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "milestone_id", "day"}).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, 2))
	mock.ExpectQuery(`SELECT \* FROM "steps" WHERE "steps"\."task_id" = \$1`).
		WithArgs(dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
	dbcoreExpectUserTimezone(mock)

	mock.ExpectBegin()
	// Roadmap reset (map keys sorted: completed, paused_at, start_date).
	mock.ExpectExec(`UPDATE "roadmaps" SET "completed"=\$1,"paused_at"=\$2,"start_date"=\$3 WHERE roadmap_id = \$4`).
		WithArgs(false, nil, today, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Milestone/task completion + status resets.
	mock.ExpectExec(`UPDATE "milestones" SET "reached"=\$1,"status"=\$2 WHERE roadmap_id = \$3`).
		WithArgs(false, "not_started", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "tasks" SET "completed"=\$1,"status"=\$2 WHERE roadmap_id = \$3`).
		WithArgs(false, "not_started", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Step reset via plucked task IDs.
	mock.ExpectQuery(`SELECT "task_id" FROM "tasks" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow(dbcoreTaskID))
	mock.ExpectExec(`UPDATE "steps" SET "completed"=\$1,"status"=\$2 WHERE task_id IN \(\$3\)`).
		WithArgs(false, "incomplete", dbcoreTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Date recalculation is two set-based UPDATEs that rebase every row from its
	// own `day` column, so there is no per-row SELECT/UPDATE pair to expect.
	//
	// What these assertions still prove is the inputs: the start day, and
	// dayZero=2 — the plan's earliest day, taken from the task's Day=2 rather
	// than the milestone's Day=5, which is the rebasing this test has always
	// been about. The arithmetic itself (Day=5 -> start+3, Day=2 -> start) is now
	// Postgres's, and sqlmock does not execute SQL, so it is covered against a
	// real server by TestStartRoadmapSchedule_MatchesGoScheduleOffset in
	// test/integration/roadmap_schedule_test.go instead.
	startDay := today.Format("2006-01-02")
	mock.ExpectExec(`UPDATE milestones SET date = \(\$1::date \+ GREATEST\("day" - \$2, 0\) \* INTERVAL '1 day' \+ INTERVAL '12 hours'\) AT TIME ZONE \$3 WHERE roadmap_id = \$4`).
		WithArgs(startDay, 2, "UTC", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE tasks SET date = \(\$1::date \+ GREATEST\("day" - \$2, 0\) \* INTERVAL '1 day' \+ INTERVAL '12 hours'\) AT TIME ZONE \$3 WHERE roadmap_id = \$4`).
		WithArgs(startDay, 2, "UTC", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "phases" SET "status"=\$1 WHERE roadmap_id = \$2`).
		WithArgs("not_started", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Post-commit recompute failure is only logged; the reload failure is
	// what surfaces.
	dbcoreExpectRecomputeSwallowed(mock)
	dbcoreExpectFullReloadFails(mock)

	_, err := RestartRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.ErrorIs(t, err, errDBCore,
		"reset writes all matched; only the trimmed final reload fails")
}

// --- RestoreRoadmap ---

func TestRestoreRoadmap_NotFound(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedNotFound(mock)

	_, err := RestoreRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, false)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestRestoreRoadmap_NotArchived(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, false)

	_, err := RestoreRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, false)
	require.Error(t, err)
	assert.Equal(t, "roadmap is not archived", err.Error())
}

func TestRestoreRoadmap_HappyUnarchive(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, true)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "archived"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(false, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// StartDate nil -> no restart; straight to GetRoadmapFullByID.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows(dbcoreLifecycleRoadmapCols).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "Learn Go", false, nil, false, nil))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
	mock.ExpectQuery(`SELECT "timezone" FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID).
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("UTC"))

	roadmap, err := RestoreRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, false)
	require.NoError(t, err)
	assert.Equal(t, dbcoreRoadmapID, roadmap.RoadmapID)
	assert.Equal(t, "Learn Go", roadmap.Goal)
}

func TestRestoreRoadmap_StartFromToday_DelegatesToRestart(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, true)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "archived"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(false, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// startFromToday + StartDate set -> RestartRoadmap fires its preloaded
	// fetch. Error it to prove delegation without mocking the whole restart.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID, 1).
		WillReturnError(errDBCore)

	_, err := RestoreRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, true)
	require.ErrorIs(t, err, errDBCore)
}

// --- RealignRoadmapDates ---

func TestRealignRoadmapDates_NotFound(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedNotFound(mock)

	_, _, err := RealignRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 0)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

// dbcoreRealignShiftSQL builds the regex for RealignRoadmapDates' shift, whose
// SET clause is identical for tasks and milestones and differs only in the
// WHERE. Placeholders: $1/$3 timezone, $2 days, $4 the row selector.
func dbcoreRealignShiftSQL(table, where string) string {
	return `UPDATE ` + table +
		` SET date = \(date_trunc\('day', \(date AT TIME ZONE \$1\) \+ \(\$2 \* INTERVAL '1 day'\)\) \+ INTERVAL '` +
		strconv.Itoa(types.ScheduleAnchorHour) +
		` hours'\) AT TIME ZONE \$3 WHERE ` + where + ` AND date IS NOT NULL`
}

func TestRealignRoadmapDates_NotStarted(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, false)

	_, _, err := RealignRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 0)
	require.Error(t, err)
	assert.Equal(t, "roadmap has not been started", err.Error())
}

func TestRealignRoadmapDates_NoIncompleteTasks(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE \(roadmap_id = \$1 AND completed = \$2\) AND date IS NOT NULL ORDER BY date ASC`).
		WithArgs(dbcoreRoadmapID, false, 1).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	_, _, err := RealignRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 0)
	require.Error(t, err)
	assert.Equal(t, "no incomplete tasks found", err.Error())
}

func TestRealignRoadmapDates_ShiftsIncompleteWork(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	today := dbcoreScheduleAnchor()
	// First incomplete task is 3 days behind; offsetDays=2 -> shift 5.
	firstTaskDate := today.AddDate(0, 0, -3)

	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE \(roadmap_id = \$1 AND completed = \$2\) AND date IS NOT NULL ORDER BY date ASC`).
		WithArgs(dbcoreRoadmapID, false, 1).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "milestone_id", "completed", "date"}).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, dbcoreMilestoneID, false, firstTaskDate))
	dbcoreExpectUserTimezone(mock)

	// Dates shift in the owner's local wall time, then re-anchor to the
	// scheduling hour: "UTC" is the mock user's fallback timezone.
	mock.ExpectBegin()
	mock.ExpectExec(dbcoreRealignShiftSQL(`tasks`, `roadmap_id = \$4 AND completed = false`)).
		WithArgs("UTC", 5, "UTC", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 4))
	// Milestone IDs plucked only for milestones that still have incomplete tasks.
	mock.ExpectQuery(`SELECT DISTINCT "milestone_id" FROM "tasks" WHERE roadmap_id = \$1 AND completed = false`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}).AddRow(dbcoreMilestoneID))
	mock.ExpectExec(dbcoreRealignShiftSQL(`milestones`, `milestone_id IN \(\$4\)`)).
		WithArgs("UTC", 5, "UTC", dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRecomputeSwallowed(mock)
	dbcoreExpectFullReloadFails(mock)

	_, daysShifted, err := RealignRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 2)
	assert.Equal(t, 5, daysShifted)
	require.ErrorIs(t, err, errDBCore)
}

func TestRealignRoadmapDates_AlreadyAligned_StillReanchors(t *testing.T) {
	mock := withMockDB(t)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// Already on today's calendar day, but at an arbitrary wall-clock time —
	// the shape of rows written before dates were anchored.
	firstTaskDate := dbcoreScheduleAnchor().Add(-5 * time.Hour)

	dbcoreExpectRoadmapOwnedFetch(mock, start, nil, false)
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE \(roadmap_id = \$1 AND completed = \$2\) AND date IS NOT NULL ORDER BY date ASC`).
		WithArgs(dbcoreRoadmapID, false, 1).
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "roadmap_id", "completed", "date"}).
			AddRow(dbcoreTaskID, dbcoreRoadmapID, false, firstTaskDate))
	dbcoreExpectUserTimezone(mock)

	// daysToShift == 0, but the update still runs so the stray time-of-day is
	// normalized to the scheduling anchor.
	mock.ExpectBegin()
	mock.ExpectExec(dbcoreRealignShiftSQL(`tasks`, `roadmap_id = \$4 AND completed = false`)).
		WithArgs("UTC", 0, "UTC", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT DISTINCT "milestone_id" FROM "tasks" WHERE roadmap_id = \$1 AND completed = false`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectCommit()

	dbcoreExpectRecomputeSwallowed(mock)
	dbcoreExpectFullReloadFails(mock)

	_, daysShifted, err := RealignRoadmapDates(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, 0)
	assert.Equal(t, 0, daysShifted)
	require.ErrorIs(t, err, errDBCore)
}
