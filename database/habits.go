package database

import (
	"dr/server/log"
	"dr/server/types"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// =============================================================================
// HABIT CRUD OPERATIONS
// =============================================================================

// CreateHabit creates a new habit in the database
func CreateHabit(c *gin.Context, habit types.Habit) (types.Habit, error) {
	ctx := c.Request.Context()

	// Extract user_id from context (set by auth middleware)
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "CreateHabit: missing or invalid user_id")
		return types.Habit{}, types.ErrHabitNotFound
	}

	// Set the user ID
	habit.UserID = userID.(string)

	// Log habit creation attempt
	log.Info(ctx, "Creating habit", habit.Title)

	// Create the habit in database
	tx := db.Create(&habit)
	if tx.Error != nil {
		log.Error(ctx, "CreateHabit: failed to create habit", tx.Error)
		return types.Habit{}, tx.Error
	}

	// Retrieve the created habit with full details
	var createdHabit types.Habit
	if err := db.Where("habit_id = ?", habit.HabitID).First(&createdHabit).Error; err != nil {
		log.Error(ctx, "CreateHabit: failed to retrieve created habit", err)
		return types.Habit{}, err
	}

	log.Info(ctx, "Habit created successfully", createdHabit.HabitID)
	return createdHabit, nil
}

// GetHabit retrieves a single habit by ID
func GetHabit(c *gin.Context, habitID string) types.Habit {
	ctx := c.Request.Context()

	var habit types.Habit
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetHabit: missing or invalid user_id")
		return types.Habit{}
	}

	// Query with user_id check to ensure user can only access their own habits
	result := db.Where("habit_id = ? AND user_id = ?", habitID, userID).First(&habit)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			log.Warn(ctx, "GetHabit: habit not found", habitID)
		} else {
			log.Error(ctx, "GetHabit: database error", result.Error)
		}
		return types.Habit{}
	}

	log.Info(ctx, "Retrieved habit", habitID)
	return habit
}

// GetAllHabits retrieves all habits for a user
func GetAllHabits(c *gin.Context, includeArchived bool) ([]types.Habit, error) {
	ctx := c.Request.Context()

	var habits []types.Habit
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetAllHabits: missing or invalid user_id")
		return nil, types.ErrHabitNotFound
	}

	// Build query
	query := db.Where("user_id = ?", userID)
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}

	// Execute query with ordering
	err := query.Order("created_at DESC").Find(&habits).Error
	if err != nil {
		log.Error(ctx, "GetAllHabits: failed to retrieve habits", err)
		return nil, err
	}

	log.Info(ctx, "Retrieved all habits for user", len(habits))
	return habits, nil
}

// UpdateHabit updates an existing habit
func UpdateHabit(c *gin.Context, habitID string, updatedHabit types.Habit) (types.Habit, error) {
	ctx := c.Request.Context()

	var habit types.Habit
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "UpdateHabit: missing or invalid user_id")
		return types.Habit{}, types.ErrHabitNotFound
	}

	// First, retrieve the existing habit
	result := db.Where("habit_id = ? AND user_id = ?", habitID, userID).First(&habit)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			log.Warn(ctx, "UpdateHabit: habit not found", habitID)
			return types.Habit{}, types.ErrHabitNotFound
		}
		log.Error(ctx, "UpdateHabit: database error", result.Error)
		return types.Habit{}, result.Error
	}

	log.Info(ctx, "Updating habit", habitID)

	// Update the habit using GORM's Save (which updates all fields)
	err := db.Model(&habit).Updates(updatedHabit).Error
	if err != nil {
		log.Error(ctx, "UpdateHabit: failed to update habit", err)
		return types.Habit{}, err
	}

	// Retrieve the updated habit
	db.Where("habit_id = ?", habitID).First(&habit)

	log.Info(ctx, "Habit updated successfully", habitID)
	return habit, nil
}

// DeleteHabit deletes a habit (cascades to completions via DB constraint)
func DeleteHabit(c *gin.Context, habitID string) error {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "DeleteHabit: missing or invalid user_id")
		return types.ErrHabitNotFound
	}

	log.Info(ctx, "Deleting habit", habitID)

	// Delete the habit (completions will cascade via DB constraints)
	result := db.Where("habit_id = ? AND user_id = ?", habitID, userID).Delete(&types.Habit{})
	if result.Error != nil {
		log.Error(ctx, "DeleteHabit: database error", result.Error)
		return result.Error
	}

	// Check if any rows were affected
	if result.RowsAffected == 0 {
		log.Warn(ctx, "DeleteHabit: habit not found", habitID)
		return types.ErrHabitNotFound
	}

	log.Info(ctx, "Habit deleted successfully", habitID)
	return nil
}

