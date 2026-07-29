package database

// Tests for agent.go background-job persistence. The interesting surface is
// UpdateBackgroundJob's partial-update map: zero-valued fields must be
// omitted so a progress ping can't wipe an error message (and vice versa),
// and Result goes through a manual json.Marshal + ::jsonb cast.

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

func TestCreateBackgroundJob_HappyPath(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "background_jobs"`).
		WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow(dbnotifJobID))
	mock.ExpectCommit()

	job := &types.BackgroundJob{UserID: testUserID, Type: string(types.JobTypeRoadmap)}
	require.NoError(t, CreateBackgroundJob(dbnotifCtx(testUserID), job))
	assert.Equal(t, dbnotifJobID, job.JobID, "generated job_id should be scanned back")
}

func TestCreateBackgroundJob_InsertError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "background_jobs"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := CreateBackgroundJob(dbnotifCtx(testUserID), &types.BackgroundJob{UserID: testUserID, Type: string(types.JobTypeRoadmap)})
	assert.ErrorContains(t, err, "insert failed")
}

func TestGetBackgroundJob_ScopedToUser(t *testing.T) {
	mock := withMockDB(t)
	// The WHERE must pin BOTH job_id and user_id — this is the tenancy check
	// that stops one user polling another user's job.
	mock.ExpectQuery(`SELECT \* FROM "background_jobs" WHERE job_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifJobID, testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"job_id", "user_id", "type", "status"}).
			AddRow(dbnotifJobID, testUserID, string(types.JobTypeRoadmap), string(types.JobStatusProcessing)))

	job, err := GetBackgroundJob(dbnotifCtx(testUserID), dbnotifJobID, testUserID)
	require.NoError(t, err)
	assert.Equal(t, dbnotifJobID, job.JobID)
	assert.Equal(t, string(types.JobStatusProcessing), job.Status)
}

func TestGetBackgroundJob_NotFound_ErrorPassthrough(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "background_jobs" WHERE job_id = \$1 AND user_id = \$2`).
		WithArgs(dbnotifJobID, testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"job_id"}))

	_, err := GetBackgroundJob(dbnotifCtx(testUserID), dbnotifJobID, testUserID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "not-found is passed to the caller undecorated")
}

func TestUpdateBackgroundJob_StatusOnly_OmitsZeroFields(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// Map keys are sorted by GORM, so the SET list is deterministic. Only
	// status + the injected updated_at may appear: progress 0, error "",
	// nil timestamps and nil Result are all omitted.
	mock.ExpectExec(`UPDATE "background_jobs" SET "status"=\$1,"updated_at"=\$2 WHERE job_id = \$3`).
		WithArgs(string(types.JobStatusProcessing), dbnotifNonZeroTime{}, dbnotifJobID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := UpdateBackgroundJob(context.Background(), &types.BackgroundJob{
		JobID:  dbnotifJobID,
		Status: string(types.JobStatusProcessing),
	})
	require.NoError(t, err)
}

func TestUpdateBackgroundJob_AllFieldsSet_AllPresent(t *testing.T) {
	started := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 7, 26, 10, 5, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectBegin()
	// Sorted keys: completed_at, error, progress, started_at, status, updated_at.
	mock.ExpectExec(`UPDATE "background_jobs" SET "completed_at"=\$1,"error"=\$2,"progress"=\$3,"started_at"=\$4,"status"=\$5,"updated_at"=\$6 WHERE job_id = \$7`).
		WithArgs(&completed, "model exploded", 100, &started, string(types.JobStatusFailed), dbnotifNonZeroTime{}, dbnotifJobID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := UpdateBackgroundJob(context.Background(), &types.BackgroundJob{
		JobID:       dbnotifJobID,
		Status:      string(types.JobStatusFailed),
		Progress:    100,
		Error:       "model exploded",
		StartedAt:   &started,
		CompletedAt: &completed,
	})
	require.NoError(t, err)
}

func TestUpdateBackgroundJob_ResultSerializedThroughJSONBCast(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// The result assignment is a gorm.Expr("?::jsonb", ...) so the SQL carries
	// the cast and the arg is the pre-marshaled JSON string.
	mock.ExpectExec(`UPDATE "background_jobs" SET "result"=\$1::jsonb,"status"=\$2,"updated_at"=\$3 WHERE job_id = \$4`).
		WithArgs(`{"roadmap_id":"r-1"}`, string(types.JobStatusCompleted), dbnotifNonZeroTime{}, dbnotifJobID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := UpdateBackgroundJob(context.Background(), &types.BackgroundJob{
		JobID:  dbnotifJobID,
		Status: string(types.JobStatusCompleted),
		Result: map[string]any{"roadmap_id": "r-1"},
	})
	require.NoError(t, err)
}

func TestUpdateBackgroundJob_ResultMarshalFailure_NoSQL(t *testing.T) {
	withMockDB(t) // zero expectations: marshal fails before any SQL

	err := UpdateBackgroundJob(context.Background(), &types.BackgroundJob{
		JobID:  dbnotifJobID,
		Status: string(types.JobStatusCompleted),
		Result: make(chan int), // json.Marshal unsupported type
	})
	assert.ErrorContains(t, err, "failed to marshal result")
}

func TestUpdateBackgroundJob_UpdateError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "background_jobs"`).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	err := UpdateBackgroundJob(context.Background(), &types.BackgroundJob{JobID: dbnotifJobID, Status: string(types.JobStatusFailed)})
	assert.ErrorContains(t, err, "update failed")
}
