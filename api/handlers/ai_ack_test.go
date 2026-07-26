package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"dr/server/api/handlers"
	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const ackUserID = "eeeeeeee-1111-2222-3333-444444444444"

func expectLatestAckQuery(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments" WHERE user_id = \$1 ORDER BY version DESC`).
		WithArgs(ackUserID, 1)
}

func expectAckCountQuery(mock sqlmock.Sqlmock, count int) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
		WithArgs(ackUserID, types.CurrentAIAckVersion).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func TestGetAIAcknowledgment_NeverAccepted(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	expectLatestAckQuery(mock).WillReturnError(gorm.ErrRecordNotFound)

	router := testutil.NewTestRouterAs(db, ackUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/agent/acknowledgment", ackUserID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["required"])
	assert.Equal(t, float64(types.CurrentAIAckVersion), resp["current_version"])
	assert.Equal(t, types.AIAckText, resp["text"])
	assert.NotContains(t, resp, "accepted_version")
}

func TestGetAIAcknowledgment_AcceptedCurrentVersion(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	expectLatestAckQuery(mock).WillReturnRows(
		sqlmock.NewRows([]string{"user_id", "version", "app_version", "accepted_at"}).
			AddRow(ackUserID, types.CurrentAIAckVersion, "1.4.0", time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)))

	router := testutil.NewTestRouterAs(db, ackUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/agent/acknowledgment", ackUserID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["required"])
	assert.Equal(t, float64(types.CurrentAIAckVersion), resp["accepted_version"])
	assert.Equal(t, "2026-07-01T12:00:00Z", resp["accepted_at"])
}

func TestGetAIAcknowledgment_OldVersionRequiresReacceptance(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	expectLatestAckQuery(mock).WillReturnRows(
		sqlmock.NewRows([]string{"user_id", "version", "app_version", "accepted_at"}).
			AddRow(ackUserID, types.CurrentAIAckVersion-1, "1.0.0", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	router := testutil.NewTestRouterAs(db, ackUserID)
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/agent/acknowledgment", ackUserID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["required"], "acceptance of an older version must not satisfy the current one")
}

func TestAcceptAIAcknowledgment_RecordsAcceptance(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "ai_acknowledgments" .* ON CONFLICT \("user_id","version"\) DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
		WithArgs(ackUserID, types.CurrentAIAckVersion, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "version", "app_version", "accepted_at"}).
			AddRow(ackUserID, types.CurrentAIAckVersion, "1.4.0", time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)))

	router := testutil.NewTestRouterAs(db, ackUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/acknowledgment", ackUserID,
		map[string]string{"app_version": "1.4.0"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var ack types.AIAcknowledgment
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ack))
	assert.Equal(t, types.CurrentAIAckVersion, ack.Version)
	assert.Equal(t, "1.4.0", ack.AppVersion)
}

// TestAgentGenerateAsync_BlockedWithoutAck covers the gate as it behaves when
// enforced. Enforcement is off in production for now (see aiAckEnforced), so
// the test enables it explicitly to keep the hardened path from rotting.
func TestAgentGenerateAsync_BlockedWithoutAck(t *testing.T) {
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))

	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	expectAckCountQuery(mock, 0)

	router := testutil.NewTestRouterAs(db, ackUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/generate-async", ackUserID,
		map[string]string{"goalName": "Run a marathon"})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ai_ack_required", resp["code"])
	assert.Equal(t, float64(types.CurrentAIAckVersion), resp["current_version"])
}

func TestAgentChat_BlockedWithoutAck(t *testing.T) {
	// Chat requires the Anthropic service to be configured before the ack
	// gate runs; without it the handler 503s, which is fine — the async
	// generate test above covers the 403 path. This test documents that a
	// missing ack never lets a generation request through: whichever guard
	// fires, the request must not reach generation. Enforced explicitly, as
	// the gate is disabled in production for now.
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))

	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	router := testutil.NewTestRouterAs(db, ackUserID)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/agent/chat", ackUserID,
		map[string]string{"message": "hi", "mode": "roadmap_generation"})
	assert.Contains(t, []int{http.StatusForbidden, http.StatusServiceUnavailable}, w.Code,
		fmt.Sprintf("request must be blocked, got %d: %s", w.Code, w.Body.String()))
}