// ArchiveHabit soft-archives a habit
func ArchiveHabit(c *gin.Context, habitID string) error {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "ArchiveHabit: missing or invalid user_id")
		return types.ErrHabitNotFound
	}

	log.Info(ctx, "Archiving habit", habitID)

	// Update the archived field
	result := db.Model(&types.Habit{}).
		Where("habit_id = ? AND user_id = ?", habitID, userID).
		Update("archived", true)

	if result.Error != nil {
		log.Error(ctx, "ArchiveHabit: database error", result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		log.Warn(ctx, "ArchiveHabit: habit not found", habitID)
		return types.ErrHabitNotFound
	}

	log.Info(ctx, "Habit archived successfully", habitID)
	return nil
}

// =============================================================================
// HABIT QUERY OPERATIONS
// =============================================================================

// GetHabitsForDate retrieves all habits due on a specific date with completion status
func GetHabitsForDate(c *gin.Context, date time.Time) ([]types.HabitWithStatus, error) {
	ctx := c.Request.Context()

	var results []types.HabitWithStatus

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetHabitsForDate: missing or invalid user_id")
		return nil, types.ErrHabitNotFound
	}

	// Normalize date to start of day in UTC
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	log.Info(ctx, "Getting habits for date", dateOnly.Format("2006-01-02"))

	// Get all active habits for user
	var habits []types.Habit
	err := db.Where("user_id = ? AND archived = ?", userID, false).Find(&habits).Error
	if err != nil {
		log.Error(ctx, "GetHabitsForDate: failed to retrieve habits", err)
		return nil, err
	}

	// Filter to those due on this date and get completion status
	for _, habit := range habits {
		if !habit.IsDueOn(dateOnly) {
			continue
		}

		var completion types.HabitCompletion
		isCompleted := false
		var completedAt *time.Time

		// Check if habit was completed on this date
		err := db.Where("habit_id = ? AND date = ?", habit.HabitID, dateOnly).First(&completion).Error
		if err == nil {
			isCompleted = true
			completedAt = &completion.CompletedAt
		}

		results = append(results, types.HabitWithStatus{
			Habit:       habit,
			IsCompleted: isCompleted,
			CompletedAt: completedAt,
		})
	}

	// Decorate with per-occurrence step progress. Two extra queries total, not
	// one per habit. Items without steps get zeroes, which is exactly what
	// pre-existing habits report, so older clients see no change.
	if len(results) > 0 {
		ids := make([]string, 0, len(results))
		for _, r := range results {
			ids = append(ids, r.HabitID)
		}
		progress := OccurrenceProgressFor(ids, dateOnly)
		for i := range results {
			if p, ok := progress[results[i].HabitID]; ok {
				results[i].StepsTotal = p.Total
				results[i].StepsCompleted = p.Completed
			}
		}
	}

	log.Info(ctx, "Found habits due on date", len(results))
	return results, nil
}

// GetHabitsForDateRange retrieves habits and completions for a date range (for calendar views)
func GetHabitsForDateRange(c *gin.Context, startDate, endDate time.Time) (map[string][]types.HabitWithStatus, error) {
	ctx := c.Request.Context()

	results := make(map[string][]types.HabitWithStatus)

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetHabitsForDateRange: missing or invalid user_id")
		return nil, types.ErrHabitNotFound
	}

	log.Info(ctx, "Getting habits for date range", startDate.Format("2006-01-02"), "to", endDate.Format("2006-01-02"))

	// Get all active habits
	var habits []types.Habit
	err := db.Where("user_id = ? AND archived = ?", userID, false).Find(&habits).Error
	if err != nil {
		log.Error(ctx, "GetHabitsForDateRange: failed to retrieve habits", err)
		return nil, err
	}

	// Get all completions in date range
	var completions []types.HabitCompletion
	err = db.Where("user_id = ? AND date >= ? AND date <= ?", userID, startDate, endDate).Find(&completions).Error
	if err != nil {
		log.Error(ctx, "GetHabitsForDateRange: failed to retrieve completions", err)
		return nil, err
	}

	// Build completion lookup map: date -> habitID -> completion
	completionMap := make(map[string]map[string]types.HabitCompletion)
	for _, c := range completions {
		dateKey := c.Date.Format("2006-01-02")
		if completionMap[dateKey] == nil {
			completionMap[dateKey] = make(map[string]types.HabitCompletion)
		}
		completionMap[dateKey][c.HabitID] = c
	}

	// Iterate through each date in the range
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dateKey := d.Format("2006-01-02")
		var dayHabits []types.HabitWithStatus

		for _, habit := range habits {
			// Check if habit is due on this date
			if !habit.IsDueOn(d) {
				continue
			}

			hws := types.HabitWithStatus{
				Habit:       habit,
				IsCompleted: false,
			}

			// Check if completed on this date
			if dayCompletions, ok := completionMap[dateKey]; ok {
				if completion, ok := dayCompletions[habit.HabitID]; ok {
					hws.IsCompleted = true
					hws.CompletedAt = &completion.CompletedAt
				}
			}

			dayHabits = append(dayHabits, hws)
		}

		results[dateKey] = dayHabits
	}

	log.Info(ctx, "Retrieved habits for date range", len(results), "days")
	return results, nil
}

