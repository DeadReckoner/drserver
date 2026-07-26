package handlers

import (
	"net/http"

	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// aiAckStatusResponse is the payload for GET /agent/acknowledgment.
type aiAckStatusResponse struct {
	// Required is true when the user must (re-)accept before generating.
	Required       bool   `json:"required"`
	CurrentVersion int    `json:"current_version"`
	Text           string `json:"text"`
	// AcceptedVersion is the highest version the user has accepted; 0 when
	// they have never accepted.
	AcceptedVersion int     `json:"accepted_version,omitempty"`
	AcceptedAt      *string `json:"accepted_at,omitempty"`
}

// acceptAIAckRequest is the body for POST /agent/acknowledgment.
type acceptAIAckRequest struct {
	AppVersion string `json:"app_version"`
}

// GetAIAcknowledgment handles GET /agent/acknowledgment (drserver#65): tells
// the client whether the acknowledgment must be shown and serves the current
// copy so text updates don't require an app release.
func (h *AgentHandler) GetAIAcknowledgment(c *gin.Context) {
	userID := c.GetString("user_id")

	latest, err := database.GetLatestAIAck(userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	resp := aiAckStatusResponse{
		Required:       latest == nil || latest.Version < types.CurrentAIAckVersion,
		CurrentVersion: types.CurrentAIAckVersion,
		Text:           types.AIAckText,
	}
	if latest != nil {
		resp.AcceptedVersion = latest.Version
		formatted := latest.AcceptedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.AcceptedAt = &formatted
	}

	utils.DataResponse(c, http.StatusOK, resp)
}

// AcceptAIAcknowledgment handles POST /agent/acknowledgment (drserver#65):
// records the user's acceptance of the current acknowledgment version.
// Idempotent — re-accepting keeps the original timestamp.
func (h *AgentHandler) AcceptAIAcknowledgment(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	// Body is optional; app_version is best-effort metadata.
	var req acceptAIAckRequest
	_ = c.ShouldBindJSON(&req)

	ack, err := database.RecordAIAck(userID, types.CurrentAIAckVersion, req.AppVersion)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, ack)
	log.Info(ctx, "AI acknowledgment accepted", "version", types.CurrentAIAckVersion)
}

// aiAckEnforced controls whether requireAIAck actually blocks generation.
//
// Currently OFF: the mobile client tracks acceptance itself and gates the
// acknowledgment UI, so server-side enforcement only blocked users from
// creating roadmaps. The gate below is kept wired into every generation
// handler so we can harden it server-side later (drserver#65) by flipping
// this to true — at which point clients that never call
// POST /agent/acknowledgment will start getting 403 ai_ack_required.
var aiAckEnforced = false

// SetAIAckEnforcedForTest toggles the acknowledgment gate and returns a
// cleanup hook that restores the previous value. ONLY for tests, so the
// gate's behavior stays covered while it is disabled in production.
func SetAIAckEnforcedForTest(enforced bool) func() {
	orig := aiAckEnforced
	aiAckEnforced = enforced
	return func() { aiAckEnforced = orig }
}

// requireAIAck gates AI generation behind the acknowledgment (drserver#65).
// Returns true when the caller may proceed; otherwise writes a 403 with a
// machine-readable code so the client can present the acknowledgment flow.
func requireAIAck(c *gin.Context) bool {
	if !aiAckEnforced {
		return true
	}

	userID := c.GetString("user_id")

	accepted, err := database.HasAcceptedAIAck(userID, types.CurrentAIAckVersion)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return false
	}
	if !accepted {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":            "ai_ack_required",
			"message":         "Accept the AI & professional advice acknowledgment before generating.",
			"current_version": types.CurrentAIAckVersion,
		})
		return false
	}
	return true
}
