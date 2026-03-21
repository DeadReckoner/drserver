package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type NudgeHandler struct {
	db *gorm.DB
}

func NewNudgeHandler(db *gorm.DB) *NudgeHandler {
	return &NudgeHandler{db: db}
}

// NudgeRoadmap sends a nudge to the owner of a roadmap.
// The caller must be the anchor for this roadmap.
func (h *NudgeHandler) NudgeRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}
	callerID := userID.(string)

	// Get roadmap and verify caller is the anchor
	roadmap, err := database.GetRoadmapForNudge(ctx, roadmapID, callerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errors.Is(err, types.ErrNotAnchor) {
			utils.ForbiddenResponse(c, "You are not the anchor for this roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	// Create the nudge (handles rate limiting internally)
	nudge, err := database.CreateNudge(ctx, callerID, roadmap.UserID, types.NudgeTargetTypeRoadmap, roadmapID)
	if err != nil {
		if errors.Is(err, types.ErrNudgeRateLimitExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Max 2 nudges per day per roadmap.",
			})
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	// Get sender info for notification
	sender, _ := database.GetUserByID(callerID)
	senderName := sender.FirstName
	if senderName == "" {
		senderName = "Your accountability partner"
	}

	// Send push notification to roadmap owner
	go utils.SendPushNotification(
		roadmap.UserID,
		"roadmap_nudge",
		"Nudge from "+senderName,
		senderName+" nudged you on '"+roadmap.Goal+"'",
		map[string]any{
			"roadmap_id": roadmapID,
			"sender_id":  callerID,
			"nudge_id":   nudge.NudgeID,
		},
	)

	c.JSON(http.StatusOK, gin.H{
		"nudge_id": nudge.NudgeID,
		"message":  "Nudge sent successfully",
	})
}

// NudgeHabit sends a nudge to the owner of a habit.
// The caller must be the anchor for this habit.
func (h *NudgeHandler) NudgeHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}
	callerID := userID.(string)

	// Get habit and verify caller is the anchor
	habit, err := database.GetHabitForNudge(ctx, habitID, callerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.NotFoundResponse(c, "Habit")
			return
		}
		if errors.Is(err, types.ErrNotAnchor) {
			utils.ForbiddenResponse(c, "You are not the anchor for this habit")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	// Create the nudge (handles rate limiting internally)
	nudge, err := database.CreateNudge(ctx, callerID, habit.UserID, types.NudgeTargetTypeHabit, habitID)
	if err != nil {
		if errors.Is(err, types.ErrNudgeRateLimitExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Max 2 nudges per day per habit.",
			})
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	// Get sender info for notification
	sender, _ := database.GetUserByID(callerID)
	senderName := sender.FirstName
	if senderName == "" {
		senderName = "Your accountability partner"
	}

	// Send push notification to habit owner
	go utils.SendPushNotification(
		habit.UserID,
		"habit_nudge",
		"Nudge from "+senderName,
		senderName+" nudged you on '"+habit.Title+"'",
		map[string]any{
			"habit_id":  habitID,
			"sender_id": callerID,
			"nudge_id":  nudge.NudgeID,
		},
	)

	c.JSON(http.StatusOK, gin.H{
		"nudge_id": nudge.NudgeID,
		"message":  "Nudge sent successfully",
	})
}

// GetNudgesReceived returns nudges received by the current user.
// Supports ?limit=N query parameter (default 20).
func (h *NudgeHandler) GetNudgesReceived(c *gin.Context) {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	nudges, err := database.GetNudgesReceived(ctx, userID.(string), limit)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nudges": nudges,
	})
}

// GetNudgesSent returns nudges sent by the current user.
// Supports ?limit=N query parameter (default 20).
func (h *NudgeHandler) GetNudgesSent(c *gin.Context) {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	nudges, err := database.GetNudgesSent(ctx, userID.(string), limit)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nudges": nudges,
	})
}
