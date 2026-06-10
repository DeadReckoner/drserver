package database

import (
	"errors"

	"dr/server/types"

	"gorm.io/gorm"
)

// InsertEmailLog records one email send attempt. Caller fills in fields
// including timestamps; we just persist.
func InsertEmailLog(tx *gorm.DB, row *types.EmailLog) error {
	if tx == nil {
		tx = db
	}
	return tx.Create(row).Error
}

// EmailAlreadySent returns true when an email_log row already exists for
// (userID, kind, dedupeKey) with status='sent'. Used to short-circuit
// duplicate sends. dedupeKey may be empty to mean "any send of this kind".
func EmailAlreadySent(tx *gorm.DB, userID string, kind, dedupeKey string) (bool, error) {
	if tx == nil {
		tx = db
	}
	if userID == "" || kind == "" {
		return false, errors.New("user_id and kind required")
	}
	q := tx.Model(&types.EmailLog{}).
		Where("user_id = ? AND kind = ? AND status = ?", userID, kind, "sent")
	if dedupeKey != "" {
		q = q.Where("dedupe_key = ?", dedupeKey)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
