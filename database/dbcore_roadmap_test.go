package database

// Tests for roadmap.go CRUD: generateUUIDs wiring, CreateRoadmap /
// CreateDraftRoadmap ensure-user branches, GetRoadmapsByUser archived
// filtering, UpdateRoadmap's pointer-compare update map and empty-roadmap
// auto-complete, milestone CRUD, and the single-column status setters.

import (
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dbcoreRequireUUID asserts id is a parseable, non-empty UUID.
func dbcoreRequireUUID(t *testing.T, id string, label string) {
	t.Helper()
	require.NotEmpty(t, id, label)
	_, err := uuid.Parse(id)
	require.NoError(t, err, "%s should be a valid UUID, got %q", label, id)
}

// --- generateUUIDs (pure, no DB) ---

func TestGenerateUUIDs_WiresAllIDsAndForeignKeys(t *testing.T) {
	presetID := "eeeeeeee-0000-0000-0000-000000000001"
	rm := types.Roadmap{
		Phases: []types.Phase{{
			Milestones: []types.Milestone{{
				Spikes: []types.Spike{{Description: "spike"}},
				Risks:  []types.Risk{{Description: "risk"}},
				Tasks: []types.Task{{
					Title: "task",
					Steps: []types.Step{{Description: "step"}},
				}},
			}},
		}},
		Milestones: []types.Milestone{
			{SubGoal: "top-level"},
			{MilestoneID: presetID, SubGoal: "pre-set id"},
		},
	}

	generateUUIDs(&rm)

	dbcoreRequireUUID(t, rm.RoadmapID, "roadmap id")

	phase := rm.Phases[0]
	dbcoreRequireUUID(t, phase.PhaseID, "phase id")
	assert.Equal(t, rm.RoadmapID, phase.RoadmapID)

	m := phase.Milestones[0]
	dbcoreRequireUUID(t, m.MilestoneID, "phase milestone id")
	assert.Equal(t, rm.RoadmapID, m.RoadmapID)
	require.NotNil(t, m.PhaseID, "milestone -> phase pointer wired")
	assert.Equal(t, phase.PhaseID, *m.PhaseID)

	dbcoreRequireUUID(t, m.Spikes[0].SpikeID, "spike id")
	assert.Equal(t, m.MilestoneID, m.Spikes[0].MilestoneID)
	dbcoreRequireUUID(t, m.Risks[0].RiskID, "risk id")
	assert.Equal(t, m.MilestoneID, m.Risks[0].MilestoneID)

	task := m.Tasks[0]
	dbcoreRequireUUID(t, task.TaskID, "task id")
	assert.Equal(t, rm.RoadmapID, task.RoadmapID)
	assert.Equal(t, m.MilestoneID, task.MilestoneID)
	dbcoreRequireUUID(t, task.Steps[0].StepID, "step id")
	assert.Equal(t, task.TaskID, task.Steps[0].TaskID)

	// Top-level milestone gets a fresh ID; a pre-set MilestoneID is kept.
	dbcoreRequireUUID(t, rm.Milestones[0].MilestoneID, "top-level milestone id")
	assert.Equal(t, rm.RoadmapID, rm.Milestones[0].RoadmapID)
	assert.Equal(t, presetID, rm.Milestones[1].MilestoneID, "pre-set milestone ID preserved")
	assert.Equal(t, rm.RoadmapID, rm.Milestones[1].RoadmapID)
}

// --- CreateRoadmap ---

// dbcoreRoadmapInsertArgCount: user_id, goal, description, days, completed,
// start_date, type, anchor_id, current_skill_level, job_id,
// generation_status, archived, paused_at, share_token, roadmap_id.
func dbcoreExpectRoadmapInsert(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WithArgs(dbcoreUserID, "Learn Go", "", 0, false, nil, "custom_made", nil, "", nil,
			"completed", false, nil, nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}).AddRow(dbcoreRoadmapID))
}

// dbcoreExpectRoadmapReloadEmpty stubs the reload after CreateRoadmap: the
// roadmap row plus empty Milestones and Phases preloads.
func dbcoreExpectRoadmapReloadEmpty(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "completed"}).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "Learn Go", false))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
}

