package handlers

import (
	"context"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type RoadmapHandler struct {
	service RoadmapService
}

type anchorUserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type roadmapWithAnchorResponse struct {
	types.Roadmap
	Anchor *anchorUserResponse `json:"anchor,omitempty"`
}

// NewRoadmapHandler builds a handler backed by the live database adapter.
// Tests use NewRoadmapHandlerWithService to inject a fake.
func NewRoadmapHandler(db *gorm.DB) *RoadmapHandler {
	return &RoadmapHandler{service: DBRoadmapService{}}
}

// NewRoadmapHandlerWithService is the test-friendly constructor; production
// code should use NewRoadmapHandler.
func NewRoadmapHandlerWithService(service RoadmapService) *RoadmapHandler {
	return &RoadmapHandler{service: service}
}

func (h *RoadmapHandler) CreateRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	var newRoadmap types.Roadmap

	if !utils.BindJSONOrAbort(c, &newRoadmap) {
		return
	}

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckCreateRoadmap(userID)
	}) {
		return
	}

	// Set type to "custom_made" for manually created roadmaps
	// (AI-generated roadmaps will have type set to "ai_generated" in the agent handler)
	if newRoadmap.Type == "" {
		newRoadmap.Type = "custom_made"
	}

	createdRoadmap, err := h.service.Create(c, newRoadmap)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdRoadmap)
	log.Info(ctx, "Roadmap created successfully")
}

func (h *RoadmapHandler) GetRoadmapsByUser(c *gin.Context) {
	roadmaps := h.service.ListByUser(c)
	utils.DataResponse(c, http.StatusOK, roadmaps)
}

func (h *RoadmapHandler) GetRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	view := c.DefaultQuery("view", "full") // Default to full view

	var roadmap types.Roadmap

	switch view {
	case "strategic":
		// Load only strategic view (phases with milestones, no tasks)
		roadmap = h.service.GetStrategic(c, roadmapID)
	case "milestones":
		// Load only detailed milestones (tactical view with tasks)
		roadmap = h.service.Get(c, roadmapID)
	case "full":
		// Load everything (phases, milestones, tasks, steps)
		roadmap = h.service.GetFull(c, roadmapID)
	default:
		utils.BadRequestResponse(c, "Invalid view parameter. Use 'strategic', 'milestones', or 'full'")
		return
	}

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	anchor := h.getAnchorUserResponse(ctx, roadmap.AnchorID)

	utils.DataResponse(c, http.StatusOK, roadmapWithAnchorResponse{
		Roadmap: roadmap,
		Anchor:  anchor,
	})
}

// GetMissedTasks handles GET /roadmaps/:roadmapid/missed-tasks (drserver#48).
// A task is "missed" when its date is before today and it is not completed.
// 404s when the roadmap doesn't exist or isn't owned by the caller.
func (h *RoadmapHandler) GetMissedTasks(c *gin.Context) {
	roadmapID := c.Param("roadmapid")

	tasks, err := database.GetMissedTasks(c, roadmapID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, types.MissedTasksResponse{
		RoadmapID:   roadmapID,
		MissedTasks: tasks,
	})
}

func (h *RoadmapHandler) getAnchorUserResponse(ctx context.Context, anchorID *string) *anchorUserResponse {
	if anchorID == nil || *anchorID == "" {
		return nil
	}

	anchorUser, err := database.GetUserByID(*anchorID)
	if err != nil {
		log.Warn(ctx, "Failed to fetch anchor user for roadmap response", err.Error())
		return nil
	}

	name := strings.TrimSpace(anchorUser.FirstName + " " + anchorUser.LastName)

	return &anchorUserResponse{
		ID:    anchorUser.UserID,
		Name:  name,
		Email: anchorUser.Email,
	}
}

func (h *RoadmapHandler) UpdateRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	var updatedRoadmap types.Roadmap

	if !utils.BindJSONOrAbort(c, &updatedRoadmap) {
		return
	}

	log.Info(ctx, "Updating roadmap", roadmapID)

	roadmap := h.service.Update(c, roadmapID, updatedRoadmap)
	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Info(ctx, "Roadmap updated successfully")
}