// =============================================================================
// COMPLETION OPERATIONS
// =============================================================================

// CompleteHabit marks a habit as complete for a specific date
func CompleteHabit(c *gin.Context, habitID string, date time.Time, notes string) (*types.HabitCompletion, error) {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "CompleteHabit: missing or invalid user_id")
		return nil, types.ErrHabitNotFound
	}

	// Normalize date to start of day in UTC
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	log.Info(ctx, "Completing habit", habitID, "for date", dateOnly.Format("2006-01-02"))

	// Ownership check. Completion previously trusted the habit_id alone, so a
	// caller who knew another user's habit UUID could write a completion
	// against it (the row would even be stamped with the caller's user_id).
	habit, _, ownErr := habitForUser(c, habitID)
	if ownErr != nil {
		log.Warn(ctx, "CompleteHabit: habit not found or not owned by caller", habitID)
		return nil, ownErr
	}

	// Check if already completed
	var existing types.HabitCompletion
	err := db.Where("habit_id = ? AND date = ?", habitID, dateOnly).First(&existing).Error
	if err == nil {
		log.Warn(ctx, "CompleteHabit: habit already completed for this date", habitID)
		return nil, types.ErrAlreadyCompleted
	}

	// Create completion record
	completion := types.HabitCompletion{
		HabitID:     habitID,
		UserID:      userID.(string),
		CompletedAt: time.Now(),
		Date:        dateOnly,
		Notes:       notes,
	}

	err = db.Create(&completion).Error
	if err != nil {
		log.Error(ctx, "CompleteHabit: failed to create completion", err)
		return nil, err
	}

	// For a stepped item, completing the parent directly brings its checklist
	// with it, so the occurrence never shows "done" over unchecked steps.
	// Roadmap does the same via CompleteAllSteps. Items with no steps skip this
	// entirely and behave exactly as they always have.
	if HabitHasSteps(habitID) {
		if err := MarkOccurrenceStepsComplete(habit, dateOnly); err != nil {
			log.Error(ctx, "CompleteHabit: failed to complete occurrence steps", err)
		}
	}

	log.Info(ctx, "Habit completion created successfully", completion.CompletionID)
	return &completion, nil
}

// UncompleteHabit removes a completion for a specific date
func UncompleteHabit(c *gin.Context, habitID string, date time.Time) error {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "UncompleteHabit: missing or invalid user_id")
		return types.ErrHabitNotFound
	}

	// Normalize date to start of day in UTC
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	log.Info(ctx, "Uncompleting habit", habitID, "for date", dateOnly.Format("2006-01-02"))

	// Delete the completion
	result := db.Where("habit_id = ? AND user_id = ? AND date = ?", habitID, userID, dateOnly).
		Delete(&types.HabitCompletion{})

	if result.Error != nil {
		log.Error(ctx, "UncompleteHabit: database error", result.Error)
		return result.Error
	}

	// Reopening the parent reopens its checklist, mirroring the roadmap reverse
	// cascade where un-completing a task leaves no step still claiming done.
	if HabitHasSteps(habitID) {
		if err := ResetOccurrenceSteps(habitID, dateOnly); err != nil {
			log.Error(ctx, "UncompleteHabit: failed to reset occurrence steps", err)
		}
	}

	log.Info(ctx, "Habit uncompleted successfully", habitID)
	return nil
}

// GetCompletions retrieves completion history for a habit
func GetCompletions(c *gin.Context, habitID string, limit int) ([]types.HabitCompletion, error) {
	ctx := c.Request.Context()

	var completions []types.HabitCompletion
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetCompletions: missing or invalid user_id")
		return nil, types.ErrHabitNotFound
	}

	log.Info(ctx, "Getting completions for habit", habitID)

	err := db.Where("habit_id = ? AND user_id = ?", habitID, userID).
		Order("date DESC").
		Limit(limit).
		Find(&completions).Error

	if err != nil {
		log.Error(ctx, "GetCompletions: database error", err)
		return nil, err
	}

	log.Info(ctx, "Retrieved completions", len(completions))
	return completions, err
}

