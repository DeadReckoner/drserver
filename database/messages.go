package database

import (
	"errors"
	"log"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateThread creates a new thread between two users and adds them as participants
func CreateThread(c *gin.Context, userID1, userID2 string) (types.Thread, error) {
	// Validate both users exist
	var user1, user2 types.User
	if err := db.Where("user_id = ?", userID1).First(&user1).Error; err != nil {
		return types.Thread{}, errors.New("user1 not found")
	}
	if err := db.Where("user_id = ?", userID2).First(&user2).Error; err != nil {
		return types.Thread{}, errors.New("user2 not found")
	}

	// Check if a thread already exists between these two users
	var existingThread types.Thread
	err := db.Raw(`
		SELECT t.* FROM threads t
		INNER JOIN thread_participants tp1 ON t.thread_id = tp1.thread_id AND tp1.user_id = ?
		INNER JOIN thread_participants tp2 ON t.thread_id = tp2.thread_id AND tp2.user_id = ?
		WHERE tp1.left_at IS NULL AND tp2.left_at IS NULL
		LIMIT 1
	`, userID1, userID2).Scan(&existingThread).Error

	if err == nil && existingThread.ThreadID != "" {
		// Thread already exists, return it
		return existingThread, nil
	}

	// Create new thread
	thread := types.Thread{
		ThreadID:  uuid.NewString(),
		Name:      "", // Direct messages don't need a name
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&thread).Error; err != nil {
		return types.Thread{}, err
	}

	// Add both users as participants
	participants := []types.ThreadParticipants{
		{
			ThreadPID: uuid.NewString(),
			ThreadID:  thread.ThreadID,
			UserID:    userID1,
			JoinedAt:  time.Now(),
			Role:      "member",
		},
		{
			ThreadPID: uuid.NewString(),
			ThreadID:  thread.ThreadID,
			UserID:    userID2,
			JoinedAt:  time.Now(),
			Role:      "member",
		},
	}

	if err := db.Create(&participants).Error; err != nil {
		// Rollback thread creation if participants fail
		db.Where("thread_id = ?", thread.ThreadID).Delete(&types.Thread{})
		return types.Thread{}, err
	}

	return thread, nil
}

// GetThreadBetweenUsers returns the thread_id between two users, or empty string if none exists
func GetThreadBetweenUsers(c *gin.Context, userID1, userID2 string) string {
	var participant types.ThreadParticipants
	err := db.Where("thread_participants.user_id = ? AND thread_participants.left_at IS NULL", userID1).
		Joins("JOIN thread_participants tp2 ON thread_participants.thread_id = tp2.thread_id AND tp2.user_id = ? AND tp2.left_at IS NULL", userID2).
		First(&participant).Error

	if err == nil {
		return participant.ThreadID
	}
	return ""
}

// GetThreadsByUser returns all threads for a given user
func GetThreadsByUser(c *gin.Context, userID string) ([]types.Thread, error) {
	var threads []types.Thread

	// Debug: Check if user has any thread_participants records
	var participantCount int64
	db.Model(&types.ThreadParticipants{}).Where("user_id = ?", userID).Count(&participantCount)
	log.Printf("DEBUG: User %s has %d thread_participants records", userID, participantCount)

	// Debug: Check thread_participants with left_at info
	var allParticipants []types.ThreadParticipants
	db.Where("user_id = ?", userID).Find(&allParticipants)
	for _, p := range allParticipants {
		log.Printf("DEBUG: Participant record - ThreadID: %s, LeftAt: %v", p.ThreadID, p.LeftAt)
	}

	// Get all threads where user is a participant and hasn't left
	err := db.Raw(`
		SELECT DISTINCT t.* FROM threads t
		INNER JOIN thread_participants tp ON t.thread_id = tp.thread_id
		WHERE tp.user_id = ? AND tp.left_at IS NULL
		ORDER BY t.updated_at DESC
	`, userID).Scan(&threads).Error

	if err != nil {
		return nil, err
	}

	log.Printf("DEBUG: Found %d threads for user %s", len(threads), userID)

	// Calculate unread counts for each thread
	for i := range threads {
		count, err := CalculateUnreadCount(c, threads[i].ThreadID, userID)
		if err != nil {
			log.Printf("Warning: Failed to calculate unread count for thread %s: %v", threads[i].ThreadID, err)
			count = 0
		}
		threads[i].TotalUnread = count

		// Update DB in background
		go func(threadID string, unreadCount int) {
			db.Model(&types.Thread{}).
				Where("thread_id = ?", threadID).
				Update("total_unread", unreadCount)
		}(threads[i].ThreadID, count)
	}

	return threads, nil
}

// VerifyThreadParticipant checks if a user is a participant of a thread
func VerifyThreadParticipant(c *gin.Context, threadID, userID string) error {
	var participant types.ThreadParticipants
	err := db.Where("thread_id = ? AND user_id = ? AND left_at IS NULL", threadID, userID).
		First(&participant).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("access denied: user is not a participant of this thread")
		}
		return err
	}
	return nil
}

