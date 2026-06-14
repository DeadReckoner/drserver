package handlers

import (
	"context"
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FriendHandler struct {
	db *gorm.DB
}

func NewFriendHandler(db *gorm.DB) *FriendHandler {
	return &FriendHandler{db: db}
}

func (h *FriendHandler) SearchFriends(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		utils.BadRequestResponse(c, "email query parameter is required")
		return
	}

	currentUserID, exists := c.Get("user_id")
	if !exists || currentUserID == "" {
		utils.InternalErrorResponse(c, nil)
		return
	}

	users := database.SearchUsersByEmail(email)

	// Convert to search results with friendship status
	results := make([]types.UserSearchResult, 0, len(users))
	for _, user := range users {
		// Skip the current user
		if user.UserID == currentUserID.(string) {
			continue
		}

		friendshipStatus := database.GetFriendshipStatus(currentUserID.(string), user.UserID)
		results = append(results, types.UserSearchResult{
			UserID:           user.UserID,
			FirstName:        user.FirstName,
			LastName:         user.LastName,
			Email:            user.Email,
			FriendshipStatus: friendshipStatus,
		})
	}

	utils.DataResponse(c, http.StatusOK, results)
}

func (h *FriendHandler) CreateFriendRequest(c *gin.Context) {
	ctx := c.Request.Context()
	addresseeID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	friendship, err := database.CreateFriendRequest(c, addresseeID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "requester user not found - please create your profile first" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		if errMsg == "addressee user not found" {
			utils.NotFoundResponse(c, "User")
			return
		}
		if errMsg == "cannot send friend request to yourself" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		if errMsg == "users are already friends" || errMsg == "friend request already exists" {
			utils.ConflictResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, friendship)
	log.Info(ctx, "Friend request created successfully")

	// Send push notification to the addressee
	go sendFriendRequestNotification(friendship)
}

func (h *FriendHandler) GetFriendRequests(c *gin.Context) {
	friendRequests := database.GetFriendRequests(c)
	utils.DataResponse(c, http.StatusOK, friendRequests)
}

func (h *FriendHandler) GetFriends(c *gin.Context) {
	currentUserID, exists := c.Get("user_id")
	if !exists || currentUserID == "" {
		utils.InternalErrorResponse(c, nil)
		return
	}

	friendships := database.GetFriends(c)

	// Transform friendships to friend responses
	friends := make([]types.FriendResponse, 0, len(friendships))
	for _, friendship := range friendships {
		var friend types.User

		// Determine which user is the friend (not the current user)
		if friendship.RequesterID == currentUserID.(string) {
			if friendship.Addressee != nil {
				friend = *friendship.Addressee
			}
		} else {
			if friendship.Requester != nil {
				friend = *friendship.Requester
			}
		}

		friends = append(friends, types.FriendResponse{
			FriendshipID: friendship.ID,
			Friend:       friend,
			Restrictions: friendship.Restrictions,
			CreatedAt:    friendship.CreatedAt,
		})
	}

	utils.DataResponse(c, http.StatusOK, friends)
}

func (h *FriendHandler) GetFriendProfile(c *gin.Context) {
	friendUserID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	friendUser, roadmaps, habits, err := database.GetFriendProfile(c, friendUserID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "you are not friends with this user" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	// Get thread_id between the two users
	userID, _ := c.Get("user_id")
	threadID := database.GetThreadBetweenUsers(c, userID.(string), friendUserID)

	response := types.FriendProfileResponse{
		User:            friendUser,
		AllowedRoadmaps: roadmaps,
		AllowedHabits:   habits,
		ThreadID:        threadID,
	}

	utils.DataResponse(c, http.StatusOK, response)
}

func (h *FriendHandler) RemoveFriend(c *gin.Context) {
	ctx := c.Request.Context()
	friendUserID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	err := database.RemoveFriend(c, friendUserID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "friendship not found" {
			utils.NotFoundResponse(c, "Friendship")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Friendship removed successfully")
	log.Info(ctx, "Friendship removed successfully")
}

func (h *FriendHandler) UpdateFriendRequestStatus(c *gin.Context) {
	ctx := c.Request.Context()
	friendshipID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}
	userID, _ := c.Get("user_id")

	var req types.UpdateFriendshipStatusRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	friendship, err := database.UpdateFriendshipStatus(c, friendshipID, req.Status)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "friendship not found" {
			// Clean up any orphaned notification for this friend request
			go cleanupOrphanedFriendRequestNotification(ctx, userID.(string), friendshipID)
			utils.NotFoundResponse(c, "Friendship")
			return
		}
		if errMsg == "only the addressee can respond to a friend request" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "friend request is not pending" {
			// Request exists but is not pending - clean up notification
			go cleanupOrphanedFriendRequestNotification(ctx, userID.(string), friendshipID)
			utils.BadRequestResponse(c, errMsg)
			return
		}
		if errMsg == "invalid status: must be 'accepted' or 'rejected'" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, friendship)
	log.Info(ctx, "Friend request status updated to: "+req.Status)

	// Clean up the original friend_request notification for the addressee
	go cleanupFriendRequestNotification(ctx, friendship)

	// Send push notification to the requester
	go sendFriendStatusNotification(friendship, req.Status)
}

// sendFriendRequestNotification sends a notification when a friend request is created.
// Routes through notifications.Dispatch so the recipient's F&A settings and any
// per-friend override take effect, and so duplicate triggers don't double-send.
func sendFriendRequestNotification(friendship types.Friendship) {
	ctx := context.Background()
	// Get requester's name
	requester, err := database.GetUserByID(friendship.RequesterID)
	if err != nil {
		log.Info(ctx, "Failed to get requester info for notification:", err)
		return
	}

	requesterName := requester.FirstName + " " + requester.LastName

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           friendship.AddresseeID,
		Category:         notifications.CategoryFAndA,
		NotificationType: "friend_request",
		DedupKey:         friendship.ID,
		FriendID:         friendship.RequesterID,
		Title:            "New Friend Request",
		Body:             requesterName + " wants to connect with you",
		Data: map[string]any{
			"friendship_id": friendship.ID,
			"requester_id":  friendship.RequesterID,
		},
	})
}

