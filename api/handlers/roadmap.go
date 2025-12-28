package handlers

import (
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"fmt"
	"net/http"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
)

func CreateRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	var newRoadmap types.Roadmap

	if !utils.BindJSONOrAbort(c, &newRoadmap) {
		return
	}

	// Set type to "custom_made" for manually created roadmaps
	// (AI-generated roadmaps will have type set to "ai_generated" in the agent handler)
	if newRoadmap.Type == "" {
		newRoadmap.Type = "custom_made"
	}

	// Set start_date to null (zero time) - it will be set when the roadmap is started
	newRoadmap.StartDate = time.Time{}

	fmt.Printf("Roadmap received: %+v\n", newRoadmap)

	createdRoadmap, err := database.CreateRoadmap(c, newRoadmap)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdRoadmap)
	log.Info(ctx, "Roadmap created successfully")
}

func GetRoadmapsByUser(c *gin.Context) {
	roadmaps := database.GetRoadmapsByUser(c)
	utils.DataResponse(c, http.StatusOK, roadmaps)
}

func GetRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	view := c.DefaultQuery("view", "full") // Default to full view

	var roadmap types.Roadmap

	switch view {
	case "macro":
		// Load only macro map (strategic view)
		roadmap = database.GetRoadmapWithMacroMap(c, roadmapID)
	case "milestones":
		// Load only detailed milestones (tactical view)
		roadmap = database.GetRoadmap(c, roadmapID)
	case "full":
		// Load both macro map and milestones
		roadmap = database.GetRoadmapWithMacroMapAndMilestones(c, roadmapID)
	default:
		utils.BadRequestResponse(c, "Invalid view parameter. Use 'macro', 'milestones', or 'full'")
		return
	}

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
}

func UpdateRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	var updatedRoadmap types.Roadmap

	if !utils.BindJSONOrAbort(c, &updatedRoadmap) {
		return
	}

	fmt.Printf("Updated roadmap received: %+v\n", updatedRoadmap)

	roadmap := database.UpdateRoadmap(c, roadmapID, updatedRoadmap)
	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Info(ctx, "Roadmap updated successfully")
}

func DeleteRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	err := database.DeleteRoadmap(c, roadmapID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Roadmap deleted successfully")
	log.Info(ctx, "Roadmap deleted successfully")
}

func GetMilestonesByRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	milestones := database.GetMilestonesByRoadmap(c, roadmapID)
	utils.DataResponse(c, http.StatusOK, milestones)
}

func GetMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	milestone := database.GetMilestone(c, milestoneID)

	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	utils.DataResponse(c, http.StatusOK, milestone)
}

func UpdateMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	milestoneID := c.Param("milestoneid")
	var updatedMilestone types.Milestone

	if !utils.BindJSONOrAbort(c, &updatedMilestone) {
		return
	}

	milestone := database.UpdateMilestone(c, milestoneID, updatedMilestone)
	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	utils.DataResponse(c, http.StatusOK, milestone)
	log.Info(ctx, "Milestone updated successfully")
}

func CreateMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	var newMilestone types.Milestone

	if !utils.BindJSONOrAbort(c, &newMilestone) {
		return
	}

	fmt.Printf("Milestone received: %+v\n", newMilestone)

	createdMilestone, err := database.CreateMilestone(c, roadmapID, newMilestone)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdMilestone)
	log.Info(ctx, "Milestone created successfully")
}

func DeleteMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	milestoneID := c.Param("milestoneid")

	err := database.DeleteMilestone(c, milestoneID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Milestone deleted successfully")
	log.Info(ctx, "Milestone deleted successfully")
}

func StartRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmap(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	roadmap.StartDate = time.Now()

	for index, milestone := range roadmap.Milestones {

		fmt.Println("starting milestone ", milestone.Day)
		roadmap.Milestones[index] = dateMilestones(c, roadmap.StartDate, milestone)
		for taskIndex, task := range milestone.Tasks {
			roadmap.Milestones[index].Tasks[taskIndex] = dateTasks(c, roadmap.StartDate, task)
			log.Info(ctx, "Setting Task Date: ", task.Day, task.Date)
		}
	}

	updatedRoadmap := database.UpdateRoadmap(c, roadmapID, roadmap)

	utils.DataResponse(c, http.StatusOK, updatedRoadmap)
	log.Info(ctx, "Roadmap started successfully")
}

func dateMilestones(c *gin.Context, startDate time.Time, milestone types.Milestone) types.Milestone {

	milestone.Date = startDate.AddDate(0, 0, milestone.Day)

	return database.UpdateMilestone(c, milestone.MilestoneID, milestone)
}

