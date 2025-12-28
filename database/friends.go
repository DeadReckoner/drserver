package database

import (
	"errors"
	"log"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetFriendshipStatus returns the friendship status between two users
// Returns "accepted", "pending", or "none"
func GetFriendshipStatus(userID1, userID2 string) string {
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status IN (?, ?)",
		userID1, userID2, userID2, userID1, "pending", "accepted",
	).First(&friendship)

	if result.Error != nil {
		return "none"
	}

	return friendship.Status
}

// CreateFriendRequest creates a new friend request
func CreateFriendRequest(c *gin.Context, addresseeID string) (types.Friendship, error) {
	requesterID, exists := c.Get("user_id")
	if !exists || requesterID == "" {
		return types.Friendship{}, errors.New("missing or invalid user_id")
	}

	requesterIDStr := requesterID.(string)

	// Validate requester exists
	var requester types.User
	if err := db.Where("user_id = ?", requesterIDStr).First(&requester).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.Friendship{}, errors.New("requester user not found - please create your profile first")
		}
		return types.Friendship{}, err
	}

	// Validate addressee exists
	var addressee types.User
	if err := db.Where("user_id = ?", addresseeID).First(&addressee).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.Friendship{}, errors.New("addressee user not found")
		}
		return types.Friendship{}, err
	}

	// Check if trying to friend yourself
	if requesterIDStr == addresseeID {
		return types.Friendship{}, errors.New("cannot send friend request to yourself")
	}

	// Check if friendship already exists (in either direction)
	var existingFriendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status IN (?, ?)",
		requesterIDStr, addresseeID, addresseeID, requesterIDStr, "pending", "accepted",
	).First(&existingFriendship)

	if result.Error == nil {
		// Friendship already exists
		if existingFriendship.Status == "accepted" {
			return types.Friendship{}, errors.New("users are already friends")
		}
		return types.Friendship{}, errors.New("friend request already exists")
	}

	// Create new friend request
	friendship := types.Friendship{
		RequesterID: requesterIDStr,
		AddresseeID: addresseeID,
		Status:      "pending",
	}

	if err := db.Create(&friendship).Error; err != nil {
		return types.Friendship{}, err
	}

	return friendship, nil
}

// GetFriendRequests returns all pending friend requests for the user (both sent and received)
func GetFriendRequests(c *gin.Context) []types.Friendship {
	var friendships []types.Friendship
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Println("missing or invalid user_id")
		return []types.Friendship{}
	}

	result := db.Where("(requester_id = ? OR addressee_id = ?) AND status = ?", userID, userID, "pending").
		Preload("Requester").
		Preload("Addressee").
		Order("created_at DESC").
		Find(&friendships)

	if result.Error != nil {
		log.Println("Error fetching friend requests:", result.Error)
		return []types.Friendship{}
	}

	return friendships
}

// GetFriends returns all accepted friendships for the user
func GetFriends(c *gin.Context) []types.Friendship {
	var friendships []types.Friendship
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Println("missing or invalid user_id")
		return []types.Friendship{}
	}

	result := db.Where("(requester_id = ? OR addressee_id = ?) AND status = ?", userID, userID, "accepted").
		Preload("Requester").
		Preload("Addressee").
		Order("created_at DESC").
		Find(&friendships)

	if result.Error != nil {
		log.Println("Error fetching friends:", result.Error)
		return []types.Friendship{}
	}

	return friendships
}

// GetFriendProfile returns a friend's profile with allowed roadmaps
func GetFriendProfile(c *gin.Context, friendUserID string) (types.User, []types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.User{}, nil, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Fetch the friendship between current user and friend
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status = ?",
		userIDStr, friendUserID, friendUserID, userIDStr, "accepted",
	).First(&friendship)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.User{}, nil, errors.New("you are not friends with this user")
		}
		return types.User{}, nil, result.Error
	}

	// Get friend's user info
	var friendUser types.User
	if err := db.Where("user_id = ?", friendUserID).First(&friendUser).Error; err != nil {
		return types.User{}, nil, err
	}

	// Get roadmaps where the current user is the anchor
	// Friends can only see roadmaps they are anchoring for accountability
	var roadmaps []types.Roadmap
	db.Where("user_id = ? AND anchor_id = ?", friendUserID, userIDStr).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Find(&roadmaps)

	return friendUser, roadmaps, nil
}

