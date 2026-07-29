package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// umwValidUUID is a fixed, well-formed UUID used across the validation tests.
const umwValidUUID = "550e8400-e29b-41d4-a716-446655440000"

// umwValidationMessage decodes the error envelope and returns its message.
func umwValidationMessage(t *testing.T, body []byte) string {
	t.Helper()
	var resp ErrorResponseStruct
	require.NoError(t, json.Unmarshal(body, &resp), "body: %s", string(body))
	return resp.Message
}

func TestValidateUUID_AsRouteMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/x/:id", ValidateUUID("id"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})

	t.Run("valid UUID reaches handler", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x/"+umwValidUUID, nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "reached")
	})

	t.Run("invalid UUID aborts with 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x/not-a-uuid", nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "Invalid id: must be a valid UUID", umwValidationMessage(t, w.Body.Bytes()))
		assert.NotContains(t, w.Body.String(), "reached", "final handler must not run")
	})
}

func TestValidateUUID_EmptyParamDirectContext(t *testing.T) {
	// Gin routing can't produce an empty path param (it 404s instead), so the
	// empty-string branch is driven through a bare test context.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	ValidateUUID("id")(c)

	assert.True(t, c.IsAborted())
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "Invalid id: must be a valid UUID", umwValidationMessage(t, w.Body.Bytes()))
}

func TestValidateMultipleUUIDs_AsRouteMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/m/:a/:b", ValidateMultipleUUIDs("a", "b"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})

	t.Run("both valid reaches handler", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/m/"+umwValidUUID+"/"+umwValidUUID, nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "reached")
	})

	t.Run("second invalid names the offending param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/m/"+umwValidUUID+"/nope", nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "Invalid b: must be a valid UUID", umwValidationMessage(t, w.Body.Bytes()))
	})
}

func TestValidateMultipleUUIDs_MissingParamDirectContext(t *testing.T) {
	// A param that isn't present at all hits the dedicated empty-string branch
	// with its "Missing required parameter" message. Driven via a direct test
	// context because gin routing can't match a route with an absent segment.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/m", nil)
	c.Params = gin.Params{{Key: "a", Value: umwValidUUID}}

	ValidateMultipleUUIDs("a", "b")(c)

	assert.True(t, c.IsAborted())
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "Missing required parameter: b", umwValidationMessage(t, w.Body.Bytes()))
}

func TestValidateMultipleUUIDs_AbortShortCircuits(t *testing.T) {
	// Both params are invalid; only the first is reported — the middleware
	// aborts before validating the rest, so the response is a single error.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/m", nil)
	c.Params = gin.Params{
		{Key: "a", Value: "bad-a"},
		{Key: "b", Value: "bad-b"},
	}

	ValidateMultipleUUIDs("a", "b")(c)

	assert.True(t, c.IsAborted())
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "Invalid a: must be a valid UUID", umwValidationMessage(t, w.Body.Bytes()))
	assert.NotContains(t, w.Body.String(), "Invalid b", "later params must not be validated after abort")
}