// =============================================================================
// STATISTICS OPERATIONS
// =============================================================================

// GetStreak calculates the current streak for a habit
func GetStreak(c *gin.Context, habitID string) (int, error) {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetStreak: missing or invalid user_id")
		return 0, types.ErrHabitNotFound
	}

	log.Info(ctx, "Calculating streak for habit", habitID)

	// Get the habit
	var habit types.Habit
	err := db.Where("habit_id = ? AND user_id = ?", habitID, userID).First(&habit).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warn(ctx, "GetStreak: habit not found", habitID)
			return 0, types.ErrHabitNotFound
		}
		log.Error(ctx, "GetStreak: database error", err)
		return 0, err
	}

	// Get completions ordered by date descending
	var completions []types.HabitCompletion
	err = db.Where("habit_id = ?", habitID).Order("date DESC").Find(&completions).Error
	if err != nil {
		log.Error(ctx, "GetStreak: failed to retrieve completions", err)
		return 0, err
	}

	if len(completions) == 0 {
		log.Info(ctx, "GetStreak: no completions found", 0)
		return 0, nil
	}

	streak := 0
	expectedDate := time.Now()

	// Normalize to start of day
	expectedDate = time.Date(expectedDate.Year(), expectedDate.Month(), expectedDate.Day(), 0, 0, 0, 0, expectedDate.Location())

	// Calculate streak by going backwards from today
	for _, completion := range completions {
		completionDate := time.Date(completion.Date.Year(), completion.Date.Month(), completion.Date.Day(), 0, 0, 0, 0, completion.Date.Location())

		// Find the next expected date (going backwards)
		for !habit.IsDueOn(expectedDate) && expectedDate.After(habit.StartDate) {
			expectedDate = expectedDate.AddDate(0, 0, -1)
		}

		if completionDate.Equal(expectedDate) {
			streak++
			expectedDate = expectedDate.AddDate(0, 0, -1)
		} else if completionDate.Before(expectedDate) {
			// Missed a day, streak broken
			break
		}
	}

	log.Info(ctx, "Calculated streak", streak)
	return streak, nil
}

// GetCompletionRate calculates completion rate for a habit over a period (in days)
func GetCompletionRate(c *gin.Context, habitID string, days int) (float64, error) {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetCompletionRate: missing or invalid user_id")
		return 0, types.ErrHabitNotFound
	}

	log.Info(ctx, "Calculating completion rate for habit", habitID, "over", days, "days")

	// Get the habit
	var habit types.Habit
	err := db.Where("habit_id = ? AND user_id = ?", habitID, userID).First(&habit).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warn(ctx, "GetCompletionRate: habit not found", habitID)
			return 0, types.ErrHabitNotFound
		}
		log.Error(ctx, "GetCompletionRate: database error", err)
		return 0, err
	}

	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -days)

	// Count expected completions based on habit's recurrence pattern
	expectedCount := 0
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		if habit.IsDueOn(d) {
			expectedCount++
		}
	}

	if expectedCount == 0 {
		log.Info(ctx, "GetCompletionRate: no expected completions in period", 0)
		return 0, nil
	}

	// Count actual completions
	var actualCount int64
	err = db.Model(&types.HabitCompletion{}).
		Where("habit_id = ? AND date >= ? AND date <= ?", habitID, startDate, endDate).
		Count(&actualCount).Error
	if err != nil {
		log.Error(ctx, "GetCompletionRate: failed to count completions", err)
		return 0, err
	}

	rate := float64(actualCount) / float64(expectedCount)
	log.Info(ctx, "Calculated completion rate", rate)
	return rate, nil
}

// GetTotalCompletions returns the total number of completions for a habit
func GetTotalCompletions(c *gin.Context, habitID string) (int64, error) {
	ctx := c.Request.Context()

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Error(ctx, "GetTotalCompletions: missing or invalid user_id")
		return 0, types.ErrHabitNotFound
	}

	log.Info(ctx, "Getting total completions for habit", habitID)

	var count int64
	err := db.Model(&types.HabitCompletion{}).
		Where("habit_id = ? AND user_id = ?", habitID, userID).
		Count(&count).Error

	if err != nil {
		log.Error(ctx, "GetTotalCompletions: database error", err)
		return 0, err
	}

	log.Info(ctx, "Total completions", count)
	return count, nil
}
