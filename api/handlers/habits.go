package handlers

import (
	"context"
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type HabitHandler struct {
	service HabitService
}

// NewHabitHandler builds a handler backed by the live database adapter.
// Tests use NewHabitHandlerWithService to inject a fake.
func NewHabitHandler(db *gorm.DB) *HabitHandler {
	return &HabitHandler{service: DBHabitService{}}
}

// NewHabitHandlerWithService is the test-friendly constructor; production
// code should use NewHabitHandler.
func NewHabitHandlerWithService(service HabitService) *HabitHandler {
	return &HabitHandler{service: service}
}

// =============================================================================
// HABIT CRUD HANDLERS
// =============================================================================

// CreateHabit creates a new habit
// POST /api/v1/habits
func (h *HabitHandler) CreateHabit(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.CreateHabitRequest
	if !utils.BindJSONOrAbort(c, &req) {
		log.Warn(ctx, "CreateHabit: failed to bind JSON request")
		return
	}

	// Validate the request
	if err := req.Validate(); err != nil {
		log.Warn(ctx, "CreateHabit: validation failed", err.Error())
		utils.BadRequestResponse(c, err.Error())
		return
	}

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckCreateHabit(userID)
	}) {
		return
	}

	// Set default start date if not provided
	startDate := time.Now()
	if req.StartDate != nil {
		startDate = *req.StartDate
	}

	// Build habit from request
	habit := types.Habit{
		Title:          req.Title,
		Description:    req.Description,
		RecurrenceType: req.RecurrenceType,
		DaysOfWeek:     req.DaysOfWeek,
		DayOfMonth:     req.DayOfMonth,
		DueDate:        req.DueDate,
		StartDate:      startDate,
		EndDate:        req.EndDate,
	}

	log.Info(ctx, "CreateHabit: creating habit", req.Title)

	// Create habit in database
	createdHabit, err := h.service.Create(c, habit)
	if err != nil {
		log.Error(ctx, "CreateHabit: failed to create habit", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdHabit)
	log.Info(ctx, "CreateHabit: habit created successfully", createdHabit.HabitID)
}

// GetHabit retrieves a single habit by ID
// GET /api/v1/habits/:habitid
func (h *HabitHandler) GetHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	log.Info(ctx, "GetHabit: retrieving habit", habitID)

	habit := h.service.Get(c, habitID)

	// Check if habit was found (empty HabitID indicates not found)
	if habit.HabitID == "" {
		log.Warn(ctx, "GetHabit: habit not found", habitID)
		utils.NotFoundResponse(c, "Habit")
		return
	}

	utils.DataResponse(c, http.StatusOK, habit)
	log.Info(ctx, "GetHabit: habit retrieved successfully", habitID)
}