// GetThreadMessages retrieves message history for a thread
// Verifies the user is a participant before returning messages
func GetThreadMessages(c *gin.Context, threadID, userID string) ([]types.MessageResponse, error) {
	// Verify user is a participant
	if err := VerifyThreadParticipant(c, threadID, userID); err != nil {
		return nil, err
	}

	// Get messages with user information
	var messages []types.Messages
	err := db.Where("thread_id = ? AND deleted_at IS NULL", threadID).
		Order("created_at ASC").
		Find(&messages).Error

	if err != nil {
		return nil, err
	}

	// Build response with user information
	var response []types.MessageResponse
	for _, msg := range messages {
		var user types.User
		if err := db.Where("user_id = ?", msg.UserID).First(&user).Error; err == nil {
			response = append(response, types.MessageResponse{
				MessageID: msg.MessageID,
				ThreadID:  msg.ThreadID,
				UserID:    msg.UserID,
				Content:   msg.Content,
				Read:      msg.Read,
				CreatedAt: msg.CreatedAt,
				User:      &user,
			})
		} else {
			// If user not found, still include message but without user object
			response = append(response, types.MessageResponse{
				MessageID: msg.MessageID,
				ThreadID:  msg.ThreadID,
				UserID:    msg.UserID,
				Content:   msg.Content,
				Read:      msg.Read,
				CreatedAt: msg.CreatedAt,
				User:      nil,
			})
		}
	}

	// Mark messages as read after retrieving them
	if err := MarkMessagesAsRead(c, threadID, userID); err != nil {
		log.Printf("Warning: Failed to mark messages as read for thread %s, user %s: %v", threadID, userID, err)
	}

	return response, nil
}

// MarkMessagesAsRead marks all messages from OTHER users as read in a thread
func MarkMessagesAsRead(c *gin.Context, threadID, userID string) error {
	result := db.Model(&types.Messages{}).
		Where("thread_id = ? AND user_id != ? AND read = ?", threadID, userID, false).
		Update("read", true)

	if result.Error != nil {
		return result.Error
	}

	log.Printf("Marked %d messages as read in thread %s for user %s", result.RowsAffected, threadID, userID)
	return nil
}

// CalculateUnreadCount counts unread messages from other users in a thread
func CalculateUnreadCount(c *gin.Context, threadID, userID string) (int, error) {
	var count int64

	err := db.Model(&types.Messages{}).
		Where("thread_id = ? AND user_id != ? AND read = ? AND deleted_at IS NULL",
			threadID, userID, false).
		Count(&count).Error

	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// CalculateTotalUnreadCount counts all unread messages from other users across all threads
func CalculateTotalUnreadCount(c *gin.Context, userID string) (int, error) {
	var count int64

	err := db.Model(&types.Messages{}).
		Joins("INNER JOIN thread_participants tp ON messages.thread_id = tp.thread_id").
		Where("tp.user_id = ? AND tp.left_at IS NULL AND messages.user_id != ? AND messages.read = ? AND messages.deleted_at IS NULL",
			userID, userID, false).
		Count(&count).Error

	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// CreateMessage saves a new message to the database
func CreateMessage(c *gin.Context, threadID, userID, content string) (types.Messages, error) {
	// Verify user is a participant
	if err := VerifyThreadParticipant(c, threadID, userID); err != nil {
		return types.Messages{}, err
	}

	// Create message
	message := types.Messages{
		MessageID: uuid.NewString(),
		ThreadID:  threadID,
		UserID:    userID,
		Content:   content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&message).Error; err != nil {
		return types.Messages{}, err
	}

	// Update thread's updated_at timestamp
	db.Model(&types.Thread{}).Where("thread_id = ?", threadID).Update("updated_at", time.Now())

	// Update last sent message
	if err := updateLastSent(threadID, &message.MessageID); err != nil {
		log.Printf("Warning: Failed to update last_sent_message for thread %s: %v", threadID, err)
	}

	return message, nil
}

// GetThreadParticipants returns all participants of a thread
func GetThreadParticipants(c *gin.Context, threadID string) ([]types.User, error) {
	var users []types.User

	err := db.Raw(`
		SELECT u.* FROM users u
		INNER JOIN thread_participants tp ON u.user_id = tp.user_id
		WHERE tp.thread_id = ? AND tp.left_at IS NULL
	`, threadID).Scan(&users).Error

	if err != nil {
		return nil, err
	}

	return users, nil
}

func updateLastSent(threadID string, messageID *string) error {
	return db.Model(&types.Thread{}).
		Where("thread_id = ?", threadID).
		Update("last_sent_message", messageID).Error
}

// GetMessageByID retrieves a single message by its ID
// Verifies the user is a participant of the thread before returning the message
func GetMessageByID(c *gin.Context, messageID, userID string) (*types.MessageResponse, error) {
	// First get the message to find which thread it belongs to
	var message types.Messages
	err := db.Where("message_id = ? AND deleted_at IS NULL", messageID).
		First(&message).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.New("message not found")
		}
		return nil, err
	}

	// Verify user is a participant of the thread
	if err := VerifyThreadParticipant(c, message.ThreadID, userID); err != nil {
		return nil, err
	}

	// Get user information
	var user types.User
	var response types.MessageResponse

	if err := db.Where("user_id = ?", message.UserID).First(&user).Error; err == nil {
		response = types.MessageResponse{
			MessageID: message.MessageID,
			ThreadID:  message.ThreadID,
			UserID:    message.UserID,
			Content:   message.Content,
			Read:      message.Read,
			CreatedAt: message.CreatedAt,
			User:      &user,
		}
	} else {
		// If user not found, still return message but without user object
		response = types.MessageResponse{
			MessageID: message.MessageID,
			ThreadID:  message.ThreadID,
			UserID:    message.UserID,
			Content:   message.Content,
			Read:      message.Read,
			CreatedAt: message.CreatedAt,
			User:      nil,
		}
	}

	return &response, nil
}