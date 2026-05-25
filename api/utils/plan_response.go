package utils

import (
	"net/http"

	"dr/server/middleware"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// PlanLimitResponse writes a 402 Payment Required with a structured body that
// the mobile client interprets to trigger the upgrade paywall. Aborts the
// gin context so subsequent middleware/handler logic doesn't run.
func PlanLimitResponse(c *gin.Context, err *types.PlanLimitError) {
	if err == nil {
		// Defensive: nil should not happen in practice, but treat as a generic
		// 402 rather than panicking inside a handler.
		err = &types.PlanLimitError{
			Code:    "plan_limit",
			Message: "This action requires an upgrade.",
		}
	}
	c.AbortWithStatusJSON(http.StatusPaymentRequired, err)
}

// EnforceLimit runs a plan check and short-circuits the request on failure.
// Returns true when the handler should proceed; false when a response has
// already been written (either 500 on lookup error or 402 on plan block).
//
// When the PRO_GATING_ENABLED flag is off, this is a no-op that always
// returns true - lets us ship gating dark and flip on per environment.
//
// Typical usage:
//
//	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
//	    return database.CheckCreateRoadmap(h.db, userID)
//	}) {
//	    return
//	}
func EnforceLimit(c *gin.Context, check func() (*types.PlanLimitError, error)) bool {
	if !middleware.IsGatingEnabled() {
		return true
	}
	limitErr, err := check()
	if err != nil {
		InternalErrorResponse(c, err)
		return false
	}
	if limitErr != nil {
		PlanLimitResponse(c, limitErr)
		return false
	}
	return true
}