func (h *RoadmapHandler) DeleteRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	err := h.service.Delete(c, roadmapID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Roadmap deleted successfully")
	log.Info(ctx, "Roadmap deleted successfully")
}

func (h *RoadmapHandler) GetMilestonesByRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	milestones := database.GetMilestonesByRoadmap(c, roadmapID)
	utils.DataResponse(c, http.StatusOK, milestones)
}

func (h *RoadmapHandler) GetMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	milestone := database.GetMilestone(c, milestoneID)

	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	utils.DataResponse(c, http.StatusOK, milestone)
}

func (h *RoadmapHandler) UpdateMilestone(c *gin.Context) {
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

func (h *RoadmapHandler) CreateMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	var newMilestone types.Milestone

	if !utils.BindJSONOrAbort(c, &newMilestone) {
		return
	}

	log.Info(ctx, "Creating milestone for roadmap", roadmapID)

	createdMilestone, err := database.CreateMilestone(c, roadmapID, newMilestone)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdMilestone)
	log.Info(ctx, "Milestone created successfully")
}

func (h *RoadmapHandler) DeleteMilestone(c *gin.Context) {
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

func (h *RoadmapHandler) StartRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmapFull(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	date := time.Now()
	roadmap.StartDate = &date

	// Date all milestones and tasks in phases
	for phaseIdx := range roadmap.Phases {
		for milestoneIdx := range roadmap.Phases[phaseIdx].Milestones {
			milestone := &roadmap.Phases[phaseIdx].Milestones[milestoneIdx]
			log.Info(ctx, "Starting phase milestone", fmt.Sprintf("Day: %d", milestone.Day))
			updatedMilestone := h.dateMilestones(c, *roadmap.StartDate, *milestone)
			roadmap.Phases[phaseIdx].Milestones[milestoneIdx] = updatedMilestone

			for taskIdx := range updatedMilestone.Tasks {
				task := updatedMilestone.Tasks[taskIdx]
				updatedTask := h.dateTasks(c, *roadmap.StartDate, task)
				roadmap.Phases[phaseIdx].Milestones[milestoneIdx].Tasks[taskIdx] = updatedTask
				log.Info(ctx, "Set task date", fmt.Sprintf("Day: %d, Date: %v", updatedTask.Day, updatedTask.Date))
			}
		}
	}

	// Date all top-level milestones and tasks (backward compatibility)
	for milestoneIdx := range roadmap.Milestones {
		milestone := &roadmap.Milestones[milestoneIdx]
		log.Info(ctx, "Starting top-level milestone", fmt.Sprintf("Day: %d", milestone.Day))
		updatedMilestone := h.dateMilestones(c, *roadmap.StartDate, *milestone)
		roadmap.Milestones[milestoneIdx] = updatedMilestone

		for taskIdx := range updatedMilestone.Tasks {
			task := updatedMilestone.Tasks[taskIdx]
			updatedTask := h.dateTasks(c, *roadmap.StartDate, task)
			roadmap.Milestones[milestoneIdx].Tasks[taskIdx] = updatedTask
			log.Info(ctx, "Set task date", fmt.Sprintf("Day: %d, Date: %v", updatedTask.Day, updatedTask.Date))
		}
	}

	// Save the roadmap with start date first (so RecomputeAndSaveStatuses sees it)
	_ = database.UpdateRoadmap(c, roadmapID, roadmap)

	// Recompute and save all statuses for the roadmap now that it's started
	// This computes task, milestone, and phase statuses in one unified operation
	if err := database.RecomputeAndSaveStatuses(roadmapID); err != nil {
		log.Error(ctx, "Error recomputing statuses", err.Error())
	}

	// Refresh the roadmap to get the computed statuses
	updatedRoadmap := database.GetRoadmapFull(c, roadmapID)

	utils.DataResponse(c, http.StatusOK, updatedRoadmap)
	log.Info(ctx, "Roadmap started successfully")
}

func (h *RoadmapHandler) dateMilestones(c *gin.Context, startDate time.Time, milestone types.Milestone) types.Milestone {

	date := startDate.AddDate(0, 0, milestone.Day)
	milestone.Date = &date

	return database.UpdateMilestone(c, milestone.MilestoneID, milestone)
}

func (h *RoadmapHandler) dateTasks(c *gin.Context, startDate time.Time, task types.Task) types.Task {

	date := startDate.AddDate(0, 0, task.Day)
	task.Date = &date

	return database.UpdateTask(c, task.TaskID, task)
}

// Anchor handlers

func (h *RoadmapHandler) CreateAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	var requestBody struct {
		AnchorID string `json:"anchor_id" binding:"required"`
	}

	if !utils.BindJSONOrAbort(c, &requestBody) {
		return
	}

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckCreateRoadmapAnchor(userID)
	}) {
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

func (h *RoadmapHandler) GetAnchorRequests(c *gin.Context) {
	requests := database.GetAnchorRequests(c)
	utils.DataResponse(c, http.StatusOK, requests)
}

func (h *RoadmapHandler) AcceptAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	requestID := c.Param("requestid")
	userID, _ := c.Get("user_id")

	anchorRequest, err := database.AcceptAnchorRequest(c, requestID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "anchor request not found" {
			// Clean up any orphaned notification for this request
			go cleanupOrphanedAnchorNotification(ctx, userID.(string), requestID)
			utils.NotFoundResponse(c, "Anchor Request")
			return
		}
		if errMsg == "only the anchor can accept this request" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "anchor request is not pending" {
			// Request exists but is not pending - clean up notification
			go cleanupOrphanedAnchorNotification(ctx, userID.(string), requestID)
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, anchorRequest)
	log.Info(ctx, "Anchor request accepted successfully")

	// Clean up the original anchor_request notification for the anchor
	go cleanupAnchorRequestNotification(ctx, anchorRequest)

	// Send push notification to the requester
	go sendAnchorStatusNotification(anchorRequest, "accepted")
}

func (h *RoadmapHandler) RejectAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	requestID := c.Param("requestid")
	userID, _ := c.Get("user_id")

	anchorRequest, err := database.RejectAnchorRequest(c, requestID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "anchor request not found" {
			// Clean up any orphaned notification for this request
			go cleanupOrphanedAnchorNotification(ctx, userID.(string), requestID)
			utils.NotFoundResponse(c, "Anchor Request")
			return
		}
		if errMsg == "only the anchor can reject this request" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "anchor request is not pending" {
			// Request exists but is not pending - clean up notification
			go cleanupOrphanedAnchorNotification(ctx, userID.(string), requestID)
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, anchorRequest)
	log.Info(ctx, "Anchor request rejected successfully")

	// Clean up the original anchor_request notification for the anchor
	go cleanupAnchorRequestNotification(ctx, anchorRequest)

	// Send push notification to the requester
	go sendAnchorStatusNotification(anchorRequest, "rejected")
}

// RemoveAnchor ends the anchor relationship on a roadmap. The roadmap owner
// or the anchor themselves may call it (drserver#93); both parties are
// notified.
func (h *RoadmapHandler) RemoveAnchor(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	roadmap, err := database.RemoveAnchor(c, roadmapID)
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

	go sendAnchorRemovedNotifications(
		c.GetString("user_id"),
		roadmap.UserID, *roadmap.AnchorID,
		types.TargetTypeRoadmap, roadmap.RoadmapID, roadmap.Goal,
	)
}

// sendAnchorRemovedNotifications notifies both parties when an anchor
// relationship ends (drserver#93): the actor sees a confirmation, the other
// party is informed. Routed through notifications.Dispatch so each
// recipient's F&A settings, mutes, and quiet hours apply. Shared by the
// roadmap and habit removal handlers; targetID is a roadmap or habit ID
// depending on targetType, and title is the goal/habit title for the body
// copy.
func sendAnchorRemovedNotifications(removedByID, ownerID, anchorID, targetType, targetID, title string) {
	ctx := context.Background()

	owner, err := database.GetUserByID(ownerID)
	if err != nil {
		log.Info(ctx, "Failed to get owner info for anchor-removed notification:", err)
		return
	}
	anchor, err := database.GetUserByID(anchorID)
	if err != nil {
		log.Info(ctx, "Failed to get anchor info for anchor-removed notification:", err)
		return
	}
	ownerName := strings.TrimSpace(owner.FirstName + " " + owner.LastName)
	anchorName := strings.TrimSpace(anchor.FirstName + " " + anchor.LastName)

	data := map[string]any{"target_type": targetType}
	roadmapID := ""
	if targetType == types.TargetTypeRoadmap {
		data["roadmap_id"] = targetID
		roadmapID = targetID
	} else {
		data["habit_id"] = targetID
	}

	var ownerBody, anchorBody string
	if removedByID == anchorID {
		ownerBody = anchorName + " stepped down as your anchor for \"" + title + "\""
		anchorBody = "You stepped down as anchor for " + ownerName + "'s \"" + title + "\""
	} else {
		ownerBody = "You removed " + anchorName + " as your anchor for \"" + title + "\""
		anchorBody = ownerName + " removed you as anchor for \"" + title + "\""
	}

	// Dedup per calendar day: a double-tap doesn't double-send, but the same
	// pair re-anchoring and parting ways later still notifies.
	day := time.Now().UTC().Format("2006-01-02")

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           ownerID,
		Category:         notifications.CategoryFAndA,
		NotificationType: "anchor_accepted_removed",
		DedupKey:         targetID + ":anchor_removed:" + anchorID + ":" + ownerID + ":" + day,
		FriendID:         anchorID,
		Title:            "Anchor Removed",
		Body:             ownerBody,
		Data:             data,
	})

	// The anchor's own per-roadmap override (if any) applies to their copy.
	notifications.Dispatch(notifications.DispatchOptions{
		UserID:            anchorID,
		Category:          notifications.CategoryFAndA,
		NotificationType:  "anchor_accepted_removed",
		DedupKey:          targetID + ":anchor_removed:" + anchorID + ":" + anchorID + ":" + day,
		FriendID:          ownerID,
		AnchoredRoadmapID: roadmapID,
		Title:             "Anchor Removed",
		Body:              anchorBody,
		Data:              data,
	})
}

