package handlers_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"dr/server/api/handlers"
	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const importUserID = "abababab-cdcd-efef-0101-232323232323"

// postPlanImport builds the multipart request POST /agent/import-plan expects.
// A nil pdf omits the file part entirely.
func postPlanImport(t *testing.T, router http.Handler, pdf []byte, contextJSON string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if pdf != nil {
		part, err := writer.CreateFormFile("file", "plan.pdf")
		require.NoError(t, err)
		_, err = part.Write(pdf)
		require.NoError(t, err)
	}
	if contextJSON != "" {
		require.NoError(t, writer.WriteField("context", contextJSON))
	}
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/import-plan", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// newImportRouter builds a router with the Anthropic key set, so the handler's
// importer is wired and validation runs. No request ever reaches the API: every
// case here fails before the background goroutine is started.
//
// The acknowledgment gate is disabled in production for now (see aiAckEnforced),
// so these requests pass through it without touching the DB — only the enforced
// test below sets an expectation.
func newImportRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key-not-used")

	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	return testutil.NewTestRouterAs(db, importUserID), mock
}

// TestImportPlan_ServiceUnavailableWithoutKey covers the wiring: plan import is
// Anthropic-only (PDF input), so without a key the endpoint says so rather than
// failing later inside the job.
func TestImportPlan_ServiceUnavailableWithoutKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	db, _ := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))
	router := testutil.NewTestRouterAs(db, importUserID)

	// The key check precedes the acknowledgment gate, so no query runs.
	w := postPlanImport(t, router, []byte("%PDF-1.7 stub"), "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ANTHROPIC_API_KEY")
}

func TestImportPlan_RejectsNonPDF(t *testing.T) {
	router, _ := newImportRouter(t)

	// A .docx renamed to .pdf: the magic number is what we trust, not the
	// client-supplied name or content type.
	w := postPlanImport(t, router, []byte("PK\x03\x04 not a pdf"), "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "not a PDF")
}

func TestImportPlan_RequiresFile(t *testing.T) {
	router, _ := newImportRouter(t)

	w := postPlanImport(t, router, nil, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "file")
}

// TestImportPlan_RejectsMalformedContext keeps a client bug from silently
// costing the user one of their daily AI generations.
func TestImportPlan_RejectsMalformedContext(t *testing.T) {
	router, _ := newImportRouter(t)

	w := postPlanImport(t, router, []byte("%PDF-1.7 stub"), "{not json")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid context JSON")
}

// TestImportPlan_RejectsOversizedUpload covers both size guards: the file-size
// check, and the request-body cap that keeps net/http from buffering a much
// larger body in memory before the handler ever sees it.
func TestImportPlan_RejectsOversizedUpload(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{name: "over the file cap", size: 11 << 20},
		{name: "over the whole-body cap", size: 13 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newImportRouter(t)

			pdf := make([]byte, tt.size)
			copy(pdf, "%PDF-1.7")

			w := postPlanImport(t, router, pdf, "")
			require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "too large")
		})
	}
}

// TestImportPlan_RequiresAIAck confirms the import path is gated by the same
// acknowledgment as the generation endpoints.
func TestImportPlan_RequiresAIAck(t *testing.T) {
	// Enforcement is off in production for now (see aiAckEnforced), so enable
	// it explicitly to keep the hardened path covered on this route too.
	t.Cleanup(handlers.SetAIAckEnforcedForTest(true))

	router, mock := newImportRouter(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
		WithArgs(importUserID, types.CurrentAIAckVersion).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := postPlanImport(t, router, []byte("%PDF-1.7 stub"), "")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ai_ack_required")
}
