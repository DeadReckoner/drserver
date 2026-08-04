package handlers_test

// HTTP-level tests for AgentHandler: job-status shaping, the generate/
// generate-async guard branches that stop before any AI provider call, the
// deprecated /agent/sessions endpoints, the unconfigured-Anthropic 503s, and
// the /ws/agent/:jobid websocket (initial status frame, ping/pong, and the
// not-found / failed-upgrade branches).
//
// All package-level identifiers carry the hroad prefix to avoid collisions
// with concurrently-added test files.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dr/server/api/handlers"
	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	hroadAgentUserID = "31313131-4242-5353-6464-757575757575"
	hroadAgentJobID  = "13131313-2424-3535-4646-575757575757"
)

var hroadAgentCreatedAt = time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

// hroadExpectJobLookup stubs GetBackgroundJob's user-scoped fetch.
func hroadExpectJobLookup(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`SELECT \* FROM "background_jobs" WHERE job_id = \$1 AND user_id = \$2`).
		WithArgs(hroadAgentJobID, hroadAgentUserID, 1)
}

// --- GetJobStatus ------------------------------------------------------------

func TestAgentJobStatus_NotFound_404(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	hroadExpectJobLookup(mock).WillReturnError(gorm.ErrRecordNotFound)

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestAgentJobStatus_Processing_NoResultOrErrorKeys(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "created_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, "roadmap", "processing", 42, hroadAgentCreatedAt))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, hroadAgentJobID, resp["job_id"])
	assert.Equal(t, "processing", resp["status"])
	assert.Equal(t, float64(42), resp["progress"])
	assert.NotContains(t, resp, "result", "in-flight jobs must not expose a result")
	assert.NotContains(t, resp, "error")
}

func TestAgentJobStatus_Completed_IncludesResultAndCompletedAt(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	completedAt := hroadAgentCreatedAt.Add(2 * time.Minute)
	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "result", "created_at", "completed_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, "roadmap", "completed", 100,
				[]byte(`{"roadmap_id":"abc-123"}`), hroadAgentCreatedAt, completedAt))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "completed", resp["status"])
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok, "completed jobs must include the JSONB result: %s", w.Body.String())
	assert.Equal(t, "abc-123", result["roadmap_id"])
	assert.Contains(t, resp, "completed_at")
}

func TestAgentJobStatus_Failed_IncludesErrorAndFailedAt(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	failedAt := hroadAgentCreatedAt.Add(time.Minute)
	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "error", "created_at", "completed_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, "roadmap", "failed", 10, "model timeout", hroadAgentCreatedAt, failedAt))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "failed", resp["status"])
	assert.Equal(t, "model timeout", resp["error"])
	assert.Contains(t, resp, "failed_at")
	assert.NotContains(t, resp, "result")
}

// --- AgentGenerate guards ----------------------------------------------------

func TestAgentGenerate_BlockedWithoutAck_403(t *testing.T) {
	// The sync generate endpoint shares requireAIAck with generate-async; the
	// gate fires before any OpenAI call, keeping this hermetic. Enforcement is
	// off in production, so flip it on for the test.
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))

	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
		WithArgs(hroadAgentUserID, types.CurrentAIAckVersion).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/generate", hroadAgentUserID,
		map[string]string{"goalName": "Run a marathon"})

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ai_ack_required", resp["code"])
}

