package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// runEnforce invokes EnforceLimit against a fresh gin.Context. Returns the
// recorder so callers can inspect status/body, plus the boolean EnforceLimit
// returned.
func runEnforce(t *testing.T, check func() (*types.PlanLimitError, error)) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	ok := EnforceLimit(c, check)
	return w, ok
}

func TestEnforceLimit_FlagOff_Passes(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "")
	_, ok := runEnforce(t, func() (*types.PlanLimitError, error) {
		t.Fatal("check should not run when flag is off")
		return nil, nil
	})
	assert.True(t, ok)
}

func TestEnforceLimit_FlagOn_AllowedReturnsTrue(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "true")
	w, ok := runEnforce(t, func() (*types.PlanLimitError, error) {
		return nil, nil // allowed
	})
	assert.True(t, ok)
	assert.Equal(t, http.StatusOK, w.Code, "no response should have been written")
}

func TestEnforceLimit_FlagOn_LimitReturns402(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "true")
	limitErr := &types.PlanLimitError{
		Code: "plan_limit", Feature: "active_roadmaps", Limit: 1, Current: 1,
		Message: "Upgrade for more.",
	}
	w, ok := runEnforce(t, func() (*types.PlanLimitError, error) {
		return limitErr, nil
	})
	assert.False(t, ok)
	require.Equal(t, http.StatusPaymentRequired, w.Code)
	var body types.PlanLimitError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "plan_limit", body.Code)
	assert.Equal(t, "active_roadmaps", body.Feature)
	assert.Equal(t, 1, body.Limit)
	assert.Equal(t, 1, body.Current)
	assert.Equal(t, "Upgrade for more.", body.Message)
}

func TestEnforceLimit_FlagOn_CheckErrorReturns500(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "true")
	w, ok := runEnforce(t, func() (*types.PlanLimitError, error) {
		return nil, errors.New("db is on fire")
	})
	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestPlanLimitResponse_NilSafe(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	PlanLimitResponse(c, nil)
	require.Equal(t, http.StatusPaymentRequired, w.Code)
	var body types.PlanLimitError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "plan_limit", body.Code, "should fall back to generic plan_limit code")
}