// GetHabits retrieves all habits for the user
// GET /api/v1/habits?include_archived=true
func (h *HabitHandler) GetHabits(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse query parameter for including archived habits
	includeArchived := c.Query("include_archived") == "true"

	log.Info(ctx, "GetHabits: retrieving all habits", "include_archived:", includeArchived)

	habits, err := h.service.List(c, includeArchived)
	if err != nil {
		log.Error(ctx, "GetHabits: failed to retrieve habits", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, habits)
	log.Info(ctx, "GetHabits: retrieved habits successfully", len(habits))
}

// GetTodaysHabits retrieves habits due today with completion status
// GET /api/v1/habits/today
func (h *HabitHandler) GetTodaysHabits(c *gin.Context) {
	ctx := c.Request.Context()

	log.Info(ctx, "GetTodaysHabits: retrieving habits for today")

	// Normalize to UTC at start of day to ensure consistent date matching
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	habits, err := h.service.ForDate(c, todayStart)
	if err != nil {
		log.Error(ctx, "GetTodaysHabits: failed to retrieve habits", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, habits)
	log.Info(ctx, "GetTodaysHabits: retrieved habits successfully", len(habits))
}

// GetHabitsForDate retrieves habits for a specific date
// GET /api/v1/habits/date/:date
func (h *HabitHandler) GetHabitsForDate(c *gin.Context) {
	ctx := c.Request.Context()
	dateStr := c.Param("date")

	// Parse the date parameter (expected format: YYYY-MM-DD)
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		log.Warn(ctx, "GetHabitsForDate: invalid date format", dateStr)
		utils.BadRequestResponse(c, "invalid date format, use YYYY-MM-DD")
		return
	}

	log.Info(ctx, "GetHabitsForDate: retrieving habits for date", dateStr)

	habits, err := h.service.ForDate(c, date)
	if err != nil {
		log.Error(ctx, "GetHabitsForDate: failed to retrieve habits", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, habits)
	log.Info(ctx, "GetHabitsForDate: retrieved habits successfully", len(habits))
}

// GetHabitsForRange retrieves habits for a date range (calendar view)
// GET /api/v1/habits/range?start=2024-01-01&end=2024-01-31
func (h *HabitHandler) GetHabitsForRange(c *gin.Context) {
	ctx := c.Request.Context()

	startStr := c.Query("start")
	endStr := c.Query("end")

	// Validate that both parameters are provided
	if startStr == "" || endStr == "" {
		log.Warn(ctx, "GetHabitsForRange: missing start or end date")
		utils.BadRequestResponse(c, "start and end dates required")
		return
	}

	// Parse start date
	startDate, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		log.Warn(ctx, "GetHabitsForRange: invalid start date format", startStr)
		utils.BadRequestResponse(c, "invalid start date format, use YYYY-MM-DD")
		return
	}

	// Parse end date
	endDate, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		log.Warn(ctx, "GetHabitsForRange: invalid end date format", endStr)
		utils.BadRequestResponse(c, "invalid end date format, use YYYY-MM-DD")
		return
	}

	// Limit range to 90 days to prevent excessive queries
	if endDate.Sub(startDate).Hours()/24 > 90 {
		log.Warn(ctx, "GetHabitsForRange: date range exceeds 90 days")
		utils.BadRequestResponse(c, "date range cannot exceed 90 days")
		return
	}

	log.Info(ctx, "GetHabitsForRange: retrieving habits for range", startStr, "to", endStr)

	habits, err := h.service.ForDateRange(c, startDate, endDate)
	if err != nil {
		log.Error(ctx, "GetHabitsForRange: failed to retrieve habits", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, habits)
	log.Info(ctx, "GetHabitsForRange: retrieved habits successfully", len(habits), "days")
}

// UpdateHabit updates an existing habit
// PATCH /api/v1/habits/:habitid
func (h *HabitHandler) UpdateHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	var req types.UpdateHabitRequest
	if !utils.BindJSONOrAbort(c, &req) {
		log.Warn(ctx, "UpdateHabit: failed to bind JSON request")
		return
	}

	log.Info(ctx, "UpdateHabit: updating habit", habitID)

	// First, get the existing habit to preserve fields not being updated
	existingHabit := h.service.Get(c, habitID)
	if existingHabit.HabitID == "" {
		log.Warn(ctx, "UpdateHabit: habit not found", habitID)
		utils.NotFoundResponse(c, "Habit")
		return
	}

	// Apply updates (only non-nil fields)
	if req.Title != nil {
		existingHabit.Title = *req.Title
	}
	if req.Description != nil {
		existingHabit.Description = *req.Description
	}
	if req.RecurrenceType != nil {
		existingHabit.RecurrenceType = *req.RecurrenceType
	}
	if req.DaysOfWeek != nil {
		existingHabit.DaysOfWeek = *req.DaysOfWeek
	}
	if req.DayOfMonth != nil {
		existingHabit.DayOfMonth = req.DayOfMonth
	}
	if req.DueDate != nil {
		existingHabit.DueDate = req.DueDate
	}
	if req.StartDate != nil {
		existingHabit.StartDate = *req.StartDate
	}
	if req.EndDate != nil {
		existingHabit.EndDate = req.EndDate
	}
	if req.Archived != nil {
		existingHabit.Archived = *req.Archived
	}

	// Update habit in database
	updatedHabit, err := h.service.Update(c, habitID, existingHabit)
	if err != nil {
		log.Error(ctx, "UpdateHabit: failed to update habit", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, updatedHabit)
	log.Info(ctx, "UpdateHabit: habit updated successfully", habitID)
}

// DeleteHabit deletes a habit
// DELETE /api/v1/habits/:habitid
func (h *HabitHandler) DeleteHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	log.Info(ctx, "DeleteHabit: deleting habit", habitID)

	err := h.service.Delete(c, habitID)
	if err != nil {
		if err == types.ErrHabitNotFound {
			log.Warn(ctx, "DeleteHabit: habit not found", habitID)
			utils.NotFoundResponse(c, "Habit")
			return
		}
		log.Error(ctx, "DeleteHabit: failed to delete habit", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Habit deleted successfully")
	log.Info(ctx, "DeleteHabit: habit deleted successfully", habitID)
}

// ArchiveHabit archives a habit
// POST /api/v1/habits/:habitid/archive
func (h *HabitHandler) ArchiveHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	log.Info(ctx, "ArchiveHabit: archiving habit", habitID)

	err := h.service.Archive(c, habitID)
	if err != nil {
		if err == types.ErrHabitNotFound {
			log.Warn(ctx, "ArchiveHabit: habit not found", habitID)
			utils.NotFoundResponse(c, "Habit")
			return
		}
		log.Error(ctx, "ArchiveHabit: failed to archive habit", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Habit archived successfully")
	log.Info(ctx, "ArchiveHabit: habit archived successfully", habitID)
}

// =============================================================================
// COMPLETION HANDLERS
// =============================================================================

// CompleteHabit marks a habit as complete
// POST /api/v1/habits/:habitid/complete
func (h *HabitHandler) CompleteHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	var req types.CompleteHabitRequest
	// Allow empty body - will use defaults
	if err := c.ShouldBindJSON(&req); err != nil {
		req = types.CompleteHabitRequest{}
	}

	// Default to today if no date provided
	date := time.Now()
	if req.Date != nil {
		date = *req.Date
	}

	log.Info(ctx, "CompleteHabit: marking habit complete", habitID, "for date", date.Format("2006-01-02"))

	completion, err := h.service.Complete(c, habitID, date, req.Notes)
	if err != nil {
		if err == types.ErrAlreadyCompleted {
			log.Warn(ctx, "CompleteHabit: habit already completed", habitID)
			utils.BadRequestResponse(c, "habit already completed for this date")
			return
		}
		log.Error(ctx, "CompleteHabit: failed to complete habit", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, completion)
	log.Info(ctx, "CompleteHabit: habit completed successfully", habitID)
}

// UncompleteHabit removes a completion
// DELETE /api/v1/habits/:habitid/complete?date=2024-01-15
func (h *HabitHandler) UncompleteHabit(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	// Default to today
	date := time.Now()
	if dateStr := c.Query("date"); dateStr != "" {
		var err error
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			log.Warn(ctx, "UncompleteHabit: invalid date format", dateStr)
			utils.BadRequestResponse(c, "invalid date format, use YYYY-MM-DD")
			return
		}
	}

	log.Info(ctx, "UncompleteHabit: removing completion", habitID, "for date", date.Format("2006-01-02"))

	err := h.service.Uncomplete(c, habitID, date)
	if err != nil {
		log.Error(ctx, "UncompleteHabit: failed to uncomplete habit", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Completion removed successfully")
	log.Info(ctx, "UncompleteHabit: completion removed successfully", habitID)
}

// GetCompletions retrieves completion history for a habit
// GET /api/v1/habits/:habitid/completions?limit=30
func (h *HabitHandler) GetCompletions(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	// Parse limit query parameter (default to 30)
	limit := 30
	if limitStr := c.Query("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	log.Info(ctx, "GetCompletions: retrieving completions", habitID, "limit:", limit)

	completions, err := h.service.Completions(c, habitID, limit)
	if err != nil {
		log.Error(ctx, "GetCompletions: failed to retrieve completions", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, completions)
	log.Info(ctx, "GetCompletions: retrieved completions successfully", len(completions))
}

// =============================================================================
// STATISTICS HANDLERS
// =============================================================================

// GetHabitStats retrieves statistics for a habit
// GET /api/v1/habits/:habitid/stats
func (h *HabitHandler) GetHabitStats(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	log.Info(ctx, "GetHabitStats: calculating statistics", habitID)

	// Calculate streak
	streak, err := h.service.Streak(c, habitID)
	if err != nil {
		if err == types.ErrHabitNotFound {
			log.Warn(ctx, "GetHabitStats: habit not found", habitID)
			utils.NotFoundResponse(c, "Habit")
			return
		}
		log.Error(ctx, "GetHabitStats: failed to calculate streak", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	// Calculate 7-day completion rate
	rate7, err := h.service.CompletionRate(c, habitID, 7)
	if err != nil {
		log.Warn(ctx, "GetHabitStats: failed to calculate 7-day rate", err)
		rate7 = 0
	}

	// Calculate 30-day completion rate
	rate30, err := h.service.CompletionRate(c, habitID, 30)
	if err != nil {
		log.Warn(ctx, "GetHabitStats: failed to calculate 30-day rate", err)
		rate30 = 0
	}

	// Get total completions
	totalCompletions, err := h.service.TotalCompletions(c, habitID)
	if err != nil {
		log.Warn(ctx, "GetHabitStats: failed to get total completions", err)
		totalCompletions = 0
	}

	stats := types.HabitStatsResponse{
		CurrentStreak:     streak,
		CompletionRate7d:  rate7,
		CompletionRate30d: rate30,
		TotalCompletions:  int(totalCompletions),
	}

	utils.DataResponse(c, http.StatusOK, stats)
	log.Info(ctx, "GetHabitStats: statistics calculated successfully", habitID)
}

// =============================================================================
// ANCHOR HANDLERS
// =============================================================================

// CreateHabitAnchorRequest creates an anchor request for a habit
// POST /api/v1/habits/:habitid/anchor-request
func (h *HabitHandler) CreateHabitAnchorRequest(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	var requestBody struct {
		AnchorID string `json:"anchor_id" binding:"required"`
	}

	if !utils.BindJSONOrAbort(c, &requestBody) {
		return
	}

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckCreateHabitAnchor(userID)
	}) {
		return
	}

	log.Info(ctx, "CreateHabitAnchorRequest: creating anchor request for habit", habitID)

	anchorRequest, err := database.CreateHabitAnchorRequest(c, habitID, requestBody.AnchorID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "habit not found or you don't have permission" {
			utils.NotFoundResponse(c, "Habit")
			return
		}
		if errMsg == "you are not friends with this user" {
			utils.ErrorResponse(c, http.StatusForbidden, errMsg)
			return
		}
		if errMsg == "anchor request already exists for this habit" || errMsg == "this user is already an anchor for this habit" {
			utils.ConflictResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, anchorRequest)
	log.Info(ctx, "CreateHabitAnchorRequest: anchor request created successfully")

	// Send push notification to the anchor
	go sendHabitAnchorRequestNotification(anchorRequest)
}

// RemoveHabitAnchor removes an anchor from a habit
// DELETE /api/v1/habits/:habitid/anchor
func (h *HabitHandler) RemoveHabitAnchor(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	log.Info(ctx, "RemoveHabitAnchor: removing anchor from habit", habitID)

	err := database.RemoveHabitAnchor(c, habitID)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "habit not found or you don't have permission" {
			utils.NotFoundResponse(c, "Habit")
			return
		}
		if errMsg == "no anchor assigned to this habit" {
			utils.BadRequestResponse(c, errMsg)
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Anchor removed successfully")
	log.Info(ctx, "RemoveHabitAnchor: anchor removed successfully")
}

// sendHabitAnchorRequestNotification sends a notification when a habit anchor request is created
func sendHabitAnchorRequestNotification(anchorRequest types.AnchorRequest) {
	ctx := context.Background()
	// Get requester's name
	requester, err := database.GetUserByID(anchorRequest.RequesterID)
	if err != nil {
		log.Info(ctx, "Failed to get requester info for notification:", err)
		return
	}

	requesterName := requester.FirstName + " " + requester.LastName

	// Rides the anchor_request toggle (mapped in FAndAPrefs.Lookup). FriendID is
	// the requester so a per-friend mute applies from the recipient's side.
	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           anchorRequest.AnchorID,
		Category:         notifications.CategoryFAndA,
		NotificationType: "habit_anchor_request",
		DedupKey:         anchorRequest.RequestID,
		FriendID:         anchorRequest.RequesterID,
		Title:            "New Habit Anchor Request",
		Body:             requesterName + " wants you to be their habit anchor",
		Data: map[string]any{
			"request_id": anchorRequest.RequestID,
			"habit_id":   anchorRequest.HabitID,
		},
	})
}
