package utils

import (
	"bytes"
	"context"
	"dr/server/analytics"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SendPushNotification creates a notification record and sends push notifications to all active devices
func SendPushNotification(userID, notificationType, title, body string, data map[string]any) {
	// Create context with logger using userID as correlation ID
	ctx := log.WithLogger(context.Background(), log.NewLogger(userID))

	// Record the in-app notification FIRST, independent of push delivery. The
	// Notifications screen reads the notifications table, so it must be written
	// even when the user has no active push token (web-only, push permission
	// not granted, or token not yet registered). Previously this lived after
	// the token check below, so a recipient without a registered device got the
	// underlying event row (e.g. a pending friendship) but nothing in their
	// Notifications list.
	alert := types.Notification{
		NotificationID: uuid.NewString(),
		UserID:         userID,
		Type:           notificationType,
		Title:          title,
		Body:           body,
		Data:           data,
		Status:         string(types.NotificationStatusPending),
	}
	if err := database.CreateNotification(ctx, &alert); err != nil {
		log.Error(ctx, "Failed to record in-app notification", err)
	}

	tokens := database.GetActivePushTokens(ctx, userID)
	if len(tokens) == 0 {
		log.Info(ctx, "No active push tokens; recorded in-app notification, skipping push", userID)
		return
	}

	// Calculate badge count for the user
	badgeCount := database.CalculateBadgeCount(ctx, userID)

	for _, token := range tokens {
		// Pass the notification ID as correlation ID for tracking
		notifCtx := log.WithLogger(context.Background(), log.NewLogger(alert.NotificationID))
		notifCtx = analytics.WithUserID(notifCtx, userID)
		go sendExpoNotification(notifCtx, token.ExpoToken, notificationType, title, body, data, alert.NotificationID, badgeCount)
	}
}

// accountabilityTypes are the notification types that constitute an
// accountability alert — the ones an anchor relationship exists to produce.
//
// Tracked separately from ordinary notifications because the CEO scorecard
// asks a question no aggregate delivery rate can answer: whether being held
// accountable actually changes behavior. That needs the alerts to be
// distinguishable from friend requests and digests.
var accountabilityTypes = map[string]struct{}{
	"missed_task":       {},
	"falling_behind":    {},
	"milestone_at_risk": {},
	"roadmap_nudge":     {},
	"habit_nudge":       {},
}

// emitNotificationOutcome records the result of a push delivery attempt.
//
// "Delivered" here means Expo accepted the message, which is the furthest
// point our server can observe. Whether the device rendered it, and whether
// the user saw it, are only knowable from the client — notification_opened
// comes from there. Conflating the two would overstate reach.
func emitNotificationOutcome(ctx context.Context, notificationType, notificationID, errCode string, err error) {
	delivered := err == nil && errCode == ""

	props := analytics.Props(
		"notification_type", notificationType,
		"notification_channel", "push",
		"work_item_id", notificationID,
	)
	name := analytics.EventNotificationDelivered
	if !delivered {
		name = analytics.EventNotificationFailed
		props["error_code"] = errCode
	}
	analytics.Emit(ctx, name, props)

	if _, ok := accountabilityTypes[notificationType]; !ok {
		return
	}
	alertName := analytics.EventAccountabilityAlertDelivered
	if !delivered {
		alertName = analytics.EventAccountabilityAlertFailed
	}
	analytics.Emit(ctx, alertName, props)
}

// sendExpoNotification sends a push notification via Expo's push service
func sendExpoNotification(ctx context.Context, expoToken, notificationType, title, body string, data map[string]any, notificationID string, badgeCount int) {

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
		"badge":    badgeCount,
	}

	payload, err := json.Marshal(pushMessage)
	if err != nil {
		log.Failure(ctx, log.FailureNotification, log.CodeInvalidRequest, err,
			slog.String("notification_type", notificationType))
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		emitNotificationOutcome(ctx, notificationType, notificationID, log.CodeInvalidRequest, err)
		return
	}

	// Send to Expo Push API
	resp, err := http.Post(
		"https://exp.host/--/api/v2/push/send",
		"application/json",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		code := log.ErrorCode(err)
		log.Failure(ctx, log.FailureNotification, code, err,
			slog.String("notification_type", notificationType))
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		emitNotificationOutcome(ctx, notificationType, notificationID, code, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		code := log.ErrorCode(err)
		log.Failure(ctx, log.FailureNotification, code, err,
			slog.String("notification_type", notificationType))
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		emitNotificationOutcome(ctx, notificationType, notificationID, code, err)
		return
	}

	// Check if successful
	if resp.StatusCode != http.StatusOK {
		log.Failure(ctx, log.FailureNotification, log.CodeUnavailable, nil,
			slog.String("notification_type", notificationType),
			slog.Int("provider_status", resp.StatusCode))
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), string(responseBody))
		emitNotificationOutcome(ctx, notificationType, notificationID, log.CodeUnavailable, nil)
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
			log.Failure(ctx, log.FailureNotification, log.CodeInvalidRequest, nil,
				slog.String("notification_type", notificationType),
				slog.String("provider_message", expoResponse.Data[0].Message))
			updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), expoResponse.Data[0].Message)
			emitNotificationOutcome(ctx, notificationType, notificationID, log.CodeInvalidRequest, nil)
			return
		}
	}

	// Success!
	updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusSent), "")
	emitNotificationOutcome(ctx, notificationType, notificationID, "", nil)
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

// SendBadgeUpdate sends a silent push notification to update only the app badge count.
// This is used when the user reads messages, completes tasks, etc. to refresh the badge.
func SendBadgeUpdate(userID string) error {
	ctx := log.WithLogger(context.Background(), log.NewLogger(userID))

	tokens := database.GetActivePushTokens(ctx, userID)
	if len(tokens) == 0 {
		log.Info(ctx, "No active push tokens for badge update", userID)
		return nil
	}

	// Calculate current badge count
	badgeCount := database.CalculateBadgeCount(ctx, userID)

	log.Info(ctx, "Sending badge update", "user_id", userID, "badge_count", badgeCount)

	var lastError error
	for _, token := range tokens {
		if err := sendBadgeOnlyNotification(ctx, token.ExpoToken, badgeCount); err != nil {
			lastError = err
			log.Error(ctx, "Failed to send badge update to token", err, "token", token.ExpoToken)
		}
	}

	return lastError
}

// sendBadgeOnlyNotification sends a content-available push that only updates the badge
func sendBadgeOnlyNotification(ctx context.Context, expoToken string, badgeCount int) error {
	// Expo silent push with badge update
	// Setting _contentAvailable: true makes it a background/silent notification
	pushMessage := map[string]any{
		"to":                expoToken,
		"badge":             badgeCount,
		"_contentAvailable": true,
	}

	payload, err := json.Marshal(pushMessage)
	if err != nil {
		return err
	}

	resp, err := http.Post(
		"https://exp.host/--/api/v2/push/send",
		"application/json",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Info(ctx, "Badge update push failed", string(body))
	}

	return nil
}

// GetBadgeCount returns the current badge count for a user without sending a notification
func GetBadgeCount(userID string) int {
	ctx := log.WithLogger(context.Background(), log.NewLogger(userID))
	return database.CalculateBadgeCount(ctx, userID)
}
