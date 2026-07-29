package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// umwNewTestCtx returns a fresh gin test context backed by a recorder, with a
// minimal request attached so helpers that read from c.Request don't panic.
func umwNewTestCtx(t *testing.T) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	return w, c
}

// umwDecodeError decodes the standard error envelope every client parses.
func umwDecodeError(t *testing.T, w *httptest.ResponseRecorder) ErrorResponseStruct {
	t.Helper()
	var body ErrorResponseStruct
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "error body must be valid JSON: %s", w.Body.String())
	return body
}

// TestErrorResponseHelpers_Contract pins down the status code and error
// envelope each helper emits — this is the API error contract every client
// parses, so any drift here is a breaking change.
func TestErrorResponseHelpers_Contract(t *testing.T) {
	tests := []struct {
		name        string
		invoke      func(c *gin.Context)
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "ErrorResponse teapot",
			invoke:      func(c *gin.Context) { ErrorResponse(c, http.StatusTeapot, "short and stout") },
			wantStatus:  http.StatusTeapot,
			wantMessage: "short and stout",
		},
		{
			name:        "ValidationErrorResponse prefixes err",
			invoke:      func(c *gin.Context) { ValidationErrorResponse(c, errors.New("bad field")) },
			wantStatus:  http.StatusBadRequest,
			wantMessage: "Invalid request data: bad field",
		},
		{
			name:        "NotFoundResponse names resource",
			invoke:      func(c *gin.Context) { NotFoundResponse(c, "Roadmap") },
			wantStatus:  http.StatusNotFound,
			wantMessage: "Roadmap not found",
		},
		{
			name:        "InternalErrorResponse prefixes err",
			invoke:      func(c *gin.Context) { InternalErrorResponse(c, errors.New("db is on fire")) },
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "Internal server error: db is on fire",
		},
		{
			name:        "ConflictResponse",
			invoke:      func(c *gin.Context) { ConflictResponse(c, "already exists") },
			wantStatus:  http.StatusConflict,
			wantMessage: "already exists",
		},
		{
			name:        "BadRequestResponse",
			invoke:      func(c *gin.Context) { BadRequestResponse(c, "nope") },
			wantStatus:  http.StatusBadRequest,
			wantMessage: "nope",
		},
		{
			name:        "ForbiddenResponse",
			invoke:      func(c *gin.Context) { ForbiddenResponse(c, "no access") },
			wantStatus:  http.StatusForbidden,
			wantMessage: "no access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, c := umwNewTestCtx(t)
			tt.invoke(c)

			require.Equal(t, tt.wantStatus, w.Code)
			body := umwDecodeError(t, w)
			// The error field is always the canonical status text for the code.
			assert.Equal(t, http.StatusText(tt.wantStatus), body.Error)
			assert.Equal(t, tt.wantMessage, body.Message)
			assert.Equal(t, tt.wantStatus, body.Code, "code field must echo the HTTP status")
		})
	}
}

func TestSuccessResponse_OmitsNilData(t *testing.T) {
	w, c := umwNewTestCtx(t)
	SuccessResponse(c, http.StatusCreated, "created", nil)

	require.Equal(t, http.StatusCreated, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "created", body["message"])
	// data carries omitempty, so a nil payload must not surface an explicit null.
	_, present := body["data"]
	assert.False(t, present, "nil data must omit the data key entirely")
}

func TestSuccessResponse_RoundTripsData(t *testing.T) {
	w, c := umwNewTestCtx(t)
	SuccessResponse(c, http.StatusOK, "ok", map[string]any{"id": "abc", "count": 2})

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["message"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "data must be an object")
	assert.Equal(t, "abc", data["id"])
	assert.Equal(t, float64(2), data["count"])
}

func TestDataResponse_RawPassthroughNoEnvelope(t *testing.T) {
	w, c := umwNewTestCtx(t)
	DataResponse(c, http.StatusOK, gin.H{"items": []string{"a", "b"}})

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// No message/data envelope — the payload is the whole body.
	assert.Equal(t, []any{"a", "b"}, body["items"])
	_, hasMessage := body["message"]
	assert.False(t, hasMessage, "DataResponse must not add an envelope")
	assert.Len(t, body, 1)
}

func TestMessageResponse_ExactShape(t *testing.T) {
	w, c := umwNewTestCtx(t)
	MessageResponse(c, http.StatusAccepted, "queued")

	require.Equal(t, http.StatusAccepted, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, map[string]any{"message": "queued"}, body)
}
