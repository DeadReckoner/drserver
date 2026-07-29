package handlers_test

// Synchronous pre-network branches of AgentGenerateMilestone and
// AgentGenerateNextMilestoneAsync, plus AgentGenerateAsync's remaining cheap
// guards. Every case stops before any AI provider call and before any
// generation goroutine is spawned (the 202-success spawn is a planned skip),
// so the tests stay hermetic (agent_http_test.go is the style reference).
//
// All package-level identifiers carry the hfin prefix (hfinGen for this file)
// to avoid collisions with concurrently-added test files.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"dr/server/api/handlers"
	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	hfinGenUserID      = "61616161-7272-8383-9494-050505050505"
	hfinGenMilestoneID = "16161616-2727-3838-4949-505050505050"
	hfinGenRoadmapID   = "26262626-3737-4848-5959-606060606060"
)

func hfinGenRouter(t *testing.T, gating bool) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	if gating {
		t.Setenv("PRO_GATING_ENABLED", "true")
	} else {
		t.Setenv("PRO_GATING_ENABLED", "false")
	}
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	return testutil.NewTestRouterAs(db, hfinGenUserID), mock
}

// hfinGenExpectAckMissing stubs requireAIAck's count query with zero rows
// (acknowledgment not yet accepted).
func hfinGenExpectAckMissing(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
		WithArgs(hfinGenUserID, types.CurrentAIAckVersion).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

// hfinGenExpectMilestone stubs GetMilestone's user-scoped fetch; found=true
// returns a bare milestone whose Tasks preload comes back empty.
func hfinGenExpectMilestone(mock sqlmock.Sqlmock, found bool) {
	q := mock.ExpectQuery(`SELECT \* FROM "milestones" WHERE user_id = \$1 AND milestone_id = \$2`).
		WithArgs(hfinGenUserID, hfinGenMilestoneID, 1)
	if !found {
		q.WillReturnError(gorm.ErrRecordNotFound)
		return
	}
	q.WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "user_id", "sub_goal"}).
		AddRow(hfinGenMilestoneID, hfinGenRoadmapID, hfinGenUserID, "Ship v1"))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE "tasks"\."milestone_id" = \$1 ORDER BY day ASC`).
		WithArgs(hfinGenMilestoneID).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}))
}

// hfinGenExpectFreeCapReached stubs the CheckAIGeneration flow for a free
// user already at the daily cap: entitlement row, then active roadmap/habit
// counts. The pro-whitelist email lookup in between is deliberately not
// stubbed - it soft-fails against the mock (unmatched query -> treated as not
// whitelisted) without consuming any pending expectation.
func hfinGenExpectFreeCapReached(mock sqlmock.Sqlmock) {
	// Period start "now" keeps shouldResetAIPeriod false regardless of the
	// day the test runs.
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(hfinGenUserID, 1).
		WillReturnRows(sqlmock.NewRows(
			[]string{"user_id", "plan", "ai_generations_used", "ai_generations_period_start"}).
			AddRow(hfinGenUserID, "free", 99, time.Now().UTC()))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(hfinGenUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(hfinGenUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

// --- AgentGenerateMilestone ---------------------------------------------------

func TestAgentGenerateMilestone_BlockedWithoutAck_403(t *testing.T) {
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectAckMissing(mock)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generateMilestone/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ai_ack_required", resp["code"])
}

func TestAgentGenerateMilestone_MilestoneNotFound_404(t *testing.T) {
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectMilestone(mock, false)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generateMilestone/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Milestone not found")
}

func TestAgentGenerateMilestone_MalformedJSON_400(t *testing.T) {
	// The milestone lookup precedes binding, so it must be stubbed even for a
	// body failure.
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectMilestone(mock, true)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generateMilestone/"+hfinGenMilestoneID, hfinGenUserID, "not-an-object")

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAgentGenerateMilestone_FreeCapReached_402(t *testing.T) {
	router, mock := hfinGenRouter(t, true)

	hfinGenExpectMilestone(mock, true)
	hfinGenExpectFreeCapReached(mock)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generateMilestone/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	require.Equal(t, http.StatusPaymentRequired, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "plan_limit")
	assert.Contains(t, w.Body.String(), "ai_generations_per_day")
}

func TestAgentGenerateMilestone_PlanCheckError_500(t *testing.T) {
	router, mock := hfinGenRouter(t, true)

	hfinGenExpectMilestone(mock, true)
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(hfinGenUserID, 1).
		WillReturnError(errors.New("entitlement lookup failed"))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generateMilestone/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- AgentGenerateNextMilestoneAsync -----------------------------------------

func TestAgentNextMilestoneAsync_InvalidUUID_400(t *testing.T) {
	router, _ := hfinGenRouter(t, false)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-next-milestone-async/not-a-uuid", hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAgentNextMilestoneAsync_BlockedWithoutAck_403(t *testing.T) {
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectAckMissing(mock)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-next-milestone-async/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ai_ack_required")
}

func TestAgentNextMilestoneAsync_MalformedJSON_400(t *testing.T) {
	// Binding precedes the milestone lookup here (opposite order to
	// AgentGenerateMilestone), so no DB stubs are needed.
	router, _ := hfinGenRouter(t, false)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-next-milestone-async/"+hfinGenMilestoneID, hfinGenUserID,
		"not-an-object")

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAgentNextMilestoneAsync_MilestoneNotFound_404(t *testing.T) {
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectMilestone(mock, false)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-next-milestone-async/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Milestone not found")
}

func TestAgentNextMilestoneAsync_FreeCapReached_402(t *testing.T) {
	router, mock := hfinGenRouter(t, true)

	hfinGenExpectMilestone(mock, true)
	hfinGenExpectFreeCapReached(mock)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-next-milestone-async/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	require.Equal(t, http.StatusPaymentRequired, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "plan_limit")
}

func TestAgentNextMilestoneAsync_JobInsertFailure_500(t *testing.T) {
	// The background_jobs INSERT failing stops the request before the
	// generation goroutine is spawned, keeping this hermetic.
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectMilestone(mock, true)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "background_jobs"`).
		WillReturnError(errors.New("insert exploded"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-next-milestone-async/"+hfinGenMilestoneID, hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Failed to create job")
}

// --- AgentGenerateAsync: remaining cheap guards -------------------------------

func TestAgentGenerateAsync_BlockedWithoutAck_403(t *testing.T) {
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))
	router, mock := hfinGenRouter(t, false)

	hfinGenExpectAckMissing(mock)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-async", hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ai_ack_required")
}

func TestAgentGenerateAsync_PlanCheckError_500(t *testing.T) {
	router, mock := hfinGenRouter(t, true)

	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(hfinGenUserID, 1).
		WillReturnError(errors.New("entitlement lookup failed"))

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-async", hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestAgentGenerateAsync_DraftInsertFailure_500(t *testing.T) {
	// Job insert succeeds, then the draft roadmap insert fails - still before
	// the generation goroutine, so nothing async is left running.
	router, mock := hfinGenRouter(t, false)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "background_jobs"`).
		WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("job-1"))
	mock.ExpectCommit()

	// CreateDraftRoadmap first ensures the user row exists.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(hfinGenUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(hfinGenUserID))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "roadmaps"`).
		WillReturnError(errors.New("draft insert exploded"))
	mock.ExpectRollback()

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/agent/generate-async", hfinGenUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Failed to create draft")
}
