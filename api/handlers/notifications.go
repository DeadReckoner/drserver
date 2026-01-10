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
func GetNotifications(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	// Check for unread query parameter
	unreadOnly := c.Query("unread") == "true"

	notifications := database.GetNotifications(c, userID.(string), unreadOnly)
	utils.DataResponse(c, http.StatusOK, notifications)
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
