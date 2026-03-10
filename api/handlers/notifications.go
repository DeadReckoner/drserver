package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/types"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetNotifications returns all notifications for the current user
// Supports ?unread=true query parameter to filter to only unread notifications
// Supports ?include_cleared=true query parameter to include cleared notifications
func GetNotifications(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	unreadOnly := c.Query("unread") == "true"
	includeCleared := c.Query("include_cleared") == "true"

	notifications := database.GetNotifications(c, userID.(string), unreadOnly, includeCleared)
	utils.DataResponse(c, http.StatusOK, notifications)
}

// ClearNotifications marks all non-request notifications as cleared for the current user.
// Also cleans up any orphaned request notifications where the underlying request
// no longer exists or is no longer pending.
func ClearNotifications(c *gin.Context) {
	ctx := c.Request.Context()
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	// First, clean up any orphaned request notifications
	orphanedCount, err := database.CleanupOrphanedRequestNotifications(ctx, userID.(string))
	if err != nil {
		// Log error but continue with normal clear
		c.Error(err)
	}

	// Then clear all non-request notifications
	requestTypes := []string{"friend_request", "anchor_request", "habit_anchor_request"}

	if err := database.ClearNotifications(ctx, userID.(string), requestTypes); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":                   "Notifications cleared",
		"orphaned_requests_cleared": orphanedCount,
	})
}

// MarkNotificationRead marks a specific notification as read
func MarkNotificationRead(c *gin.Context) {
	ctx := c.Request.Context()
	notificationID := c.Param("alertid")
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	// Mark the notification as read
	if err := database.MarkNotificationRead(ctx, notificationID); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Notification marked as read",
	})
}

// DismissNotification dismisses a specific notification by ID.
// Unlike ClearNotifications which excludes request types, this endpoint
// allows dismissing any individual notification including orphaned request
// notifications where the underlying request is no longer valid.
func DismissNotification(c *gin.Context) {
	ctx := c.Request.Context()
	notificationID := c.Param("alertid")
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	if err := database.DismissNotification(ctx, userID.(string), notificationID); err != nil {
		if err.Error() == "record not found" {
			utils.NotFoundResponse(c, "Notification")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Notification dismissed",
	})
}

// RegisterPushToken registers or updates a push notification token for the user
func RegisterPushToken(c *gin.Context) {
	ctx := c.Request.Context()
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	var req struct {
		ExpoToken string `json:"expo_token" binding:"required"`
		Platform  string `json:"platform" binding:"required,oneof=ios android"`
		DeviceID  string `json:"device_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}

	token := &types.PushToken{
		TokenID:    uuid.NewString(),
		UserID:     userID.(string),
		ExpoToken:  req.ExpoToken,
		Platform:   req.Platform,
		DeviceID:   req.DeviceID,
		IsActive:   true,
		LastUsedAt: time.Now(),
		CreatedAt:  time.Now(),
	}

	if err := database.CreateOrUpdatePushToken(ctx, token); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Push token registered successfully",
		"token_id": token.TokenID,
	})
}

// DeactivatePushToken deactivates a push notification token
func DeactivatePushToken(c *gin.Context) {
	ctx := c.Request.Context()
	expoToken := c.Param("token")

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	if err := database.DeactivatePushToken(ctx, expoToken); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Push token deactivated successfully",
	})
}

// RefreshBadge calculates the current badge count and sends a silent push to update the app badge.
// This should be called when the user reads messages, completes tasks/habits, etc.
func RefreshBadge(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	// Calculate badge count
	badgeCount := utils.GetBadgeCount(userID.(string))

	// Send silent push to update badge on all devices
	if err := utils.SendBadgeUpdate(userID.(string)); err != nil {
		// Log the error but don't fail the request - badge count is still returned
		c.JSON(http.StatusOK, gin.H{
			"badge_count": badgeCount,
			"push_sent":   false,
			"message":     "Badge count calculated but push update failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"badge_count": badgeCount,
		"push_sent":   true,
	})
}

// GetBadgeCount returns the current badge count without sending a push notification.
// Useful for the mobile app to display badge count in the UI.
func GetBadgeCount(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	badgeCount := utils.GetBadgeCount(userID.(string))

	c.JSON(http.StatusOK, gin.H{
		"badge_count": badgeCount,
	})
}
