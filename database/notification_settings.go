package database

import (
	"errors"
	"time"

	"dr/server/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// =============================================================================
// notification_settings
// =============================================================================

// GetNotificationSettings returns the user's settings row, or
// gorm.ErrRecordNotFound if absent. In practice the row exists because the
// on_user_created_create_notification_settings trigger seeds it on user insert;
// callers that want to defend against a missing row should use
// EnsureNotificationSettings instead.
func GetNotificationSettings(userID string) (types.NotificationSettings, error) {
	var settings types.NotificationSettings
	err := db.Where("user_id = ?", userID).First(&settings).Error
	return settings, err
}

// EnsureNotificationSettings returns the user's settings, creating a default
// row if absent. Safe to call on every settings access in case the trigger
// ever fails or a user is created out-of-band.
func EnsureNotificationSettings(userID string) (types.NotificationSettings, error) {
	settings, err := GetNotificationSettings(userID)
	if err == nil {
		return settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return types.NotificationSettings{}, err
	}
	// Build the in-memory default. The DB also has DEFAULTs on every column,
	// but populating the struct lets us return a usable value without a
	// follow-up read.
	settings = types.NotificationSettings{
		UserID:                  userID,
		PushEnabled:             true,
		EmailEnabled:            true,
		QuietHoursEnabled:       false,
		QuietHoursStart:         "22:00",
		QuietHoursEnd:           "08:00",
		DigestFrequency:         types.DigestFrequencyNone,
		DigestDeliveryHour:      8,
		DigestDeliveryDayOfWeek: 1,
		MyGoalsPrefs:            types.DefaultMyGoalsPrefs(),
		FAndAEnabled:            true,
		ApplyToAllFriends:       true,
		FAndAPrefs:              types.DefaultFAndAPrefs(),
	}
	if err := db.Create(&settings).Error; err != nil {
		return types.NotificationSettings{}, err
	}
	return settings, nil
}

// UpdateNotificationSettings applies a partial update via GORM's struct-based
// Updates (which skips zero values). For boolean fields like PushEnabled where
// false is a legitimate value to set, callers should use UpdateNotificationSettingsMap.
func UpdateNotificationSettings(userID string, patch types.NotificationSettings) error {
	return db.Model(&types.NotificationSettings{}).
		Where("user_id = ?", userID).
		Updates(patch).Error
}

// UpdateNotificationSettingsMap applies a partial update from a column->value
// map. Use this when the caller needs to set boolean fields to false (which
// GORM's struct-Updates would skip as a zero value).
func UpdateNotificationSettingsMap(userID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	patch["updated_at"] = time.Now()
	return db.Model(&types.NotificationSettings{}).
		Where("user_id = ?", userID).
		Updates(patch).Error
}

// =============================================================================
// notification_overrides
// =============================================================================

// ListNotificationOverrides returns all overrides owned by the user.
func ListNotificationOverrides(userID string) ([]types.NotificationOverride, error) {
	var overrides []types.NotificationOverride
	err := db.Where("user_id = ?", userID).Find(&overrides).Error
	return overrides, err
}

// GetFriendOverride returns the override for (userID, friendID), or nil if absent.
func GetFriendOverride(userID, friendID string) (*types.NotificationOverride, error) {
	var o types.NotificationOverride
	err := db.Where("user_id = ? AND friend_id = ?", userID, friendID).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// GetAnchoredGoalOverride returns the override for (userID, anchoredRoadmapID),
// or nil if absent.
func GetAnchoredGoalOverride(userID, anchoredRoadmapID string) (*types.NotificationOverride, error) {
	var o types.NotificationOverride
	err := db.Where("user_id = ? AND anchored_roadmap_id = ?", userID, anchoredRoadmapID).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// UpsertFriendOverride creates or updates the override for a friend.
func UpsertFriendOverride(o types.NotificationOverride) error {
	if o.FriendID == nil || o.AnchoredRoadmapID != nil {
		return errors.New("UpsertFriendOverride: FriendID must be set and AnchoredRoadmapID must be nil")
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "friend_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"prefs", "mute_until", "updated_at"}),
	}).Create(&o).Error
}

// UpsertAnchoredGoalOverride creates or updates the override for an anchored goal.
func UpsertAnchoredGoalOverride(o types.NotificationOverride) error {
	if o.AnchoredRoadmapID == nil || o.FriendID != nil {
		return errors.New("UpsertAnchoredGoalOverride: AnchoredRoadmapID must be set and FriendID must be nil")
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "anchored_roadmap_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"prefs", "mute_until", "updated_at"}),
	}).Create(&o).Error
}

// DeleteFriendOverride removes the override for (userID, friendID) if present.
func DeleteFriendOverride(userID, friendID string) error {
	return db.Where("user_id = ? AND friend_id = ?", userID, friendID).
		Delete(&types.NotificationOverride{}).Error
}

// DeleteAnchoredGoalOverride removes the override for (userID, anchoredRoadmapID)
// if present.
func DeleteAnchoredGoalOverride(userID, anchoredRoadmapID string) error {
	return db.Where("user_id = ? AND anchored_roadmap_id = ?", userID, anchoredRoadmapID).
		Delete(&types.NotificationOverride{}).Error
}

// =============================================================================
// notification_delivery_log
// =============================================================================

// RecordDelivery inserts an entry into the delivery log if one does not already
// exist for (userID, notificationType, dedupKey, channel).
//
// Returns (true, nil) if a row was inserted — the caller should proceed to
// send the notification. Returns (false, nil) if a row already existed —
// the caller should skip (already sent).
//
// This is the idempotent-send primitive used throughout the scheduler.
// Wrap RecordDelivery + the actual send in a single transaction so a send
// failure rolls back the log row and the next scan retries naturally.
func RecordDelivery(userID, notificationType, dedupKey, channel string) (inserted bool, err error) {
	entry := types.NotificationDeliveryLog{
		UserID:           userID,
		NotificationType: notificationType,
		DedupKey:         dedupKey,
		Channel:          channel,
	}
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// RecordDeliveryAndSend wraps RecordDelivery + a send callback in a single
// transaction. If the send returns an error, the delivery log row is rolled
// back, so the next scan retries naturally. If the row already exists (the
// ON CONFLICT path), the send is skipped silently.
//
// Returns (sent, err) where sent=true means a fresh row was inserted AND
// the send callback returned without error.
//
// Use this from cron jobs in Steps 7–11 where the send is synchronous and
// failure should leave the dedup slot reclaimable. For the fire-and-forget
// Expo push path in notifications.Dispatch, the simpler RecordDelivery
// pattern is correct because the Expo client returns no error to the caller.
func RecordDeliveryAndSend(userID, notificationType, dedupKey, channel string, send func() error) (sent bool, err error) {
	err = db.Transaction(func(tx *gorm.DB) error {
		entry := types.NotificationDeliveryLog{
			UserID:           userID,
			NotificationType: notificationType,
			DedupKey:         dedupKey,
			Channel:          channel,
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Already delivered (or claimed by a concurrent worker).
			// Not an error — the caller should treat this as a no-op.
			return nil
		}
		if sendErr := send(); sendErr != nil {
			// Rolling back the transaction drops the log row, so the next
			// scan will re-detect this candidate and try again.
			return sendErr
		}
		sent = true
		return nil
	})
	return
}

// RecentDeliveries returns the user's delivery log entries for a given type
// since the lookback point. Used by frequency-cap logic.
func RecentDeliveries(userID, notificationType string, since time.Time) ([]types.NotificationDeliveryLog, error) {
	var entries []types.NotificationDeliveryLog
	err := db.Where("user_id = ? AND notification_type = ? AND delivered_at >= ?", userID, notificationType, since).
		Order("delivered_at DESC").
		Find(&entries).Error
	return entries, err
}

// CleanupDeliveryLog deletes entries older than the cutoff. Called from the
// hourly retention cron in Step 12 of the plan. Returns the number of rows
// removed.
func CleanupDeliveryLog(cutoff time.Time) (int64, error) {
	res := db.Where("delivered_at < ?", cutoff).Delete(&types.NotificationDeliveryLog{})
	return res.RowsAffected, res.Error
}

// =============================================================================
// Authorization helpers (used by per-target override endpoints)
// =============================================================================

// IsAnchorOfRoadmap reports whether the given user is the accepted anchor of
// the given roadmap. Anchored-goal notification overrides may only be created
// for roadmaps where this returns true.
func IsAnchorOfRoadmap(anchorUserID, roadmapID string) (bool, error) {
	var count int64
	err := db.Model(&types.Roadmap{}).
		Where("roadmap_id = ? AND anchor_id = ?", roadmapID, anchorUserID).
		Count(&count).Error
	return count > 0, err
}
