package handlers

// White-box tests for the async generation plumbing in agent.go:
// getProgressMessage bands, updateJobProgress, handleGenerationError,
// handleNextMilestoneError, and the ERROR paths of processRoadmapGeneration /
// processNextMilestoneGeneration driven synchronously with a fake provider.
// The success-persistence paths (UpdateRoadmapWithGeneration / SaveNextMilestone
// committing multi-table transactions) are deliberately out of scope.
//
// notifications.Dispatch runs at the tail of the error handlers; its settings
// lookup hits the sqlmock as an unexpected query and fails safe inside
// Dispatch, which doesn't affect ExpectationsWereMet. The global DB is
// deliberately NOT restored on cleanup (same mitigation as
// roadmap_anchor_test.go): stray fire-and-forget goroutines from other tests
// in this package must never observe a nil DB.
//
// All package-level identifiers carry the hfin prefix (hfinAg for this file)
// to avoid collisions with concurrently-added test files.

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"dr/server/database"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	hfinAgJobID       = "81818181-9292-0303-1414-252525252525"
	hfinAgRoadmapID   = "18181818-2929-3030-4141-525252525252"
	hfinAgMilestoneID = "28282828-3939-4040-5151-626262626626"
	hfinAgUserID      = "38383838-4949-5050-6161-727272727272"
)

// GORM sorts map keys in Updates(), so the SET column order is deterministic.
const (
	hfinAgJobStartSQL    = `UPDATE "background_jobs" SET "progress"=\$1,"started_at"=\$2,"status"=\$3,"updated_at"=\$4 WHERE job_id = \$5`
	hfinAgJobProgressSQL = `UPDATE "background_jobs" SET "progress"=\$1,"status"=\$2,"updated_at"=\$3 WHERE job_id = \$4`
	hfinAgJobFailSQL     = `UPDATE "background_jobs" SET "completed_at"=\$1,"error"=\$2,"status"=\$3,"updated_at"=\$4 WHERE job_id = \$5`
	hfinAgRoadmapFailSQL = `UPDATE "roadmaps" SET "generation_status"=\$1 WHERE roadmap_id = \$2`
)

// hfinAgDB wires a fresh regex-matching sqlmock into the package-global DB.
// Inlined rather than using internal/testutil, which would create an import
// cycle from a package-handlers (white-box) test. Never restored; see the
// file comment.
func hfinAgDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = sqlDB.Close()
	})

	database.SetDBForTest(gormDB)
	return mock
}

func hfinAgExpectExec(mock sqlmock.Sqlmock, pattern string, args ...driver.Value) {
	mock.ExpectBegin()
	e := mock.ExpectExec(pattern)
	if len(args) > 0 {
		e.WithArgs(args...)
	}
	e.WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// hfinAgFakeProvider satisfies agent.Provider without any network access.
type hfinAgFakeProvider struct {
	roadmap      *types.Roadmap
	roadmapErr   error
	tasks        []types.Task
	tasksErr     error
	adjustments  *types.MilestoneAdjustments
	nextTasks    []types.Task
	nextErr      error
	tasksCalled  bool
	reviewCalled bool
}

func (p *hfinAgFakeProvider) Name() string { return "hfin-fake" }

func (p *hfinAgFakeProvider) GenerateStrategicRoadmap(_ context.Context, _ types.GoalRequest) (*types.Roadmap, error) {
	return p.roadmap, p.roadmapErr
}

func (p *hfinAgFakeProvider) GenerateMilestoneTasks(_ context.Context, _ types.GoalRequest, _ *types.Milestone) ([]types.Task, error) {
	p.tasksCalled = true
	return p.tasks, p.tasksErr
}

func (p *hfinAgFakeProvider) GenerateNextMilestoneFromReview(_ context.Context, _ types.GoalRequest, _ *types.Milestone, _ types.MilestoneReviewAnswers) (*types.MilestoneAdjustments, []types.Task, error) {
	p.reviewCalled = true
	return p.adjustments, p.nextTasks, p.nextErr
}

// --- getProgressMessage ------------------------------------------------------

func TestGetProgressMessage_AllBands(t *testing.T) {
	cases := []struct {
		name     string
		status   string
		progress int
		want     string
	}{
		{"failed short-circuits", "failed", 90, "Generation failed"},
		{"completed short-circuits", "completed", 100, "Roadmap ready!"},
		{"below 10", "processing", 5, "Initializing..."},
		{"10-39 low edge", "processing", 10, "Generating strategic roadmap..."},
		{"10-39 high edge", "processing", 39, "Generating strategic roadmap..."},
		{"40-79", "processing", 50, "Creating detailed tasks..."},
		{"80-94 low edge", "processing", 80, "Saving roadmap..."},
		{"80-94 high edge", "processing", 94, "Saving roadmap..."},
		{"95 and up", "processing", 95, "Finalizing..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, getProgressMessage(tc.status, tc.progress))
		})
	}
}

