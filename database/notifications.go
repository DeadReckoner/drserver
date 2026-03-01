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

	// Update existing token (refresh user_id in case device changed hands, plus platform, device_id, last_used_at, reactivate if needed)
	existing.UserID = token.UserID
	existing.Platform = token.Platform
	existing.DeviceID = token.DeviceID
	existing.IsActive = true
	existing.LastUsedAt = time.Now()
	token.TokenID = existing.TokenID
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

func GetNotifications(c *gin.Context, userID string, unreadOnly bool, includeCleared bool) []types.Notification {
	var notifs []types.Notification
	query := db.Where("user_id = ?", userID)

	if unreadOnly {
		query = query.Where("read_at IS NULL")
	}

	if !includeCleared {
		query = query.Where("cleared_at IS NULL")
	}

	query.Order("created_at DESC").Find(&notifs)
	return notifs
}

func ClearNotifications(ctx context.Context, userID string, excludeTypes []string) error {
	now := time.Now()
	return db.Model(&types.Notification{}).
		Where("user_id = ? AND cleared_at IS NULL AND type NOT IN ?", userID, excludeTypes).
		Update("cleared_at", now).Error
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

// DeleteNotificationsByTypeAndData deletes notifications matching a type and data field
// This is used to clean up request notifications when requests are accepted/rejected
func DeleteNotificationsByTypeAndData(ctx context.Context, userID string, notifType string, dataKey string, dataValue string) error {
	// Use raw SQL with JSON query since GORM doesn't handle JSONB queries well
	return db.Exec(
		`DELETE FROM notifications WHERE user_id = ? AND type = ? AND data->>? = ?`,
		userID, notifType, dataKey, dataValue,
	).Error
}

// DeleteNotificationsByTypeAndMultipleDataKeys deletes notifications matching type and multiple data fields
func DeleteNotificationsByTypeAndMultipleDataKeys(ctx context.Context, userID string, notifType string, dataConditions map[string]string) error {
	query := db.Where("user_id = ? AND type = ?", userID, notifType)
	for key, value := range dataConditions {
		query = query.Where("data->>? = ?", key, value)
	}
	return query.Delete(&types.Notification{}).Error
}