// RemoveFriend deletes a friendship between the current user and the specified friend
func RemoveFriend(c *gin.Context, friendUserID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Find the friendship in either direction
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?))",
		userIDStr, friendUserID, friendUserID, userIDStr,
	).First(&friendship)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return errors.New("friendship not found")
		}
		return result.Error
	}

	// Delete the friendship
	if err := db.Delete(&friendship).Error; err != nil {
		return err
	}

	return nil
}

// UpdateFriendshipStatus updates the status of a friend request (accept or reject)
func UpdateFriendshipStatus(c *gin.Context, friendshipID string, newStatus string) (types.Friendship, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Friendship{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Validate status
	if newStatus != "accepted" && newStatus != "rejected" {
		return types.Friendship{}, errors.New("invalid status: must be 'accepted' or 'rejected'")
	}

	// Fetch the friendship
	var friendship types.Friendship
	if err := db.Where("id = ?", friendshipID).First(&friendship).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.Friendship{}, errors.New("friendship not found")
		}
		return types.Friendship{}, err
	}

	// Verify current user is the addressee
	if friendship.AddresseeID != userIDStr {
		return types.Friendship{}, errors.New("only the addressee can respond to a friend request")
	}

	// Verify status is pending
	if friendship.Status != "pending" {
		return types.Friendship{}, errors.New("friend request is not pending")
	}

	// If rejected, delete the friendship
	if newStatus == "rejected" {
		if err := db.Delete(&friendship).Error; err != nil {
			return types.Friendship{}, err
		}
		// Return the friendship with rejected status for the response
		friendship.Status = "rejected"
		return friendship, nil
	}

	// If accepted, update status
	now := time.Now()
	friendship.Status = "accepted"
	friendship.UpdatedAt = &now

	if err := db.Save(&friendship).Error; err != nil {
		return types.Friendship{}, err
	}

	// Auto-create a thread between the two users
	thread, err := CreateThread(c, friendship.RequesterID, friendship.AddresseeID)
	if err != nil {
		// Log the error but don't fail the friendship acceptance
		// The thread can be created later if needed
		log.Printf("Warning: Failed to create thread for friendship %s: %v", friendshipID, err)
	} else {
		log.Printf("Thread %s created for friendship between %s and %s", thread.ThreadID, friendship.RequesterID, friendship.AddresseeID)
	}

	return friendship, nil
}

// CheckAreFriends checks if two users are friends (accepted status)
func CheckAreFriends(userID1, userID2 string) bool {
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status = ?",
		userID1, userID2, userID2, userID1, "accepted",
	).First(&friendship)

	return result.Error == nil
}

// CreateAnchorRequest creates a new anchor request for a roadmap
func CreateAnchorRequest(c *gin.Context, roadmapID string, anchorUserID string) (types.AnchorRequest, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.AnchorRequest{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Verify the roadmap exists and belongs to the requester
	var roadmap types.Roadmap
	if err := db.Where("roadmap_id = ? AND user_id = ?", roadmapID, userIDStr).First(&roadmap).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.AnchorRequest{}, errors.New("roadmap not found or you don't have permission")
		}
		return types.AnchorRequest{}, err
	}

	// Check if they are friends
	if !CheckAreFriends(userIDStr, anchorUserID) {
		return types.AnchorRequest{}, errors.New("you are not friends with this user")
	}

	// Check if there's already a pending request for this roadmap and anchor
	var existingRequest types.AnchorRequest
	result := db.Where("roadmap_id = ? AND anchor_id = ? AND status = ?", roadmapID, anchorUserID, "pending").First(&existingRequest)
	if result.Error == nil {
		return types.AnchorRequest{}, errors.New("anchor request already exists for this roadmap")
	}

	// Check if the anchor is already assigned to this roadmap
	if roadmap.AnchorID != nil && *roadmap.AnchorID == anchorUserID {
		return types.AnchorRequest{}, errors.New("this user is already an anchor for this roadmap")
	}

	// Create the anchor request
	anchorRequest := types.AnchorRequest{
		RoadmapID:   roadmapID,
		RequesterID: userIDStr,
		AnchorID:    anchorUserID,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}

	if err := db.Create(&anchorRequest).Error; err != nil {
		return types.AnchorRequest{}, err
	}

	// Preload relationships
	db.Preload("Roadmap").Preload("Requester").Preload("Anchor").First(&anchorRequest, "request_id = ?", anchorRequest.RequestID)

	return anchorRequest, nil
}