// --- updateJobProgress -------------------------------------------------------

func TestUpdateJobProgress_InitialTick_StampsStartedAt(t *testing.T) {
	mock := hfinAgDB(t)

	// progress==5 + processing is the job's first tick -> started_at is set.
	hfinAgExpectExec(mock, hfinAgJobStartSQL,
		5, sqlmock.AnyArg(), string(types.JobStatusProcessing), sqlmock.AnyArg(), hfinAgJobID)

	updateJobProgress(hfinAgJobID, 5, string(types.JobStatusProcessing), "starting")
}

func TestUpdateJobProgress_MidTick_NoStartedAt(t *testing.T) {
	mock := hfinAgDB(t)

	hfinAgExpectExec(mock, hfinAgJobProgressSQL,
		50, string(types.JobStatusProcessing), sqlmock.AnyArg(), hfinAgJobID)

	updateJobProgress(hfinAgJobID, 50, string(types.JobStatusProcessing), "halfway")
}

// --- handleGenerationError ---------------------------------------------------

func TestHandleGenerationError_FailsJobAndRoadmap(t *testing.T) {
	mock := hfinAgDB(t)

	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "model exploded", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)
	hfinAgExpectExec(mock, hfinAgRoadmapFailSQL,
		string(types.GenerationStatusFailed), hfinAgRoadmapID)

	handleGenerationError(hfinAgJobID, hfinAgRoadmapID, hfinAgUserID, errors.New("model exploded"))
}

// --- handleNextMilestoneError ------------------------------------------------

func TestHandleNextMilestoneError_FailsJobButNotRoadmap(t *testing.T) {
	// Unlike handleGenerationError, the roadmap's generation_status must stay
	// untouched (the roadmap itself is already generated). Only the job row is
	// updated; a roadmaps UPDATE would surface as an unmatched expectation.
	mock := hfinAgDB(t)

	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "review exploded", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)

	handleNextMilestoneError(hfinAgJobID, hfinAgMilestoneID, hfinAgUserID, errors.New("review exploded"))
}

// --- processRoadmapGeneration error paths ------------------------------------

func TestProcessRoadmapGeneration_StrategicFailure_MarksFailed(t *testing.T) {
	mock := hfinAgDB(t)
	provider := &hfinAgFakeProvider{roadmapErr: errors.New("strategic gen failed")}

	hfinAgExpectExec(mock, hfinAgJobStartSQL)    // progress 5
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 10
	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "strategic gen failed", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)
	hfinAgExpectExec(mock, hfinAgRoadmapFailSQL,
		string(types.GenerationStatusFailed), hfinAgRoadmapID)

	processRoadmapGeneration(context.Background(), provider,
		hfinAgJobID, hfinAgRoadmapID, hfinAgUserID, types.GoalRequest{GoalName: "Run a marathon"})

	assert.False(t, provider.tasksCalled, "task generation must not run after strategic failure")
}

func TestProcessRoadmapGeneration_TaskGenFailure_MarksFailed(t *testing.T) {
	mock := hfinAgDB(t)
	provider := &hfinAgFakeProvider{
		roadmap: &types.Roadmap{Phases: []types.Phase{
			{Milestones: []types.Milestone{{SubGoal: "First milestone"}}},
		}},
		tasksErr: errors.New("task gen failed"),
	}

	hfinAgExpectExec(mock, hfinAgJobStartSQL)    // progress 5
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 10
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 50 (strategic done)
	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "task gen failed", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)
	hfinAgExpectExec(mock, hfinAgRoadmapFailSQL,
		string(types.GenerationStatusFailed), hfinAgRoadmapID)

	processRoadmapGeneration(context.Background(), provider,
		hfinAgJobID, hfinAgRoadmapID, hfinAgUserID, types.GoalRequest{GoalName: "Run a marathon"})

	assert.True(t, provider.tasksCalled)
}