func dbcoreNewRoadmapFixture() types.Roadmap {
	// Type and GenerationStatus set explicitly: gorm omits zero-valued
	// fields that carry column defaults, which would change the INSERT shape.
	return types.Roadmap{Goal: "Learn Go", Type: "custom_made", GenerationStatus: "completed"}
}

func TestCreateRoadmap_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := CreateRoadmap(dbcoreCtx(""), dbcoreNewRoadmapFixture())
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCreateRoadmap_ExistingUser_NoUserInsert(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(dbcoreUserID))

	// No INSERT INTO "users" expected — straight to the roadmap insert with
	// the ctx user stamped as user_id (first insert arg).
	mock.ExpectBegin()
	dbcoreExpectRoadmapInsert(mock)
	mock.ExpectCommit()

	dbcoreExpectRoadmapReloadEmpty(mock)

	got, err := CreateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreNewRoadmapFixture())
	require.NoError(t, err)
	assert.Equal(t, "Learn Go", got.Goal)
}

func TestCreateRoadmap_UnknownUser_CreatedFirst(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"})) // not found

	// User row created before the roadmap.
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "users"`).
		WithArgs(dbcoreUserID, "", "", "", "", sqlmock.AnyArg(), "", "UTC", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	dbcoreExpectRoadmapInsert(mock)
	mock.ExpectCommit()

	dbcoreExpectRoadmapReloadEmpty(mock)

	_, err := CreateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreNewRoadmapFixture())
	require.NoError(t, err)
}

func TestCreateRoadmap_UserCheckErrorWrapped(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnError(errDBCore)

	_, err := CreateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreNewRoadmapFixture())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check user existence")
}

func TestCreateRoadmap_UserCreateErrorWrapped(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "users"`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	_, err := CreateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreNewRoadmapFixture())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create user")
}

func TestCreateRoadmap_InsertErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(dbcoreUserID))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	_, err := CreateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreNewRoadmapFixture())
	require.ErrorIs(t, err, errDBCore)
}

// --- CreateDraftRoadmap ---

func TestCreateDraftRoadmap_MissingUserID(t *testing.T) {
	withMockDB(t)

	draft := dbcoreNewRoadmapFixture()
	err := CreateDraftRoadmap(dbcoreCtx(""), &draft)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCreateDraftRoadmap_OverwritesUserIDWithCtxUser(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(dbcoreUserID))

	mock.ExpectBegin()
	// The draft claimed another user; the ctx user must win (1st arg).
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WithArgs(dbcoreUserID, "Learn Go", "", 0, false, nil, "custom_made", nil, "", nil,
			"completed", false, nil, nil, dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}).AddRow(dbcoreRoadmapID))
	mock.ExpectCommit()

	draft := dbcoreNewRoadmapFixture()
	draft.RoadmapID = dbcoreRoadmapID
	draft.UserID = "33333333-3333-3333-3333-333333333333" // spoofed
	require.NoError(t, CreateDraftRoadmap(dbcoreCtx(dbcoreUserID), &draft))
	assert.Equal(t, dbcoreUserID, draft.UserID)
}

func TestCreateDraftRoadmap_UnknownUser_CreatedFirst(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "users"`).
		WithArgs(dbcoreUserID, "", "", "", "", sqlmock.AnyArg(), "", "UTC", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}).AddRow(dbcoreRoadmapID))
	mock.ExpectCommit()

	draft := dbcoreNewRoadmapFixture()
	draft.RoadmapID = dbcoreRoadmapID
	require.NoError(t, CreateDraftRoadmap(dbcoreCtx(dbcoreUserID), &draft))
}

func TestCreateDraftRoadmap_CreateErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(dbcoreUserID))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	draft := dbcoreNewRoadmapFixture()
	draft.RoadmapID = dbcoreRoadmapID
	require.ErrorIs(t, CreateDraftRoadmap(dbcoreCtx(dbcoreUserID), &draft), errDBCore)
}

// --- GetRoadmapsByUser ---

func TestGetRoadmapsByUser_MissingUserID_NoQuery(t *testing.T) {
	withMockDB(t)

	assert.Empty(t, GetRoadmapsByUser(dbcoreCtx("")))
}

func TestGetRoadmapsByUser_DefaultExcludesArchived(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(dbcoreUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	assert.Empty(t, GetRoadmapsByUser(dbcoreCtx(dbcoreUserID)))
}

func TestGetRoadmapsByUser_IncludeArchivedDropsPredicate(t *testing.T) {
	mock := withMockDB(t)
	// ?include_archived=true -> only the user_id predicate remains.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1`).
		WithArgs(dbcoreUserID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	assert.Empty(t, GetRoadmapsByUser(dbcoreCtxWithQuery(dbcoreUserID, "include_archived=true")))
}

