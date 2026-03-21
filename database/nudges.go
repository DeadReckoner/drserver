package database

import (
	"context"
	"time"

	"dr/server/log"
	"dr/server/types"

	"github.com/google/uuid"
)

// CheckNudgeRateLimit checks if the sender has exceeded the rate limit for nudging a target.
// Returns true if rate limit is exceeded (cannot send more nudges).
func CheckNudgeRateLimit(ctx context.Context, senderID, targetType, targetID string) (bool, error) {
	var count int64
	twentyFourHoursAgo := time.Now().Add(-24 * time.Hour)

	err := db.Model(&types.Nudge{}).
		Where("sender_id = ? AND target_type = ? AND target_id = ? AND created_at > ?",
			senderID, targetType, targetID, twentyFourHoursAgo).
		Count(&count).Error

	if err != nil {
		log.Error(ctx, "Failed to check nudge rate limit", err)
		return false, err
	}

	return count >= types.NudgeRateLimitPerDay, nil
}

// CreateNudge creates a new nudge record after verifying rate limits.
// Returns ErrNudgeRateLimitExceeded if the rate limit is exceeded.
func CreateNudge(ctx context.Context, senderID, receiverID, targetType, targetID string) (*types.Nudge, error) {
	// Check rate limit first
	exceeded, err := CheckNudgeRateLimit(ctx, senderID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	if exceeded {
		return nil, types.ErrNudgeRateLimitExceeded
	}

	nudge := &types.Nudge{
		NudgeID:    uuid.NewString(),
		SenderID:   senderID,
		ReceiverID: receiverID,
		TargetType: targetType,
		TargetID:   targetID,
		CreatedAt:  time.Now(),
	}

	if err := db.Create(nudge).Error; err != nil {
		log.Error(ctx, "Failed to create nudge", err)
		return nil, err
	}

	log.Info(ctx, "Nudge created", "nudge_id", nudge.NudgeID, "sender", senderID, "receiver", receiverID, "target_type", targetType)
	return nudge, nil
}

// GetNudgesReceived returns nudges received by a user, with sender info and target titles.
func GetNudgesReceived(ctx context.Context, userID string, limit int) ([]types.NudgeResponse, error) {
	if limit <= 0 {
		limit = 20
	}

	var nudges []types.Nudge
	err := db.Where("receiver_id = ?", userID).
		Preload("Sender").
		Order("created_at DESC").
		Limit(limit).
		Find(&nudges).Error

	if err != nil {
		log.Error(ctx, "Failed to get received nudges", err)
		return nil, err
	}

	return enrichNudgesWithTargetTitles(ctx, nudges)
}

// GetNudgesSent returns nudges sent by a user, with receiver info and target titles.
func GetNudgesSent(ctx context.Context, userID string, limit int) ([]types.NudgeResponse, error) {
	if limit <= 0 {
		limit = 20
	}

	var nudges []types.Nudge
	err := db.Where("sender_id = ?", userID).
		Preload("Receiver").
		Order("created_at DESC").
		Limit(limit).
		Find(&nudges).Error

	if err != nil {
		log.Error(ctx, "Failed to get sent nudges", err)
		return nil, err
	}

	return enrichNudgesWithTargetTitles(ctx, nudges)
}

// enrichNudgesWithTargetTitles adds target titles (roadmap/habit names) to nudge responses.
func enrichNudgesWithTargetTitles(ctx context.Context, nudges []types.Nudge) ([]types.NudgeResponse, error) {
	// Collect target IDs by type
	roadmapIDs := make([]string, 0)
	habitIDs := make([]string, 0)

	for _, n := range nudges {
		switch n.TargetType {
		case types.NudgeTargetTypeRoadmap:
			roadmapIDs = append(roadmapIDs, n.TargetID)
		case types.NudgeTargetTypeHabit:
			habitIDs = append(habitIDs, n.TargetID)
		}
	}

	// Batch fetch roadmap goals (used as titles)
	roadmapTitles := make(map[string]string)
	if len(roadmapIDs) > 0 {
		var roadmaps []types.Roadmap
		if err := db.Select("roadmap_id", "goal").Where("roadmap_id IN ?", roadmapIDs).Find(&roadmaps).Error; err != nil {
			log.Error(ctx, "Failed to fetch roadmap goals for nudges", err)
		} else {
			for _, r := range roadmaps {
				roadmapTitles[r.RoadmapID] = r.Goal
			}
		}
	}

	// Batch fetch habit titles
	habitTitles := make(map[string]string)
	if len(habitIDs) > 0 {
		var habits []types.Habit
		if err := db.Select("habit_id", "title").Where("habit_id IN ?", habitIDs).Find(&habits).Error; err != nil {
			log.Error(ctx, "Failed to fetch habit titles for nudges", err)
		} else {
			for _, h := range habits {
				habitTitles[h.HabitID] = h.Title
			}
		}
	}

	// Build response
	responses := make([]types.NudgeResponse, len(nudges))
	for i, n := range nudges {
		var title string
		switch n.TargetType {
		case types.NudgeTargetTypeRoadmap:
			title = roadmapTitles[n.TargetID]
		case types.NudgeTargetTypeHabit:
			title = habitTitles[n.TargetID]
		}

		responses[i] = types.NudgeResponse{
			NudgeID:     n.NudgeID,
			Sender:      n.Sender,
			Receiver:    n.Receiver,
			TargetType:  n.TargetType,
			TargetID:    n.TargetID,
			TargetTitle: title,
			CreatedAt:   n.CreatedAt,
		}
	}

	return responses, nil
}

// GetRoadmapForNudge retrieves a roadmap and verifies the caller is the anchor.
// Returns the roadmap if valid, or an error.
func GetRoadmapForNudge(ctx context.Context, roadmapID, callerID string) (*types.Roadmap, error) {
	var roadmap types.Roadmap
	err := db.Where("roadmap_id = ?", roadmapID).First(&roadmap).Error
	if err != nil {
		return nil, err
	}

	// Verify caller is the anchor
	if roadmap.AnchorID == nil || *roadmap.AnchorID != callerID {
		return nil, types.ErrNotAnchor
	}

	return &roadmap, nil
}

// GetHabitForNudge retrieves a habit and verifies the caller is the anchor.
// Returns the habit if valid, or an error.
func GetHabitForNudge(ctx context.Context, habitID, callerID string) (*types.Habit, error) {
	var habit types.Habit
	err := db.Where("habit_id = ?", habitID).First(&habit).Error
	if err != nil {
		return nil, err
	}

	// Verify caller is the anchor
	if habit.AnchorID == nil || *habit.AnchorID != callerID {
		return nil, types.ErrNotAnchor
	}

	return &habit, nil
}
