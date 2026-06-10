package handlers

import (
	"net/http"

	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// BillingHandler serves plan/entitlement state to the mobile client.
// Step 1 only exposes GET /billing/status. Step 6 will add the webhook handler.
type BillingHandler struct {
	db *gorm.DB
}

func NewBillingHandler(db *gorm.DB) *BillingHandler {
	return &BillingHandler{db: db}
}

// BillingUsage reports current period usage counts, used by the mobile client
// to render progress bars and pre-empt limit-hit modals before a 402.
type BillingUsage struct {
	ActiveRoadmaps          int `json:"active_roadmaps"`
	ActiveHabits            int `json:"active_habits"`
	AIGenerationsThisPeriod int `json:"ai_generations_this_period"`
}

// BillingStatusResponse is the shape returned by GET /api/v1/billing/status.
// Internal/provider fields on Entitlement are stripped via json:"-" tags.
type BillingStatusResponse struct {
	Plan        string           `json:"plan"`
	Source      string           `json:"source"`
	Status      *string          `json:"status,omitempty"`
	TrialEndsAt *string          `json:"trial_ends_at,omitempty"`
	RenewsAt    *string          `json:"renews_at,omitempty"`
	ProductID   *string          `json:"product_id,omitempty"`
	Limits      types.PlanLimits `json:"limits"`
	Usage       BillingUsage     `json:"usage"`
}

// GetBillingStatus returns the authoritative plan + limits + usage for the
// current user. Mobile calls this on app foreground and after a purchase to
// reconcile with whatever RevenueCat said client-side.
func (h *BillingHandler) GetBillingStatus(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists || userIDVal == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}
	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	ctx, err := database.GetUserPlanContext(userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	resp := BillingStatusResponse{
		Plan:      ctx.Entitlement.Plan,
		Source:    ctx.Entitlement.Source,
		Status:    ctx.Entitlement.Status,
		ProductID: ctx.Entitlement.ProductID,
		Limits:    ctx.Entitlement.Limits(),
		Usage: BillingUsage{
			ActiveRoadmaps:          ctx.ActiveRoadmapCount,
			ActiveHabits:            ctx.ActiveHabitCount,
			AIGenerationsThisPeriod: ctx.Entitlement.AIGenerationsUsed,
		},
	}
	if t := ctx.Entitlement.TrialEndsAt; t != nil {
		s := t.Format("2006-01-02T15:04:05Z07:00")
		resp.TrialEndsAt = &s
	}
	if t := ctx.Entitlement.CurrentPeriodEnd; t != nil {
		s := t.Format("2006-01-02T15:04:05Z07:00")
		resp.RenewsAt = &s
	}

	utils.DataResponse(c, http.StatusOK, resp)
}
