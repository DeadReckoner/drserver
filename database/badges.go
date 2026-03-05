package database

import (
	"context"
	"time"

	"dr/server/log"
	"dr/server/types"
)

// CalculateBadgeCount returns the total badge count for a user.
// This includes:
// - Unread messages
// - Incomplete habits due today
// - Incomplete tasks due today
// - Pending incoming friend requests
// - Pending incoming anchor requests
func CalculateBadgeCount(ctx context.Context, userID string) int {
	total := 0

	// 1. Unread messages
	unreadMessages := countUnreadMessages(ctx, userID)
	total += unreadMessages

	// 2. Incomplete habits due today
	incompleteHabits := countIncompleteHabitsToday(ctx, userID)
	total += incompleteHabits

	// 3. Incomplete tasks due today
	incompleteTasks := countIncompleteTasksToday(ctx, userID)
	total += incompleteTasks

	// 4. Pending friend requests (where user is the addressee)
	pendingFriendRequests := countPendingFriendRequests(ctx, userID)
	total += pendingFriendRequests

	// 5. Pending anchor requests (where user is the anchor)
	pendingAnchorRequests := countPendingAnchorRequests(ctx, userID)
	total += pendingAnchorRequests

	log.Info(ctx, "Badge count calculated",
		"user_id", userID,
		"unread_messages", unreadMessages,
		"incomplete_habits", incompleteHabits,
		"incomplete_tasks", incompleteTasks,
		"pending_friend_requests", pendingFriendRequests,
		"pending_anchor_requests", pendingAnchorRequests,
		"total", total,
	)

	return total
}

// countUnreadMessages counts all unread messages for a user across all threads
func countUnreadMessages(ctx context.Context, userID string) int {
	var count int64

	err := db.Model(&types.Messages{}).
		Joins("INNER JOIN thread_participants tp ON messages.thread_id = tp.thread_id").
		Where("tp.user_id = ? AND tp.left_at IS NULL AND messages.user_id != ? AND messages.read = ? AND messages.deleted_at IS NULL",
			userID, userID, false).
		Count(&count).Error

	if err != nil {
		log.Error(ctx, "Failed to count unread messages", err)
		return 0
	}

	return int(count)
}

// countIncompleteHabitsToday counts habits due today that haven't been completed
func countIncompleteHabitsToday(ctx context.Context, userID string) int {
	today := time.Now().UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	// Get all active habits for user
	var habits []types.Habit
	err := db.Where("user_id = ? AND archived = ?", userID, false).Find(&habits).Error
	if err != nil {
		log.Error(ctx, "Failed to fetch habits for badge count", err)
		return 0
	}

	// Get today's completions for this user
	var completions []types.HabitCompletion
	err = db.Where("user_id = ? AND date = ?", userID, today).Find(&completions).Error
	if err != nil {
		log.Error(ctx, "Failed to fetch habit completions for badge count", err)
		return 0
	}

	// Build set of completed habit IDs
	completedSet := make(map[string]struct{}, len(completions))
	for _, c := range completions {
		completedSet[c.HabitID] = struct{}{}
	}

	// Count habits due today that aren't completed
	incompleteCount := 0
	for _, habit := range habits {
		if habit.IsDueOn(today) {
			if _, completed := completedSet[habit.HabitID]; !completed {
				incompleteCount++
			}
		}
	}

	return incompleteCount
}

// countIncompleteTasksToday counts tasks scheduled for today that aren't completed
func countIncompleteTasksToday(ctx context.Context, userID string) int {
	today := time.Now().UTC()
	startOfDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := startOfDay.Add(24 * time.Hour)

	var count int64
	err := db.Model(&types.Task{}).
		Where("user_id = ? AND completed = ? AND date >= ? AND date < ?",
			userID, false, startOfDay, endOfDay).
		Count(&count).Error

	if err != nil {
		log.Error(ctx, "Failed to count incomplete tasks", err)
		return 0
	}

	return int(count)
}

// countPendingFriendRequests counts pending friend requests where user is the addressee
func countPendingFriendRequests(ctx context.Context, userID string) int {
	var count int64
	err := db.Model(&types.Friendship{}).
		Where("addressee_id = ? AND status = ?", userID, "pending").
		Count(&count).Error

	if err != nil {
		log.Error(ctx, "Failed to count pending friend requests", err)
		return 0
	}

	return int(count)
}

// countPendingAnchorRequests counts pending anchor requests where user is the anchor
func countPendingAnchorRequests(ctx context.Context, userID string) int {
	var count int64
	err := db.Model(&types.AnchorRequest{}).
		Where("anchor_id = ? AND status = ?", userID, "pending").
		Count(&count).Error

	if err != nil {
		log.Error(ctx, "Failed to count pending anchor requests", err)
		return 0
	}

	return int(count)
}