// sendFriendStatusNotification sends a notification when a friend request is accepted or rejected
func sendFriendStatusNotification(friendship types.Friendship, status string) {
	ctx := context.Background()
	// Get addressee's name (the person who accepted/rejected)
	addressee, err := database.GetUserByID(friendship.AddresseeID)
	if err != nil {
		log.Info(ctx, "Failed to get addressee info for notification:", err)
		return
	}

	addresseeName := addressee.FirstName + " " + addressee.LastName

	var notificationType, title, body string
	switch status {
	case "accepted":
		notificationType = "friend_accepted"
		title = "Friend Request Accepted"
		body = addresseeName + " accepted your friend request"
	case "rejected":
		notificationType = "friend_rejected"
		title = "Friend Request Declined"
		body = addresseeName + " declined your friend request"
	default:
		return // Unknown status
	}

	// Both accepted/rejected ride the friend_request toggle (mapped in
	// FAndAPrefs.Lookup). FriendID is the addressee so a per-friend mute applies.
	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           friendship.RequesterID,
		Category:         notifications.CategoryFAndA,
		NotificationType: notificationType,
		DedupKey:         friendship.ID + ":" + status,
		FriendID:         friendship.AddresseeID,
		Title:            title,
		Body:             body,
		Data: map[string]any{
			"friendship_id": friendship.ID,
			"addressee_id":  friendship.AddresseeID,
		},
	})
}

// cleanupFriendRequestNotification removes the original friend_request notification
// when a friend request is accepted or rejected to prevent duplicate notifications
func cleanupFriendRequestNotification(ctx context.Context, friendship types.Friendship) {
	err := database.DeleteNotificationsByTypeAndData(
		ctx,
		friendship.AddresseeID,
		"friend_request",
		"friendship_id",
		friendship.ID,
	)
	if err != nil {
		log.Info(ctx, "Failed to cleanup friend request notification:", err)
	}
}

// cleanupOrphanedFriendRequestNotification cleans up notifications for friend requests
// that no longer exist or are no longer pending. This handles cases where the
// underlying request was deleted or already processed but the notification remains.
func cleanupOrphanedFriendRequestNotification(ctx context.Context, userID string, friendshipID string) {
	err := database.DeleteNotificationsByTypeAndData(
		ctx,
		userID,
		"friend_request",
		"friendship_id",
		friendshipID,
	)
	if err != nil {
		log.Info(ctx, "Failed to cleanup orphaned friend request notification:", err)
	}
}