// --- UpdateRoadmap ---

// dbcoreExpectUpdateRoadmapFetch stubs UpdateRoadmap's initial user-scoped
// fetch (roadmap row + empty Milestones/Phases preloads).
func dbcoreExpectUpdateRoadmapFetch(mock sqlmock.Sqlmock, goal string, completed bool, startDate any) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "completed", "start_date"}).
			AddRow(dbcoreRoadmapID, dbcoreUserID, goal, completed, startDate))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
}

// dbcoreExpectUpdateRoadmapReload stubs the trailing reload; the dest struct
// carries its pk, so gorm appends a pk condition.
func dbcoreExpectUpdateRoadmapReload(mock sqlmock.Sqlmock, completed bool) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1 AND "roadmaps"\."roadmap_id" = \$2`).
		WithArgs(dbcoreRoadmapID, dbcoreRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "completed"}).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "Learn Go", completed))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))
}

// dbcoreExpectRoadmapNotComplete makes shouldCompleteRoadmap report an
// incomplete milestone so the auto-complete branch stays quiet.
func dbcoreExpectRoadmapNotComplete(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "milestones" WHERE roadmap_id = \$1 AND reached = \$2`).
		WithArgs(dbcoreRoadmapID, false).
		WillReturnRows(dbcoreCountRows(1))
}

func TestUpdateRoadmap_StartDateGainedOverNil(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateRoadmapFetch(mock, "Learn Go", false, nil)

	newStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "start_date"=\$1 WHERE "roadmap_id" = \$2`).
		WithArgs(newStart, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRoadmapNotComplete(mock)
	dbcoreExpectUpdateRoadmapReload(mock, false)

	UpdateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, types.Roadmap{StartDate: &newStart})
}

func TestUpdateRoadmap_EqualStartDate_NoUpdate(t *testing.T) {
	mock := withMockDB(t)
	sameStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	dbcoreExpectUpdateRoadmapFetch(mock, "Learn Go", false, sameStart)

	// Pointer values compared, not addresses: identical times -> no UPDATE.
	dbcoreExpectRoadmapNotComplete(mock)
	dbcoreExpectUpdateRoadmapReload(mock, false)

	UpdateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, types.Roadmap{StartDate: &sameStart})
}

func TestUpdateRoadmap_GoalOnlyChange(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateRoadmapFetch(mock, "Old goal", false, nil)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "goal"=\$1 WHERE "roadmap_id" = \$2`).
		WithArgs("New goal", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectRoadmapNotComplete(mock)
	dbcoreExpectUpdateRoadmapReload(mock, false)

	UpdateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, types.Roadmap{Goal: "New goal"})
}

