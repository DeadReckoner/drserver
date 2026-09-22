package handlers

import (
	"context"
	"dr/server/analytics"
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

	// Ids and the roadmap's provenance only — never the goal text the user
	// typed, which is the single most sensitive field in the product.
	analytics.Emit(ctx, analytics.EventGoalCreated, analytics.Props(
		"roadmap_id", createdRoadmap.RoadmapID,
		"goal_id", createdRoadmap.RoadmapID,
		"source_screen", createdRoadmap.Type,
	))

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

	// Anchor the start to the owner's calendar day, not the server's UTC day —
	// a user starting a roadmap on their evening is still starting it today.
	loc := database.UserLocation(roadmap.UserID)
	startDate := types.ScheduleDayIn(types.Now(), loc)

	// Day is an offset from the start day, so rebase onto the plan's earliest
	// day; otherwise a plan numbered from day 1 starts tomorrow.
	dayZero := roadmap.MinScheduledDay()

	// One transaction records the start date and dates every milestone and task
	// from its Day offset. This used to walk the hierarchy in Go and write each
	// row through UpdateMilestone/UpdateTask, which cost roughly ten queries per
	// milestone and seven per task — hundreds of round trips for one button
	// press on a full roadmap.
	if err := database.StartRoadmapSchedule(roadmapID, startDate, dayZero, loc); err != nil {
		log.Error(ctx, "Error scheduling roadmap dates", err.Error())
		utils.InternalErrorResponse(c, err)
		return
	}

	// Recompute and save all statuses for the roadmap now that it's started
	// This computes task, milestone, and phase statuses in one unified operation
	if err := database.RecomputeAndSaveStatuses(roadmapID); err != nil {
		log.Error(ctx, "Error recomputing statuses", err.Error())
	}

	// Refresh the roadmap to get the computed statuses
	updatedRoadmap := database.GetRoadmapFull(c, roadmapID)

	// Starting a roadmap is the activation moment: it is when the plan gets
	// real dates and the user's first scheduled action comes into existence.
	// Both events fire here because the CEO scorecard counts activated
	// roadmaps while the funnel counts activated goals, and they are the same
	// act — keeping them as one event would force one of the two dashboards to
	// use the other's vocabulary.
	analytics.Emit(ctx, analytics.EventRoadmapActivated, analytics.Props(
		"roadmap_id", roadmapID,
		"goal_id", roadmapID,
		"count", countScheduledTasks(updatedRoadmap),
	))
	analytics.Emit(ctx, analytics.EventGoalActivated, analytics.Props(
		"roadmap_id", roadmapID,
		"goal_id", roadmapID,
	))

	utils.DataResponse(c, http.StatusOK, updatedRoadmap)
	log.Info(ctx, "Roadmap started successfully")
}

// countScheduledTasks totals the tasks a started roadmap put on the calendar,
// across both the phase hierarchy and the legacy top-level milestones.
//
// Recorded at activation because it is the denominator's starting point: a
// roadmap that activates with three tasks and one that activates with sixty
// produce very different completion rates, and without the count those look
// identical in aggregate.
func countScheduledTasks(r types.Roadmap) int {
	total := 0
	for _, phase := range r.Phases {
		for _, m := range phase.Milestones {
			total += len(m.Tasks)
		}
	}
	for _, m := range r.Milestones {
		total += len(m.Tasks)
	}
	return total
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

	analytics.Emit(ctx, analytics.EventAnchorInviteSent, analytics.Props(
		"anchor_relationship_id", anchorRequest.RequestID,
		"roadmap_id", roadmapID,
		"item_type", "roadmap",
	))

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

	// Acceptance and relationship activation are the same instant today, but
	// they answer different questions — invite conversion versus how many
	// live anchor relationships exist — so both are recorded rather than
	// leaving one to be inferred.
	analytics.Emit(ctx, analytics.EventAnchorInviteAccepted, analytics.Props(
		"anchor_relationship_id", anchorRequest.RequestID,
		"roadmap_id", derefID(anchorRequest.RoadmapID),
		"item_type", "roadmap",
	))
	analytics.Emit(ctx, analytics.EventAnchorRelationshipActivated, analytics.Props(
		"anchor_relationship_id", anchorRequest.RequestID,
		"roadmap_id", derefID(anchorRequest.RoadmapID),
		"item_type", "roadmap",
		"anchor_active", true,
	))

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

	analytics.Emit(ctx, analytics.EventAnchorInviteDeclined, analytics.Props(
		"anchor_relationship_id", anchorRequest.RequestID,
		"roadmap_id", derefID(anchorRequest.RoadmapID),
		"item_type", "roadmap",
	))

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

// derefID returns the value of a nullable id column, or "" when unset.
// Anchor requests target either a roadmap or a habit, so exactly one of the
// two id columns is populated on any given row.
func derefID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}
