package utils

import (
	"bytes"
	"context"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SendPushNotification creates a notification record and sends push notifications to all active devices
func SendPushNotification(userID, notificationType, title, body string, data map[string]any) {
	// Create context with logger using userID as correlation ID
	ctx := log.WithLogger(context.Background(), log.NewLogger(userID))

	tokens := database.GetActivePushTokens(ctx, userID)
	if len(tokens) == 0 {
		log.Info(ctx, "No active push tokens", userID)
		return
	}

	alert := types.Notification{
		NotificationID: uuid.NewString(),
		UserID:         userID,
		Type:           notificationType,
		Title:          title,
		Body:           body,
		Data:           data,
		Status:         string(types.NotificationStatusPending),
	}

	database.CreateNotification(ctx, &alert)

	for _, token := range tokens {
		// Pass the notification ID as correlation ID for tracking
		notifCtx := log.WithLogger(context.Background(), log.NewLogger(alert.NotificationID))
		go sendExpoNotification(notifCtx, token.ExpoToken, notificationType, title, body, data, alert.NotificationID)
	}
}

// sendExpoNotification sends a push notification via Expo's push service
func sendExpoNotification(ctx context.Context, expoToken, notificationType, title, body string, data map[string]any, notificationID string) {

	// Ensure data map exists and add type field
	if data == nil {
		data = make(map[string]any)
	}
	data["type"] = notificationType

	// Prepare Expo Push notification payload
	pushMessage := map[string]any{
		"to":       expoToken,
		"title":    title,
		"body":     body,
		"data":     data,
		"sound":    "default",
		"priority": "high",
	}

	payload, err := json.Marshal(pushMessage)
	if err != nil {
		log.Info(ctx, "Failed to marshal push notification payload", err)
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		return
	}

	// Send to Expo Push API
	resp, err := http.Post(
		"https://exp.host/--/api/v2/push/send",
		"application/json",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		log.Info(ctx, "Failed to send push notification", err)
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		return
	}
	defer resp.Body.Close()

	// Read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Info(ctx, "Failed to read push notification response", err)
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		return
	}

	// Check if successful
	if resp.StatusCode != http.StatusOK {
		log.Info(ctx, "Expo push notification failed", string(responseBody))
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), string(responseBody))
		return
	}

	// Parse response to check for errors
	var expoResponse struct {
		Data []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"data"`
	}

	if err := json.Unmarshal(responseBody, &expoResponse); err == nil {
		if len(expoResponse.Data) > 0 && expoResponse.Data[0].Status == "error" {
			log.Info(ctx, "Expo returned error", expoResponse.Data[0].Message)
			updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), expoResponse.Data[0].Message)
			return
		}
	}

	// Success!
	updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusSent), "")
	log.Info(ctx, "Push notification sent successfully", notificationID)
}

// updateNotificationStatus updates the status of a notification in the database
func updateNotificationStatus(ctx context.Context, notificationID, status, errorMsg string) {
	updates := map[string]any{
		"status": status,
	}

	if status == string(types.NotificationStatusSent) {
		now := time.Now()
		updates["sent_at"] = now
	}

	if errorMsg != "" {
		updates["error"] = errorMsg
	}

	// Update notification status in database
	notification := types.Notification{
		NotificationID: notificationID,
	}

	if err := database.UpdateNotificationStatus(ctx, &notification, updates); err != nil {
		log.Info(ctx, "Failed to update notification status", err)
	}
}