func TestUpdateRoadmap_EmptyRoadmapAutoCompletes(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectUpdateRoadmapFetch(mock, "Learn Go", false, nil)

	// Documents current behavior: shouldCompleteRoadmap has no total-count
	// guard, so a roadmap with zero milestones auto-completes on any update.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "milestones" WHERE roadmap_id = \$1 AND reached = \$2`).
		WithArgs(dbcoreRoadmapID, false).
		WillReturnRows(dbcoreCountRows(0))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "completed"=\$1 WHERE "roadmap_id" = \$2`).
		WithArgs(true, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	dbcoreExpectUpdateRoadmapReload(mock, true)

	got := UpdateRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, types.Roadmap{})
	assert.True(t, got.Completed)
}

// --- DeleteRoadmap / DeleteMilestone ---

func TestDeleteRoadmap_MissingUserID(t *testing.T) {
	withMockDB(t)

	err := DeleteRoadmap(dbcoreCtx(""), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestDeleteRoadmap_UserScopedDelete(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "roadmaps" WHERE user_id = \$1 AND roadmap_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID))
}

func TestDeleteRoadmap_DBErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "roadmaps"`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	require.ErrorIs(t, DeleteRoadmap(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID), errDBCore)
}

func TestDeleteMilestone_MissingUserID(t *testing.T) {
	withMockDB(t)

	err := DeleteMilestone(dbcoreCtx(""), dbcoreMilestoneID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestDeleteMilestone_UserScopedDelete(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteMilestone(dbcoreCtx(dbcoreUserID), dbcoreMilestoneID))
}

func TestDeleteMilestone_DBErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "milestones"`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	require.ErrorIs(t, DeleteMilestone(dbcoreCtx(dbcoreUserID), dbcoreMilestoneID), errDBCore)
}

// --- CreateMilestone ---

func TestCreateMilestone_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := CreateMilestone(dbcoreCtx(""), dbcoreRoadmapID, types.Milestone{})
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCreateMilestone_WiresNestedTaskAndStepFKs(t *testing.T) {
	mock := withMockDB(t)

	mock.ExpectBegin()
	// Milestone insert: roadmap_id from the path param, user_id from ctx.
	mock.ExpectQuery(`INSERT INTO "milestones"`).
		WithArgs(dbcoreRoadmapID, dbcoreUserID, "Base miles", "", 5, false, nil, "not_started",
			nil, "", 0, 0, 0, nil, dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}).AddRow(dbcoreMilestoneID))
	// Nested task inherits roadmap/milestone/user IDs.
	mock.ExpectQuery(`INSERT INTO "tasks"`).
		WithArgs(dbcoreRoadmapID, dbcoreMilestoneID, dbcoreUserID, "Long run", "", "not_started",
			"", 0, nil, "", 1, false, nil, dbcoreTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow(dbcoreTaskID))
	// Nested step wired to its task.
	mock.ExpectQuery(`INSERT INTO "steps"`).
		WithArgs(dbcoreTaskID, "stretch", false, "incomplete", 1, dbcoreStepID).
		WillReturnRows(sqlmock.NewRows([]string{"step_id"}).AddRow(dbcoreStepID))
	mock.ExpectCommit()

	// Re-fetch with Tasks.Steps preloaded.
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "sub_goal"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, "Base miles"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))

	got, err := CreateMilestone(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, types.Milestone{
		MilestoneID: dbcoreMilestoneID,
		SubGoal:     "Base miles",
		Day:         5,
		Status:      "not_started",
		Tasks: []types.Task{{
			TaskID: dbcoreTaskID,
			Title:  "Long run",
			Status: "not_started",
			Day:    1,
			Steps:  []types.Step{{StepID: dbcoreStepID, Description: "stretch", Status: "incomplete", Number: 1}},
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Base miles", got.SubGoal)
}

func TestCreateMilestone_CreateErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "milestones"`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	_, err := CreateMilestone(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID, types.Milestone{
		MilestoneID: dbcoreMilestoneID, SubGoal: "sg", Status: "not_started",
	})
	require.ErrorIs(t, err, errDBCore)
}

// --- UpdateMilestone ---

// dbcoreExpectMilestoneFetch stubs UpdateMilestone's user-scoped fetch
// (milestone row + empty Tasks preload).
func dbcoreExpectMilestoneFetch(mock sqlmock.Sqlmock, reached bool) {
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal", "day", "reached", "date"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, dbcoreUserID, "Old", 1, reached, nil))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))
}

