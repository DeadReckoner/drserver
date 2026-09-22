package database

// Tests for the roadmap sharing flow in roadmap_lifecycle.go:
// GenerateShareToken / RevokeShareToken / GetRoadmapByShareToken /
// CopyRoadmapForUser guards, plus a direct test of the copyMilestone deep
// copy (the meat of the copy flow) and GetRoadmapFullByID's error path.

import (
	"database/sql/driver"
	"encoding/base64"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dbcoreShareToken = "dbcore-share-token"

// --- GenerateShareToken ---

func TestGenerateShareToken_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := GenerateShareToken(dbcoreCtx(""), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestGenerateShareToken_RoadmapNotFound(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedNotFound(mock)

	_, err := GenerateShareToken(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestGenerateShareToken_UpdateErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, false)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE roadmap_id = \$2`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	_, err := GenerateShareToken(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.ErrorIs(t, err, errDBCore)
}

func TestGenerateShareToken_HappyPath_TokenShapeAndPersistence(t *testing.T) {
	mock := withMockDB(t)
	dbcoreExpectRoadmapOwnedFetch(mock, nil, nil, false)

	var storedToken driver.Value
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE roadmap_id = \$2`).
		WithArgs(dbcoreArgCapture{&storedToken}, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	token, err := GenerateShareToken(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.NoError(t, err)
	assert.Len(t, token, 32, "24 random bytes -> 32 base64url chars")
	decoded, decErr := base64.URLEncoding.DecodeString(token)
	require.NoError(t, decErr, "token must be base64.URLEncoding-decodable")
	assert.Len(t, decoded, 24)
	assert.Equal(t, token, storedToken, "the stored token is the returned token")
}

// --- RevokeShareToken ---

func TestRevokeShareToken_RowsAffectedZero_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WithArgs(nil, dbcoreUserID, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := RevokeShareToken(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID)
	require.Error(t, err)
	assert.Equal(t, "roadmap not found", err.Error())
}

func TestRevokeShareToken_Success_ClearsToken(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1 WHERE user_id = \$2 AND roadmap_id = \$3`).
		WithArgs(nil, dbcoreUserID, dbcoreRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, RevokeShareToken(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID))
}

func TestRevokeShareToken_DBErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "roadmaps" SET "share_token"=\$1`).
		WillReturnError(errDBCore)
	mock.ExpectRollback()

	require.ErrorIs(t, RevokeShareToken(dbcoreCtx(dbcoreUserID), dbcoreRoadmapID), errDBCore)
}

// --- GetRoadmapByShareToken ---

func TestGetRoadmapByShareToken_NotFound(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE share_token = \$1`).
		WithArgs(dbcoreShareToken, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	_, err := GetRoadmapByShareToken(dbcoreShareToken)
	require.Error(t, err)
	assert.Equal(t, "shared roadmap not found", err.Error())
}

func TestGetRoadmapByShareToken_OtherErrorPassthrough(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE share_token = \$1`).
		WithArgs(dbcoreShareToken, 1).
		WillReturnError(errDBCore)

	_, err := GetRoadmapByShareToken(dbcoreShareToken)
	require.ErrorIs(t, err, errDBCore)
}

func TestGetRoadmapByShareToken_MinimalHappyPath(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE share_token = \$1`).
		WithArgs(dbcoreShareToken, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal"}).
			AddRow(dbcoreRoadmapID, dbcoreUserID, "Shared goal"))
	mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE "milestones"\."roadmap_id" = \$1 ORDER BY day ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id"}))
	mock.ExpectQuery(`SELECT \* FROM "phases" WHERE "phases"\."roadmap_id" = \$1 ORDER BY order_index ASC`).
		WithArgs(dbcoreRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"phase_id"}))

	got, err := GetRoadmapByShareToken(dbcoreShareToken)
	require.NoError(t, err)
	assert.Equal(t, "Shared goal", got.Goal)
}

// --- CopyRoadmapForUser ---

func TestCopyRoadmapForUser_MissingUserID(t *testing.T) {
	withMockDB(t)

	_, err := CopyRoadmapForUser(dbcoreCtx(""), dbcoreShareToken)
	require.Error(t, err)
	assert.Equal(t, "missing or invalid user_id", err.Error())
}

func TestCopyRoadmapForUser_UnknownToken_NoInsert(t *testing.T) {
	mock := withMockDB(t)
	// The share-token lookup fails before any INSERT happens.
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE share_token = \$1`).
		WithArgs(dbcoreShareToken, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id"}))

	_, err := CopyRoadmapForUser(dbcoreCtx(dbcoreUserID), dbcoreShareToken)
	require.Error(t, err)
	assert.Equal(t, "shared roadmap not found", err.Error())
}

// --- copyMilestone (pure, no DB) ---

func TestCopyMilestone_DeepCopyResetsStateAndRegeneratesIDs(t *testing.T) {
	oldDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	oldPhase := "ffffffff-0000-0000-0000-000000000001"
	forecast := "2 books, 1 course"
	deps := "task-a"
	original := types.Milestone{
		MilestoneID:       "ffffffff-0000-0000-0000-000000000002",
		RoadmapID:         "ffffffff-0000-0000-0000-000000000003",
		UserID:            "ffffffff-0000-0000-0000-000000000004",
		PhaseID:           &oldPhase,
		SubGoal:           "Run 10k",
		Description:       "desc",
		Day:               9,
		Reached:           true,
		Date:              &oldDate,
		Status:            "completed",
		AcceptanceTest:    "finish under an hour",
		DurationDaysMin:   2,
		DurationDaysMax:   5,
		OrderIndex:        3,
		ResourcesForecast: &forecast,
		Spikes:            []types.Spike{{SpikeID: "old-spike", MilestoneID: "old", Description: "shoes?", TimeboxDays: 4}},
		Risks:             []types.Risk{{RiskID: "old-risk", MilestoneID: "old", Description: "injury", Severity: "high"}},
		Tasks: []types.Task{{
			TaskID:       "old-task",
			RoadmapID:    "old-roadmap",
			MilestoneID:  "old",
			UserID:       "old-user",
			Title:        "Interval session",
			Description:  "tdesc",
			Status:       "completed",
			Rationale:    "speed",
			EstMinutes:   45,
			Dependencies: &deps,
			Confidence:   "high",
			Day:          2,
			Completed:    true,
			Date:         &oldDate,
			Steps: []types.Step{{
				StepID: "old-step", TaskID: "old", Description: "warm up",
				Completed: true, Status: "completed", Number: 2,
			}},
		}},
	}

	newPhase := "ffffffff-0000-0000-0000-000000000005"
	got := copyMilestone(original, dbcoreRoadmapID, dbcoreUserID, &newPhase)

	// Fresh IDs everywhere.
	dbcoreRequireUUID(t, got.MilestoneID, "copied milestone id")
	assert.NotEqual(t, original.MilestoneID, got.MilestoneID)
	dbcoreRequireUUID(t, got.Spikes[0].SpikeID, "copied spike id")
	assert.NotEqual(t, "old-spike", got.Spikes[0].SpikeID)
	dbcoreRequireUUID(t, got.Risks[0].RiskID, "copied risk id")
	dbcoreRequireUUID(t, got.Tasks[0].TaskID, "copied task id")
	assert.NotEqual(t, "old-task", got.Tasks[0].TaskID)
	dbcoreRequireUUID(t, got.Tasks[0].Steps[0].StepID, "copied step id")

	// FKs rewired to the new owner.
	assert.Equal(t, dbcoreRoadmapID, got.RoadmapID)
	assert.Equal(t, dbcoreUserID, got.UserID)
	require.NotNil(t, got.PhaseID)
	assert.Equal(t, newPhase, *got.PhaseID)
	assert.Equal(t, got.MilestoneID, got.Spikes[0].MilestoneID)
	assert.Equal(t, got.MilestoneID, got.Risks[0].MilestoneID)
	assert.Equal(t, got.MilestoneID, got.Tasks[0].MilestoneID)
	assert.Equal(t, dbcoreRoadmapID, got.Tasks[0].RoadmapID)
	assert.Equal(t, dbcoreUserID, got.Tasks[0].UserID)
	assert.Equal(t, got.Tasks[0].TaskID, got.Tasks[0].Steps[0].TaskID)

	// State reset: the copy starts life unstarted.
	assert.False(t, got.Reached)
	assert.Nil(t, got.Date)
	assert.Equal(t, "not_started", got.Status)
	assert.Equal(t, "not_started", got.Tasks[0].Status)
	assert.False(t, got.Tasks[0].Completed)
	assert.Nil(t, got.Tasks[0].Date)
	assert.False(t, got.Tasks[0].Steps[0].Completed)
	assert.Equal(t, "incomplete", got.Tasks[0].Steps[0].Status)

	// Content copied verbatim.
	assert.Equal(t, "Run 10k", got.SubGoal)
	assert.Equal(t, "desc", got.Description)
	assert.Equal(t, 9, got.Day)
	assert.Equal(t, "finish under an hour", got.AcceptanceTest)
	assert.Equal(t, 2, got.DurationDaysMin)
	assert.Equal(t, 5, got.DurationDaysMax)
	assert.Equal(t, 3, got.OrderIndex)
	assert.Equal(t, &forecast, got.ResourcesForecast)
	assert.Equal(t, "shoes?", got.Spikes[0].Description)
	assert.Equal(t, 4, got.Spikes[0].TimeboxDays)
	assert.Equal(t, "injury", got.Risks[0].Description)
	assert.Equal(t, "high", got.Risks[0].Severity)
	task := got.Tasks[0]
	assert.Equal(t, "Interval session", task.Title)
	assert.Equal(t, "tdesc", task.Description)
	assert.Equal(t, "speed", task.Rationale)
	assert.Equal(t, 45, task.EstMinutes)
	assert.Equal(t, &deps, task.Dependencies)
	assert.Equal(t, "high", task.Confidence)
	assert.Equal(t, 2, task.Day)
	assert.Equal(t, "warm up", task.Steps[0].Description)
	assert.Equal(t, 2, task.Steps[0].Number)
}

func TestCopyMilestone_NilPhaseIDStaysNil(t *testing.T) {
	got := copyMilestone(types.Milestone{SubGoal: "sg"}, dbcoreRoadmapID, dbcoreUserID, nil)
	assert.Nil(t, got.PhaseID)
}

// --- GetRoadmapFullByID ---

func TestGetRoadmapFullByID_FetchErrorPropagated(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbcoreRoadmapID, 1).
		WillReturnError(errDBCore)

	got, err := GetRoadmapFullByID(dbcoreRoadmapID)
	require.ErrorIs(t, err, errDBCore)
	assert.Equal(t, types.Roadmap{}, got)
}