func dateTasks(c *gin.Context, startDate time.Time, task types.Task) types.Task {

	task.Date = startDate.AddDate(0, 0, task.Day)

	return database.UpdateTask(c, task.TaskID, task)
}

// Macro Map handlers

func CreateMacroMap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	var newMacroMap types.MacroMap

	if !utils.BindJSONOrAbort(c, &newMacroMap) {
		return
	}

	fmt.Printf("Macro map received: %+v\n", newMacroMap)

	createdMacroMap, err := database.CreateMacroMap(c, roadmapID, newMacroMap)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdMacroMap)
	log.Info(ctx, "Macro map created successfully")
}

func GetMacroMap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	macroMap, err := database.GetMacroMap(c, roadmapID)

	if err != nil {
		utils.NotFoundResponse(c, "Macro Map")
		return
	}

	utils.DataResponse(c, http.StatusOK, macroMap)
}

// Anchor handlers

func CreateAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	var requestBody struct {
		AnchorID string `json:"anchor_id" binding:"required"`
	}

	if !utils.BindJSONOrAbort(c, &requestBody) {
		return
	}

	anchorRequest, err := database.CreateAnchorRequest(c, roadmapID, requestBody.AnchorID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found or you don't have permission" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "you are not friends with this user" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "anchor request already exists for this roadmap" || errMsg == "this user is already an anchor for this roadmap" {
			utils.ConflictResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, anchorRequest)
	log.Info(ctx, "Anchor request created successfully")

	// Send push notification to the anchor
	go sendAnchorRequestNotification(anchorRequest)
}

func GetAnchorRequests(c *gin.Context) {
	requests := database.GetAnchorRequests(c)
	utils.DataResponse(c, http.StatusOK, requests)
}

func AcceptAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	requestID := c.Param("requestid")

	anchorRequest, err := database.AcceptAnchorRequest(c, requestID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "anchor request not found" {
			utils.NotFoundResponse(c, "Anchor Request")
			return
		}
		if errMsg == "only the anchor can accept this request" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "anchor request is not pending" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, anchorRequest)
	log.Info(ctx, "Anchor request accepted successfully")

	// Send push notification to the requester
	go sendAnchorStatusNotification(anchorRequest, "accepted")
}

func RejectAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	requestID := c.Param("requestid")

	anchorRequest, err := database.RejectAnchorRequest(c, requestID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "anchor request not found" {
			utils.NotFoundResponse(c, "Anchor Request")
			return
		}
		if errMsg == "only the anchor can reject this request" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "anchor request is not pending" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, anchorRequest)
	log.Info(ctx, "Anchor request rejected successfully")

	// Send push notification to the requester
	go sendAnchorStatusNotification(anchorRequest, "rejected")
}

func RemoveAnchor(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	err := database.RemoveAnchor(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found or you don't have permission" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "no anchor assigned to this roadmap" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Anchor removed successfully")
	log.Info(ctx, "Anchor removed successfully")
}

// sendAnchorRequestNotification sends a notification when an anchor request is created
func sendAnchorRequestNotification(anchorRequest types.AnchorRequest) {
	// Get requester's name
	requester, err := database.GetUserByID(anchorRequest.RequesterID)
	if err != nil {
		log.Info(nil, "Failed to get requester info for notification:", err)
		return
	}

	requesterName := requester.FirstName + " " + requester.LastName

	utils.SendPushNotification(
		anchorRequest.AnchorID,
		"anchor_request",
		"New Anchor Request",
		requesterName+" wants you to be their anchor",
		map[string]any{
			"request_id": anchorRequest.RequestID,
			"roadmap_id": anchorRequest.RoadmapID,
		},
	)
}

// sendAnchorStatusNotification sends a notification when an anchor request is accepted or rejected
func sendAnchorStatusNotification(anchorRequest types.AnchorRequest, status string) {
	// Get anchor's name (the person who accepted/rejected)
	anchor, err := database.GetUserByID(anchorRequest.AnchorID)
	if err != nil {
		log.Info(nil, "Failed to get anchor info for notification:", err)
		return
	}

	anchorName := anchor.FirstName + " " + anchor.LastName

	var notificationType, title, body string
	if status == "accepted" {
		notificationType = "anchor_accepted"
		title = "Anchor Request Accepted"
		body = anchorName + " accepted your anchor request"
	} else if status == "rejected" {
		notificationType = "anchor_rejected"
		title = "Anchor Request Declined"
		body = anchorName + " declined your anchor request"
	} else {
		return // Unknown status
	}

	utils.SendPushNotification(
		anchorRequest.RequesterID,
		notificationType,
		title,
		body,
		map[string]any{
			"request_id": anchorRequest.RequestID,
			"roadmap_id": anchorRequest.RoadmapID,
		},
	)
}