// dbcoreExpectMilestoneReload stubs the trailing reload (pk condition added
// by gorm since the dest struct is populated).
func dbcoreExpectMilestoneReload(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE milestone_id = \$1 AND "milestones"\."milestone_id" = \$2`).
		WithArgs(dbcoreMilestoneID, dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "sub_goal"}).
			AddRow(dbcoreMilestoneID, dbcoreRoadmapID, "New"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))
}

func TestUpdateMilestone_SubGoalDayDate_UpdateMapAndNoAutoReach(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectMilestoneFetch(mock, false)

	newDate := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	// Map keys sorted: date, day, sub_goal.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "date"=\$1,"day"=\$2,"sub_goal"=\$3 WHERE "milestone_id" = \$4`).
		WithArgs(newDate, 7, "New", dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// No reached change -> no recompute. Zero tasks -> shouldCompleteMilestone
	// is false: no auto-reach cascade (no reached UPDATE expected).
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(0))

	dbcoreExpectMilestoneReload(mock)

	got := UpdateMilestone(dbcoreCtx(dbcoreUserID), dbcoreMilestoneID, types.Milestone{
		SubGoal: "New", Day: 7, Date: &newDate,
	})
	assert.Equal(t, "New", got.SubGoal)
}

func TestUpdateMilestone_ReachedFlip_TriggersRecompute(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectMilestoneFetch(mock, false)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "reached"=\$1 WHERE "milestone_id" = \$2`).
		WithArgs(true, dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// reached changed -> RecomputeAndSaveStatuses fires; its error is
	// discarded by design (`_ =`), so the flow continues.
	dbcoreExpectRecomputeSwallowed(mock)

	// One task still incomplete -> no auto-reach cascade.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1`).
		WithArgs(dbcoreMilestoneID).
		WillReturnRows(dbcoreCountRows(1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE milestone_id = \$1 AND completed = \$2`).
		WithArgs(dbcoreMilestoneID, false).
		WillReturnRows(dbcoreCountRows(1))

	dbcoreExpectMilestoneReload(mock)

	UpdateMilestone(dbcoreCtx(dbcoreUserID), dbcoreMilestoneID, types.Milestone{Reached: true})
}

// --- GetMilestoneForGeneration ---

func TestGetMilestoneForGeneration_EmptyUserID_NoQuery(t *testing.T) {
	withMockDB(t)

	assert.Equal(t, types.Milestone{}, GetMilestoneForGeneration("", dbcoreMilestoneID))
}

func TestGetMilestoneForGeneration_NotFound_ZeroValue(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(dbcoreUserID, dbcoreMilestoneID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))

	assert.Equal(t, types.Milestone{}, GetMilestoneForGeneration(dbcoreUserID, dbcoreMilestoneID))
}

// --- Single-column status setters ---

func TestUpdatePhaseStatus(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "phases" SET "status"=\$1 WHERE phase_id = \$2`).
		WithArgs("in_progress", dbcorePhaseID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, UpdatePhaseStatus(dbcorePhaseID, "in_progress"))
}

func TestUpdatePhaseStatus_ErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "phases" SET "status"=\$1`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	require.ErrorIs(t, UpdatePhaseStatus(dbcorePhaseID, "in_progress"), errDBCore)
}

func TestUpdateMilestoneStatus(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "status"=\$1 WHERE milestone_id = \$2`).
		WithArgs("completed", dbcoreMilestoneID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, UpdateMilestoneStatus(dbcoreMilestoneID, "completed"))
}

func TestUpdateMilestoneStatus_ErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "milestones" SET "status"=\$1`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	require.ErrorIs(t, UpdateMilestoneStatus(dbcoreMilestoneID, "completed"), errDBCore)
}

func TestUpdateRoadmapGenerationStatus(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "generation_status"=\$1 WHERE roadmap_id = \$2`).
		WithArgs("completed", dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, UpdateRoadmapGenerationStatus(t.Context(), dbcoreRoadmapID, "completed"))
}

func TestUpdateRoadmapGenerationStatus_ErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "generation_status"=\$1`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	require.ErrorIs(t, UpdateRoadmapGenerationStatus(t.Context(), dbcoreRoadmapID, "failed"), errDBCore)
}
