package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetNotifications returns all unread notifications for the current user
func GetNotifications(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	notifications := database.GetUnreadNotifications(c, userID.(string))
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
