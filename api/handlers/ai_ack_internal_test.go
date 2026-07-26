package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dr/server/database"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireAIAck_NotEnforcedByDefault pins the current production behavior:
// the gate is wired into every generation handler but lets requests through.
// The nil DB is deliberate — a gate that still queried with enforcement off
// would panic here rather than quietly pass.
func TestRequireAIAck_NotEnforcedByDefault(t *testing.T) {
	t.Cleanup(database.SetDBForTest(nil))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "eeeeeeee-1111-2222-3333-444444444444")

	require.False(t, aiAckEnforced, "enforcement stays off until the gate is hardened server-side")
	assert.True(t, requireAIAck(c), "generation must not be blocked while the client owns the acknowledgment")
	assert.False(t, c.IsAborted())
	assert.Equal(t, http.StatusOK, w.Code)
}
