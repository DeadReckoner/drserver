package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"net/http"

	"github.com/gin-gonic/gin"
)

func SearchFriends(c *gin.Context) {
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

func CreateFriendRequest(c *gin.Context) {
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

func GetFriendRequests(c *gin.Context) {
	friendRequests := database.GetFriendRequests(c)
	utils.DataResponse(c, http.StatusOK, friendRequests)
}

func GetFriends(c *gin.Context) {
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

func GetFriendProfile(c *gin.Context) {
	friendUserID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	friendUser, roadmaps, err := database.GetFriendProfile(c, friendUserID)
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
		ThreadID:        threadID,
	}

	utils.DataResponse(c, http.StatusOK, response)
}

func RemoveFriend(c *gin.Context) {
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

func UpdateFriendRequestStatus(c *gin.Context) {
	ctx := c.Request.Context()
	friendshipID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	var req types.UpdateFriendshipStatusRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	friendship, err := database.UpdateFriendshipStatus(c, friendshipID, req.Status)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "friendship not found" {
			utils.NotFoundResponse(c, "Friendship")
			return
		}
		if errMsg == "only the addressee can respond to a friend request" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "friend request is not pending" {
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

	// Send push notification to the requester
	go sendFriendStatusNotification(friendship, req.Status)
}

// sendFriendRequestNotification sends a notification when a friend request is created
func sendFriendRequestNotification(friendship types.Friendship) {
	// Get requester's name
	requester, err := database.GetUserByID(friendship.RequesterID)
	if err != nil {
		log.Info(nil, "Failed to get requester info for notification:", err)
		return
	}

	requesterName := requester.FirstName + " " + requester.LastName

	utils.SendPushNotification(
		friendship.AddresseeID,
		"friend_request",
		"New Friend Request",
		requesterName+" wants to connect with you",
		map[string]any{
			"friendship_id": friendship.ID,
			"requester_id":  friendship.RequesterID,
		},
	)
}

// sendFriendStatusNotification sends a notification when a friend request is accepted or rejected
func sendFriendStatusNotification(friendship types.Friendship, status string) {
	// Get addressee's name (the person who accepted/rejected)
	addressee, err := database.GetUserByID(friendship.AddresseeID)
	if err != nil {
		log.Info(nil, "Failed to get addressee info for notification:", err)
		return
	}

	addresseeName := addressee.FirstName + " " + addressee.LastName

	var notificationType, title, body string
	if status == "accepted" {
		notificationType = "friend_accepted"
		title = "Friend Request Accepted"
		body = addresseeName + " accepted your friend request"
	} else if status == "rejected" {
		notificationType = "friend_rejected"
		title = "Friend Request Declined"
		body = addresseeName + " declined your friend request"
	} else {
		return // Unknown status
	}

	utils.SendPushNotification(
		friendship.RequesterID,
		notificationType,
		title,
		body,
		map[string]any{
			"friendship_id": friendship.ID,
			"addressee_id":  friendship.AddresseeID,
		},
	)
}