// GetAnchorRequests gets all pending anchor requests for the current user (as anchor)
func GetAnchorRequests(c *gin.Context) []types.AnchorRequest {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return []types.AnchorRequest{}
	}

	var requests []types.AnchorRequest
	db.Where("anchor_id = ? AND status = ?", userID.(string), "pending").
		Preload("Roadmap").
		Preload("Requester").
		Order("created_at DESC").
		Find(&requests)

	return requests
}

// AcceptAnchorRequest accepts an anchor request and updates the roadmap
func AcceptAnchorRequest(c *gin.Context, requestID string) (types.AnchorRequest, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.AnchorRequest{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Fetch the anchor request
	var anchorRequest types.AnchorRequest
	if err := db.Where("request_id = ?", requestID).First(&anchorRequest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.AnchorRequest{}, errors.New("anchor request not found")
		}
		return types.AnchorRequest{}, err
	}

	// Verify the current user is the anchor
	if anchorRequest.AnchorID != userIDStr {
		return types.AnchorRequest{}, errors.New("only the anchor can accept this request")
	}

	// Verify the request is pending
	if anchorRequest.Status != "pending" {
		return types.AnchorRequest{}, errors.New("anchor request is not pending")
	}

	// Start a transaction
	tx := db.Begin()

	// Update the anchor request status
	now := time.Now()
	anchorRequest.Status = "accepted"
	anchorRequest.UpdatedAt = &now
	if err := tx.Save(&anchorRequest).Error; err != nil {
		tx.Rollback()
		return types.AnchorRequest{}, err
	}

	// Update the roadmap's anchor_id
	if err := tx.Model(&types.Roadmap{}).
		Where("roadmap_id = ?", anchorRequest.RoadmapID).
		Update("anchor_id", userIDStr).Error; err != nil {
		tx.Rollback()
		return types.AnchorRequest{}, err
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return types.AnchorRequest{}, err
	}

	// Reload with relationships
	db.Preload("Roadmap").Preload("Requester").Preload("Anchor").First(&anchorRequest, "request_id = ?", anchorRequest.RequestID)

	return anchorRequest, nil
}

// RejectAnchorRequest rejects an anchor request
func RejectAnchorRequest(c *gin.Context, requestID string) (types.AnchorRequest, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.AnchorRequest{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Fetch the anchor request
	var anchorRequest types.AnchorRequest
	if err := db.Where("request_id = ?", requestID).First(&anchorRequest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.AnchorRequest{}, errors.New("anchor request not found")
		}
		return types.AnchorRequest{}, err
	}

	// Verify the current user is the anchor
	if anchorRequest.AnchorID != userIDStr {
		return types.AnchorRequest{}, errors.New("only the anchor can reject this request")
	}

	// Verify the request is pending
	if anchorRequest.Status != "pending" {
		return types.AnchorRequest{}, errors.New("anchor request is not pending")
	}

	// Update the status
	now := time.Now()
	anchorRequest.Status = "rejected"
	anchorRequest.UpdatedAt = &now
	if err := db.Save(&anchorRequest).Error; err != nil {
		return types.AnchorRequest{}, err
	}

	// Reload with relationships
	db.Preload("Roadmap").Preload("Requester").Preload("Anchor").First(&anchorRequest, "request_id = ?", anchorRequest.RequestID)

	return anchorRequest, nil
}

// RemoveAnchor removes an anchor from a roadmap
func RemoveAnchor(c *gin.Context, roadmapID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Verify the roadmap exists and belongs to the user
	var roadmap types.Roadmap
	if err := db.Where("roadmap_id = ? AND user_id = ?", roadmapID, userIDStr).First(&roadmap).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("roadmap not found or you don't have permission")
		}
		return err
	}

	// Check if there's an anchor
	if roadmap.AnchorID == nil {
		return errors.New("no anchor assigned to this roadmap")
	}

	// Remove the anchor
	if err := db.Model(&types.Roadmap{}).
		Where("roadmap_id = ?", roadmapID).
		Update("anchor_id", nil).Error; err != nil {
		return err
	}

	return nil
}