// sendAnchorRequestNotification sends a notification when an anchor request is created.
// Routes through notifications.Dispatch so the recipient's F&A settings take
// effect and so duplicate triggers don't double-send.
func sendAnchorRequestNotification(anchorRequest types.AnchorRequest) {
	ctx := context.Background()
	// Get requester's name
	requester, err := database.GetUserByID(anchorRequest.RequesterID)
	if err != nil {
		log.Info(ctx, "Failed to get requester info for notification:", err)
		return
	}

	requesterName := requester.FirstName + " " + requester.LastName

	// AnchoredRoadmapID lets the resolver consider any per-roadmap override
	// the user might have created in the past for this roadmap (rare but
	// possible if they previously anchored and removed themselves).
	roadmapID := ""
	if anchorRequest.RoadmapID != nil {
		roadmapID = *anchorRequest.RoadmapID
	}

	// Build data with the dereferenced roadmap_id so the frontend never
	// sees a JSON `null` here — the key is simply absent if there's no
	// roadmap (the anchor-on-habit case, where the data carries habit_id
	// instead via sendAnchorStatusNotification's handling).
	data := map[string]any{"request_id": anchorRequest.RequestID}
	if anchorRequest.RoadmapID != nil {
		data["roadmap_id"] = *anchorRequest.RoadmapID
	}

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:            anchorRequest.AnchorID,
		Category:          notifications.CategoryFAndA,
		NotificationType:  "anchor_request",
		DedupKey:          anchorRequest.RequestID,
		AnchoredRoadmapID: roadmapID,
		Title:             "New Anchor Request",
		Body:              requesterName + " wants you to be their anchor",
		Data:              data,
	})
}

