package handlers

import (
	"crypto/subtle"
	"net/http"
	"os"
	"time"

	"dr/server/api/utils"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AdminHandler holds admin-only endpoints. Today the only one is a comp tool
// for flipping a user's entitlement plan without going through payments -
// useful for early testers and support overrides before billing webhooks
// (Step 6) are wired up.
//
// Authentication is intentionally minimal: a single ADMIN_API_KEY shared
// secret in X-Admin-Key. This is appropriate for Step 2's scope (gating
// rollout) and is expected to be replaced with proper RBAC before public
// launch.
type AdminHandler struct {
	db *gorm.DB
}

func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

// SetUserPlanRequest is the body for POST /admin/users/:userid/plan.
type SetUserPlanRequest struct {
	// Plan must be one of types.PlanFree or types.PlanPaid.
	Plan string `json:"plan" binding:"required"`
	// Source identifies how this entitlement was granted. Use "comp" for
	// manual overrides; the webhook handler (Step 6) will set apple/google/
	// stripe automatically. Defaults to "comp" when omitted.
	Source string `json:"source"`
	// ExpiresAt optionally sets when the comp expires. Omit for indefinite.
	ExpiresAt *time.Time `json:"expires_at"`
}

// SetUserPlan flips a user's entitlement. Lazily creates the entitlements row
// if missing (GetEntitlement handles this).
//
//	POST /admin/users/:userid/plan
//	X-Admin-Key: <ADMIN_API_KEY>
//	{ "plan": "paid", "source": "comp", "expires_at": "2026-12-31T00:00:00Z" }
func (h *AdminHandler) SetUserPlan(c *gin.Context) {
	userID := c.Param("userid")
	if userID == "" {
		utils.BadRequestResponse(c, "userid path param required")
		return
	}

	var req SetUserPlanRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}
	if req.Plan != types.PlanFree && req.Plan != types.PlanPaid {
		utils.BadRequestResponse(c, "plan must be 'free' or 'paid'")
		return
	}
	source := req.Source
	if source == "" {
		source = types.EntitlementSourceComp
	}
	switch source {
	case types.EntitlementSourceDefault, types.EntitlementSourceApple,
		types.EntitlementSourceGoogle, types.EntitlementSourceStripe,
		types.EntitlementSourceComp:
		// ok
	default:
		utils.BadRequestResponse(c, "source must be one of default|apple|google|stripe|comp")
		return
	}

	updates := map[string]interface{}{
		"plan":   req.Plan,
		"source": source,
	}
	if req.ExpiresAt != nil {
		updates["current_period_end"] = req.ExpiresAt
	}
	if req.Plan == types.PlanFree {
		// Clear paid-side fields when downgrading so we don't leave stale
		// trial/period metadata around.
		updates["status"] = nil
		updates["trial_ends_at"] = nil
		updates["current_period_end"] = nil
		updates["product_id"] = nil
	}

	// Ensure the row exists, then patch.
	ent, err := getOrCreateEntitlement(h.db, userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	if err := h.db.Model(&types.Entitlement{}).
		Where("user_id = ?", userID).
		Updates(updates).Error; err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	// Reload to return canonical state to the caller.
	if err := h.db.Where("user_id = ?", userID).First(ent).Error; err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, ent)
}

// AdminAuthMiddleware checks the X-Admin-Key header against the ADMIN_API_KEY
// env var. Returns 503 when the env var isn't set (so a misconfigured prod
// doesn't silently accept any key) and 403 on mismatch.
func AdminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := os.Getenv("ADMIN_API_KEY")
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable,
				gin.H{"error": "Admin endpoints disabled: ADMIN_API_KEY not configured"})
			return
		}
		// Constant-time: this endpoint is reachable without a Supabase JWT and
		// has no rate limit in front of it, so a length-or-prefix-dependent
		// comparison is the kind of thing worth not having.
		got := c.GetHeader("X-Admin-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden,
				gin.H{"error": "Invalid or missing X-Admin-Key"})
			return
		}
		c.Next()
	}
}

// getOrCreateEntitlement is a thin wrapper kept local to handlers so we don't
// expose the lazy-create behavior beyond admin/billing paths. database.
// GetEntitlement already does this internally; this indirection just keeps
// admin.go importing only the database package surface it needs.
func getOrCreateEntitlement(db *gorm.DB, userID string) (*types.Entitlement, error) {
	var ent types.Entitlement
	if err := db.Where("user_id = ?", userID).First(&ent).Error; err == nil {
		return &ent, nil
	}
	ent = types.Entitlement{
		UserID:                   userID,
		Plan:                     types.PlanFree,
		Source:                   types.EntitlementSourceDefault,
		AIGenerationsPeriodStart: time.Now().UTC(),
	}
	if err := db.Create(&ent).Error; err != nil {
		// Race with the user-insert trigger - re-read.
		if err2 := db.Where("user_id = ?", userID).First(&ent).Error; err2 == nil {
			return &ent, nil
		}
		return nil, err
	}
	return &ent, nil
}
