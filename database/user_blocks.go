package database

import (
	"errors"

	"dr/server/types"

	"github.com/gin-gonic/gin"
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

// GetBlockedUsers returns the users the current user has blocked (blocker_id =
// me), newest first, with each blocked user's profile. Only the caller's own
// blocks are returned - a user can never see who has blocked them.
func GetBlockedUsers(c *gin.Context) ([]types.BlockedUserResponse, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return nil, errors.New("missing or invalid user_id")
	}
	me := userID.(string)

	var blocks []types.UserBlock
	if err := db.Where("blocker_id = ?", me).Order("created_at DESC").Find(&blocks).Error; err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return []types.BlockedUserResponse{}, nil
	}

	// Batch-load the blocked users' profiles in one query.
	ids := make([]string, 0, len(blocks))
	for _, b := range blocks {
		ids = append(ids, b.BlockedID)
	}
	var users []types.User
	if err := db.Where("user_id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	userMap := make(map[string]types.User, len(users))
	for _, u := range users {
		userMap[u.UserID] = u
	}

	resp := make([]types.BlockedUserResponse, 0, len(blocks))
	for _, b := range blocks {
		u, ok := userMap[b.BlockedID]
		if !ok {
			continue // blocked user no longer exists
		}
		resp = append(resp, types.BlockedUserResponse{
			User:      u,
			BlockedAt: b.CreatedAt,
		})
	}
	return resp, nil
}