// sendAnchorStatusNotification sends a notification when an anchor request is
// accepted or rejected.
//
// Both acceptance and rejection route through notifications.Dispatch. Rejection
// rides the "anchor_accepted_removed" toggle (FAndAPrefs.Lookup maps the
// "anchor_rejected" type string to it) so the recipient's F&A settings, mutes,
// and quiet hours all apply.
func sendAnchorStatusNotification(anchorRequest types.AnchorRequest, status string) {
	ctx := context.Background()
	anchor, err := database.GetUserByID(anchorRequest.AnchorID)
	if err != nil {
		log.Info(ctx, "Failed to get anchor info for notification:", err)
		return
	}

	anchorName := anchor.FirstName + " " + anchor.LastName

	data := map[string]any{
		"request_id":  anchorRequest.RequestID,
		"target_type": anchorRequest.TargetType,
	}
	if anchorRequest.RoadmapID != nil {
		data["roadmap_id"] = *anchorRequest.RoadmapID
	}
	if anchorRequest.HabitID != nil {
		data["habit_id"] = *anchorRequest.HabitID
	}

	roadmapID := ""
	if anchorRequest.RoadmapID != nil {
		roadmapID = *anchorRequest.RoadmapID
	}

	switch status {
	case "accepted":
		notifications.Dispatch(notifications.DispatchOptions{
			UserID:            anchorRequest.RequesterID,
			Category:          notifications.CategoryFAndA,
			NotificationType:  "anchor_accepted_removed",
			DedupKey:          anchorRequest.RequestID + ":accepted",
			AnchoredRoadmapID: roadmapID,
			Title:             "Anchor Request Accepted",
			Body:              anchorName + " accepted your anchor request",
			Data:              data,
		})

	case "rejected":
		notifications.Dispatch(notifications.DispatchOptions{
			UserID:            anchorRequest.RequesterID,
			Category:          notifications.CategoryFAndA,
			NotificationType:  "anchor_rejected",
			DedupKey:          anchorRequest.RequestID + ":rejected",
			AnchoredRoadmapID: roadmapID,
			Title:             "Anchor Request Declined",
			Body:              anchorName + " declined your anchor request",
			Data:              data,
		})

	default:
		return // Unknown status
	}
}