func TestAgentGenerate_MalformedJSON_400(t *testing.T) {
	// Ack gate off (production default) -> binding is the first live check.
	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/generate", hroadAgentUserID, "not-an-object")

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// --- AgentGenerateAsync guards ----------------------------------------------

func TestAgentGenerateAsync_MalformedJSON_400(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/generate-async", hroadAgentUserID, "not-an-object")

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAgentGenerateAsync_JobInsertFailure_500(t *testing.T) {
	// The background_jobs INSERT failing stops the request before the draft
	// roadmap and the generation goroutine, so this path is fully hermetic.
	t.Setenv("PRO_GATING_ENABLED", "false")

	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "background_jobs"`).
		WillReturnError(errors.New("insert exploded"))
	mock.ExpectRollback()

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/generate-async", hroadAgentUserID,
		map[string]string{"goalName": "Run a marathon"})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Failed to create job")
}

// --- Deprecated /agent/sessions endpoints ------------------------------------

func TestAgentSessions_AllEndpointsReturn410Gone(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	router := testutil.NewTestRouterAs(db, hroadAgentUserID)

	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/agent/sessions"},
		{http.MethodGet, "/api/v1/agent/sessions/some-id"},
		{http.MethodPost, "/api/v1/agent/sessions/some-id/reply"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := testutil.Do(t, router, tc.method, tc.path, hroadAgentUserID, nil)
			assert.Equal(t, http.StatusGone, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "/agent/chat",
				"the deprecation message must point at the supported endpoint")
		})
	}
}

// --- Anthropic chat: unconfigured 503s ---------------------------------------

func TestAgentChat_Unconfigured_503(t *testing.T) {
	// Clearing the key BEFORE the router is built makes NewAgentHandler skip
	// the Anthropic service, so the 503 is deterministic even on machines
	// where ANTHROPIC_API_KEY is exported.
	t.Setenv("ANTHROPIC_API_KEY", "")

	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	router := testutil.NewTestRouterAs(db, hroadAgentUserID)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/chat", hroadAgentUserID,
		map[string]string{"message": "hi", "mode": "roadmap_generation"})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ANTHROPIC_API_KEY")
}

func TestAgentEndChatSession_Unconfigured_503(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	router := testutil.NewTestRouterAs(db, hroadAgentUserID)

	w := testutil.Do(t, router, http.MethodDelete, "/api/v1/agent/chat/session/some-session", hroadAgentUserID, nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}

// --- WSAgentProgress ---------------------------------------------------------

func TestAgentWS_InitialStatusFrame_ThenPingPong(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	// Job at progress 42 -> the initial frame's message locks the
	// getProgressMessage 40-79 band.
	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "created_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, "roadmap", "processing", 42, hroadAgentCreatedAt))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws/agent/" + hroadAgentJobID
	header := http.Header{}
	header.Set(testutil.TestUserIDHeader, hroadAgentUserID)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	require.NoError(t, err, "dial failed")
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	// First frame: the current job status.
	var status types.ProgressUpdate
	require.NoError(t, conn.ReadJSON(&status))
	assert.Equal(t, "status", status.Type)
	assert.Equal(t, hroadAgentJobID, status.JobID)
	assert.Equal(t, 42, status.Progress)
	assert.Equal(t, "processing", status.Status)
	assert.Equal(t, "Creating detailed tasks...", status.Message)

	// Ping -> pong keep-alive loop.
	require.NoError(t, conn.WriteJSON(map[string]string{"type": "ping"}))
	var pong map[string]string
	require.NoError(t, conn.ReadJSON(&pong))
	assert.Equal(t, "pong", pong["type"])
}

func TestAgentWS_JobNotFound_HandshakeRejected404(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	hroadExpectJobLookup(mock).WillReturnError(gorm.ErrRecordNotFound)

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws/agent/" + hroadAgentJobID

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err, "handshake must fail when the job doesn't exist")
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAgentWS_PlainHTTPRequest_UpgradeFails(t *testing.T) {
	// A request without Upgrade headers reaches the handler (job found) but the
	// websocket upgrade fails. Documents current behavior: gorilla's Upgrader
	// writes its own 400 before the handler's c.JSON(500, ...) runs, so the
	// client sees status 400 — the handler's 500 status is unreachable, though
	// its body is still appended.
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "created_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, "roadmap", "processing", 10, hroadAgentCreatedAt))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/ws/agent/"+hroadAgentJobID, hroadAgentUserID, nil)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Websocket upgrade failed")
}

// --- stale job expiry on the status read -------------------------------------
//
// Generation runs in an in-process goroutine with no durable queue behind it, so
// a deploy or a crash mid-generation leaves the row 'processing' with nobody
// left to finish it. The status endpoint is what the mobile client polls, so it
// is where that has to become a terminal state — otherwise the client waits on
// a dead job forever.

func TestAgentJobStatus_AbandonedJobIsReportedAsFailed(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	// A goal-discovery job stuck in 'processing' well past its 15-minute window.
	staleCreatedAt := time.Now().Add(-2 * time.Hour)
	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "created_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, string(types.JobTypeGoalDiscovery),
				string(types.JobStatusProcessing), 25, staleCreatedAt))

	// The read retires it, guarded on the row still being unfinished so a worker
	// that is genuinely still alive cannot be overwritten. GORM wraps a write in
	// its own transaction.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "background_jobs" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// The response describes the row that now exists, not the one that was read.
	assert.Equal(t, string(types.JobStatusFailed), resp["status"],
		"a client polling an abandoned job must be told it is over")
	assert.Equal(t, types.JobExpiredError, resp["error"],
		"the reason must be typed so the client can tell it from a generation failure")
	assert.Contains(t, resp, "failed_at")
	// The client's recovery path keys off this: answers preserved, retry offered.
	assert.Contains(t, resp["error"], "try again")
}

// A job still inside its window is genuinely running and must be left alone —
// no UPDATE is expected here, and sqlmock fails the test if one is issued.
func TestAgentJobStatus_RecentInFlightJobIsNotExpired(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "created_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, string(types.JobTypeGoalDiscovery),
				string(types.JobStatusProcessing), 25, time.Now().Add(-30*time.Second)))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, string(types.JobStatusProcessing), resp["status"])
	assert.NotContains(t, resp, "error")
}

// A roadmap job gets the same treatment on a longer window — the goal discovery
// FINAL roadmap is an ordinary roadmap_generation job, so its pending state
// needs the same escape hatch.
func TestAgentJobStatus_RoadmapJobUsesItsOwnStaleWindow(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	// 20 minutes: past the goal-discovery window, inside the roadmap one.
	hroadExpectJobLookup(mock).WillReturnRows(
		sqlmock.NewRows([]string{"job_id", "user_id", "type", "status", "progress", "created_at"}).
			AddRow(hroadAgentJobID, hroadAgentUserID, string(types.JobTypeRoadmap),
				string(types.JobStatusProcessing), 50, time.Now().Add(-20*time.Minute)))

	router := testutil.NewTestRouterAs(db, hroadAgentUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/jobs/"+hroadAgentJobID+"/status", hroadAgentUserID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, string(types.JobStatusProcessing), resp["status"],
		"a roadmap generation gets a longer window than an options generation")
}
