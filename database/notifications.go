package database

import (
	"context"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Push token management
func CreateOrUpdatePushToken(ctx context.Context, token *types.PushToken) error {
	// Try to find existing token by expo_token (which has unique constraint)
	var existing types.PushToken
	result := db.Where("expo_token = ?", token.ExpoToken).First(&existing)

	if result.Error == gorm.ErrRecordNotFound {
		// Create new token
		token.TokenID = uuid.NewString()
		token.CreatedAt = time.Now()
		token.LastUsedAt = time.Now()
		token.IsActive = true
		return db.Create(token).Error
	} else if result.Error != nil {
		return result.Error
	}

	// Update existing token (refresh platform, device_id, last_used_at, reactivate if needed)
	existing.Platform = token.Platform
	existing.DeviceID = token.DeviceID
	existing.IsActive = true
	existing.LastUsedAt = time.Now()
	return db.Save(&existing).Error
}

func GetActivePushTokens(ctx context.Context, userID string) []types.PushToken {
	var tokens []types.PushToken
	db.Where("user_id = ? AND is_active = ?", userID, true).Find(&tokens)
	return tokens
}

func DeactivatePushToken(ctx context.Context, expoToken string) error {
	return db.Model(&types.PushToken{}).
		Where("expo_token = ?", expoToken).
		Update("is_active", false).Error
}

// Notification management
func CreateNotification(ctx context.Context, notif *types.Notification) error {
	return db.Create(notif).Error
}

func GetUnreadNotifications(c *gin.Context, userID string) []types.Notification {
	var notifs []types.Notification
	db.Where("user_id = ? AND read_at IS NULL", userID).
		Order("created_at DESC").
		Find(&notifs)
	return notifs
}

func MarkNotificationRead(ctx context.Context, notifID string) error {
	now := time.Now()
	return db.Model(&types.Notification{}).
		Where("notification_id = ?", notifID).
		Update("read_at", now).Error
}

func UpdateNotificationStatus(ctx context.Context, notif *types.Notification, updates map[string]any) error {
	return db.Model(&types.Notification{}).
		Where("notification_id = ?", notif.NotificationID).
		Updates(updates).Error
}