// cleanupAnchorRequestNotification removes the original anchor_request notification
// when an anchor request is accepted or rejected to prevent duplicate notifications
func cleanupAnchorRequestNotification(ctx context.Context, anchorRequest types.AnchorRequest) {
	notifType := "anchor_request"
	if anchorRequest.TargetType == types.TargetTypeHabit {
		notifType = "habit_anchor_request"
	}
	err := database.DeleteNotificationsByTypeAndData(
		ctx,
		anchorRequest.AnchorID,
		notifType,
		"request_id",
		anchorRequest.RequestID,
	)
	if err != nil {
		log.Info(ctx, "Failed to cleanup anchor request notification:", err)
	}
}

// cleanupOrphanedAnchorNotification cleans up notifications for anchor requests
// that no longer exist or are no longer pending. This handles cases where the
// underlying request was deleted or already processed but the notification remains.
func cleanupOrphanedAnchorNotification(ctx context.Context, userID string, requestID string) {
	// Try both notification types since we don't know which type it was
	for _, notifType := range []string{"anchor_request", "habit_anchor_request"} {
		err := database.DeleteNotificationsByTypeAndData(
			ctx,
			userID,
			notifType,
			"request_id",
			requestID,
		)
		if err != nil {
			log.Info(ctx, "Failed to cleanup orphaned notification:", err)
		}
	}
}

// Lifecycle handlers

// RestartRoadmap resets a roadmap to start fresh from today
func (h *RoadmapHandler) RestartRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	roadmap, err := h.service.Restart(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "roadmap has not been started" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Info(ctx, "Roadmap restarted successfully")
}

// ShiftDates shifts all dates in a roadmap by the specified number of days.
// This shifts the entire roadmap including start_date, all milestones, and all tasks.
func (h *RoadmapHandler) ShiftDates(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	var req types.ShiftDatesRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	roadmap, err := h.service.ShiftDates(c, roadmapID, req.Days)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "roadmap has not been started" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Info(ctx, "Roadmap dates shifted successfully", fmt.Sprintf("days=%d", req.Days))
}

