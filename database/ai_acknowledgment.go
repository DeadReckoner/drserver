package database

import (
	"errors"

	"dr/server/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetLatestAIAck returns the user's most recent AI-acknowledgment acceptance,
// or nil when they have never accepted any version.
func GetLatestAIAck(userID string) (*types.AIAcknowledgment, error) {
	var ack types.AIAcknowledgment
	err := db.Where("user_id = ?", userID).
		Order("version DESC").
		First(&ack).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ack, nil
}

// HasAcceptedAIAck reports whether the user has accepted the given
// acknowledgment version. Acceptance of an older version does not count —
// version bumps require re-acceptance (drserver#65).
func HasAcceptedAIAck(userID string, version int) (bool, error) {
	var count int64
	if err := db.Model(&types.AIAcknowledgment{}).
		Where("user_id = ? AND version = ?", userID, version).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// RecordAIAck stores the user's acceptance of an acknowledgment version.
// Idempotent: re-accepting the same version keeps the original accepted_at.
func RecordAIAck(userID string, version int, appVersion string) (types.AIAcknowledgment, error) {
	ack := types.AIAcknowledgment{
		UserID:     userID,
		Version:    version,
		AppVersion: appVersion,
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "version"}},
		DoNothing: true,
	}).Create(&ack).Error; err != nil {
		return types.AIAcknowledgment{}, err
	}

	// Re-read so the caller gets the canonical row (original accepted_at on
	// repeat acceptance rather than the zero value from DoNothing).
	var stored types.AIAcknowledgment
	if err := db.Where("user_id = ? AND version = ?", userID, version).
		First(&stored).Error; err != nil {
		return types.AIAcknowledgment{}, err
	}
	return stored, nil
}
