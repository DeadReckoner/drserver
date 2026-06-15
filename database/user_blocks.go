package database

import (
	"errors"

	"dr/server/types"

	"gorm.io/gorm/clause"
)

// BlockExistsBetween reports whether a block exists in EITHER direction between
// the two users. A block is mutual in effect (neither can contact the other)
// even though only the blocker owns the row.
func BlockExistsBetween(userA, userB string) bool {
	var count int64
	db.Model(&types.UserBlock{}).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)",
			userA, userB, userB, userA).
		Count(&count)
	return count > 0
}

// CreateBlock records that blockerID has blocked blockedID. Idempotent: a
// repeat block is a no-op via the unique (blocker_id, blocked_id) constraint.
func CreateBlock(blockerID, blockedID string) error {
	if blockerID == blockedID {
		return errors.New("cannot block yourself")
	}
	block := types.UserBlock{BlockerID: blockerID, BlockedID: blockedID}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "blocker_id"}, {Name: "blocked_id"}},
		DoNothing: true,
	}).Create(&block).Error
}

// RemoveBlock lifts blockerID's block on blockedID. Only the blocker's own row
// is affected, so the blocked user can never unblock themselves.
func RemoveBlock(blockerID, blockedID string) error {
	return db.Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).
		Delete(&types.UserBlock{}).Error
}
