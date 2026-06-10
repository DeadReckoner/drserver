package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// runAdminAuth issues a request through AdminAuthMiddleware to a stub handler
// that returns 200. Returns the recorder for assertion.
func runAdminAuth(t *testing.T, envKey, headerKey string) *httptest.ResponseRecorder {
	t.Helper()
	t.Setenv("ADMIN_API_KEY", envKey)

	router := gin.New()
	router.Use(AdminAuthMiddleware())
	router.POST("/admin/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	req := httptest.NewRequest(http.MethodPost, "/admin/ping", strings.NewReader(""))
	if headerKey != "" {
		req.Header.Set("X-Admin-Key", headerKey)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestAdminAuthMiddleware_NoEnvKey_Returns503(t *testing.T) {
	w := runAdminAuth(t, "", "anything")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"admin endpoints must refuse to authenticate when ADMIN_API_KEY isn't set")
}

func TestAdminAuthMiddleware_MissingHeader_Returns403(t *testing.T) {
	w := runAdminAuth(t, "s3cret", "")
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminAuthMiddleware_WrongHeader_Returns403(t *testing.T) {
	w := runAdminAuth(t, "s3cret", "wrong")
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminAuthMiddleware_CorrectHeader_Allows(t *testing.T) {
	w := runAdminAuth(t, "s3cret", "s3cret")
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
}