// RealignDates realigns incomplete task dates so the first incomplete task
// is scheduled for today + offset_days. Only affects incomplete tasks and their
// associated milestones. Completed tasks retain their original dates.
//
// Request body:
//   - offset_days (optional, default 0): 0 = first task due today, 2 = first task due in 2 days
func (h *RoadmapHandler) RealignDates(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	var req types.RealignDatesRequest
	// Body is optional (offset_days defaults to 0)
	_ = c.ShouldBindJSON(&req)

	roadmap, daysShifted, err := database.RealignRoadmapDates(c, roadmapID, req.OffsetDays)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "roadmap has not been started" || errMsg == "no incomplete tasks found" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	response := types.RealignDatesResponse{
		Roadmap:     roadmap,
		DaysShifted: daysShifted,
	}
	utils.DataResponse(c, http.StatusOK, response)
	log.Info(ctx, "Roadmap dates realigned successfully", fmt.Sprintf("days_shifted=%d, offset=%d", daysShifted, req.OffsetDays))
}

// PauseRoadmap pauses a roadmap by setting the paused_at timestamp
func (h *RoadmapHandler) PauseRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	err := h.service.Pause(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "roadmap has not been started" || errMsg == "roadmap is already paused" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Roadmap paused successfully")
	log.Info(ctx, "Roadmap paused successfully")
}

// ResumeRoadmap resumes a paused roadmap and shifts dates forward
func (h *RoadmapHandler) ResumeRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	roadmap, err := h.service.Resume(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "roadmap is not paused" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Info(ctx, "Roadmap resumed successfully")
}

// ArchiveRoadmap archives a roadmap. Free users may archive (drmobile#183):
// archiving is how they swap which roadmap is active without deleting
// history, so it must not sit behind the paywall.
func (h *RoadmapHandler) ArchiveRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	err := h.service.Archive(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Roadmap archived successfully")
	log.Info(ctx, "Roadmap archived successfully")
}

// RestoreRoadmap restores an archived roadmap. Reactivating counts against
// the free active-roadmap cap (drmobile#183): a free user with an active
// roadmap must archive or delete it (or upgrade) before reactivating another.
func (h *RoadmapHandler) RestoreRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckCreateRoadmap(userID)
	}) {
		return
	}

	var req types.RestoreRoadmapRequest
	// Body is optional, so we don't use BindJSONOrAbort
	_ = c.ShouldBindJSON(&req)

	roadmap, err := h.service.Restore(c, roadmapID, req.StartFromToday)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		if errMsg == "roadmap is not archived" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Info(ctx, "Roadmap restored successfully")
}

// ShareRoadmap generates a share token for a roadmap
func (h *RoadmapHandler) ShareRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	token, err := database.GenerateShareToken(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, gin.H{
		"share_token": token,
	})
	log.Info(ctx, "Roadmap share token generated successfully")
}

// RevokeShare removes the share token from a roadmap
func (h *RoadmapHandler) RevokeShare(c *gin.Context) {
	ctx := c.Request.Context()
	roadmapID := c.Param("roadmapid")

	err := database.RevokeShareToken(c, roadmapID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "roadmap not found" {
			utils.NotFoundResponse(c, "Roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Share link revoked successfully")
	log.Info(ctx, "Roadmap share token revoked successfully")
}

// GetSharedRoadmap returns a shared roadmap by its share token (public endpoint)
func (h *RoadmapHandler) GetSharedRoadmap(c *gin.Context) {
	token := c.Param("token")

	roadmap, err := database.GetRoadmapByShareToken(token)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "shared roadmap not found" {
			utils.NotFoundResponse(c, "Shared roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	// Return roadmap without sensitive user data
	// Clear user-specific fields for privacy
	roadmap.UserID = ""
	roadmap.AnchorID = nil
	roadmap.ShareToken = nil

	utils.DataResponse(c, http.StatusOK, roadmap)
}

// CopySharedRoadmap creates a copy of a shared roadmap for the authenticated user
func (h *RoadmapHandler) CopySharedRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	token := c.Param("token")

	roadmap, err := database.CopyRoadmapForUser(c, token)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "shared roadmap not found" {
			utils.NotFoundResponse(c, "Shared roadmap")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, roadmap)
	log.Info(ctx, "Shared roadmap copied successfully")
}