func TestProcessRoadmapGeneration_PersistFailure_NoMilestones(t *testing.T) {
	// A strategic roadmap with zero milestones skips task generation entirely
	// (firstMilestone stays nil) and goes straight to persistence, which fails.
	mock := hfinAgDB(t)
	provider := &hfinAgFakeProvider{roadmap: &types.Roadmap{}}

	hfinAgExpectExec(mock, hfinAgJobStartSQL)    // progress 5
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 10
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 50
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 80 (saving)

	// UpdateRoadmapWithGeneration's association-save transaction fails.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET`).
		WillReturnError(errors.New("persist failed"))
	mock.ExpectRollback()

	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "persist failed", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)
	hfinAgExpectExec(mock, hfinAgRoadmapFailSQL,
		string(types.GenerationStatusFailed), hfinAgRoadmapID)

	processRoadmapGeneration(context.Background(), provider,
		hfinAgJobID, hfinAgRoadmapID, hfinAgUserID, types.GoalRequest{GoalName: "Run a marathon"})

	assert.False(t, provider.tasksCalled, "no milestone -> no task generation")
}

// --- processNextMilestoneGeneration error paths ------------------------------

// hfinAgExpectMilestoneForGeneration stubs GetMilestoneForGeneration's fetch;
// found=true returns a bare milestone whose Spikes/Risks/Tasks preloads all
// come back empty.
func hfinAgExpectMilestoneForGeneration(mock sqlmock.Sqlmock, found bool) {
	q := mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hfinAgUserID, hfinAgMilestoneID, 1)
	if !found {
		q.WillReturnError(gorm.ErrRecordNotFound)
		return
	}
	q.WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal"}).
		AddRow(hfinAgMilestoneID, hfinAgRoadmapID, hfinAgUserID, "Ship v1"))
	// Preloads run in sorted order: Risks, Spikes, Tasks.
	mock.ExpectQuery(`SELECT \* FROM "risks" WHERE "risks"\."milestone_id" = \$1`).
		WithArgs(hfinAgMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"risk_id"}))
	mock.ExpectQuery(`SELECT \* FROM "spikes" WHERE "spikes"\."milestone_id" = \$1`).
		WithArgs(hfinAgMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"spike_id"}))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(hfinAgMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))
}

func TestProcessNextMilestoneGeneration_MilestoneNotFound_FailsJob(t *testing.T) {
	mock := hfinAgDB(t)
	provider := &hfinAgFakeProvider{}

	hfinAgExpectExec(mock, hfinAgJobStartSQL) // progress 5
	hfinAgExpectMilestoneForGeneration(mock, false)
	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "milestone not found", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)

	processNextMilestoneGeneration(context.Background(), provider,
		hfinAgJobID, hfinAgMilestoneID, hfinAgUserID, types.GenerateNextMilestoneRequest{})

	assert.False(t, provider.reviewCalled, "provider must not be called without a milestone")
}

func TestProcessNextMilestoneGeneration_ProviderFailure_FailsJob(t *testing.T) {
	mock := hfinAgDB(t)
	provider := &hfinAgFakeProvider{nextErr: errors.New("review gen failed")}

	hfinAgExpectExec(mock, hfinAgJobStartSQL) // progress 5
	hfinAgExpectMilestoneForGeneration(mock, true)
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 15
	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "review gen failed", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)

	processNextMilestoneGeneration(context.Background(), provider,
		hfinAgJobID, hfinAgMilestoneID, hfinAgUserID, types.GenerateNextMilestoneRequest{})

	assert.True(t, provider.reviewCalled)
}

func TestProcessNextMilestoneGeneration_SaveFailure_FailsJob(t *testing.T) {
	mock := hfinAgDB(t)
	provider := &hfinAgFakeProvider{
		adjustments: &types.MilestoneAdjustments{Adjusted: false},
		nextTasks:   nil,
	}

	hfinAgExpectExec(mock, hfinAgJobStartSQL) // progress 5
	hfinAgExpectMilestoneForGeneration(mock, true)
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 15
	// (progress 45 is broadcast-only, no DB write)
	hfinAgExpectExec(mock, hfinAgJobProgressSQL) // progress 80 (saving)

	// SaveNextMilestone reloads the milestone row first; fail there.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(hfinAgMilestoneID, 1).
		WillReturnError(errors.New("save reload failed"))

	hfinAgExpectExec(mock, hfinAgJobFailSQL,
		sqlmock.AnyArg(), "save reload failed", string(types.JobStatusFailed), sqlmock.AnyArg(), hfinAgJobID)

	processNextMilestoneGeneration(context.Background(), provider,
		hfinAgJobID, hfinAgMilestoneID, hfinAgUserID, types.GenerateNextMilestoneRequest{})

	assert.True(t, provider.reviewCalled)
}
