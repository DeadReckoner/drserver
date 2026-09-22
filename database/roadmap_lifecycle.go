package database

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// scheduleDateExpr is the SQL that places a milestone or task on
// startDay + (day - dayZero) calendar days, at ScheduleAnchorHour local time.
// It takes three placeholders, in order: the start day as YYYY-MM-DD, dayZero,
// and the owner's IANA timezone.
//
// The arithmetic runs on a local `timestamp` and only converts to timestamptz
// at the very end. That is what keeps a DST transition inside the roadmap's
// span from dragging dates onto a neighbouring day, and it is the same reason
// RealignRoadmapDates builds its shift the same way — adding INTERVAL '1 day'
// to a timestamptz instead would add exactly 24h and drift the local hour.
//
// GREATEST("day" - dayZero, 0) is types.ScheduleOffset expressed in SQL: a
// stray low day can't schedule work before the roadmap starts.
func scheduleDateExpr() string {
	return fmt.Sprintf(
		`(?::date + GREATEST("day" - ?, 0) * INTERVAL '1 day' + INTERVAL '%d hours') AT TIME ZONE ?`,
		types.ScheduleAnchorHour,
	)
}

// scheduleRoadmapDates writes `date` onto every milestone and task of a roadmap
// from its stored Day offset, as two set-based UPDATEs.
//
// Both callers used to load every row and issue one UPDATE per row, which made
// starting or restarting a 90-task roadmap cost hundreds of sequential round
// trips with the transaction held open across all of them. The Day offsets are
// already in the rows, so Postgres can do the whole rebase without the data
// making a trip through Go.
//
// Takes a *gorm.DB so it can run inside a caller's transaction.
func scheduleRoadmapDates(tx *gorm.DB, roadmapID string, startDate time.Time, dayZero int, loc *time.Location) error {
	if loc == nil {
		loc = time.UTC
	}
	startDay := startDate.In(loc).Format("2006-01-02")
	tz := loc.String()
	expr := scheduleDateExpr()

	if err := tx.Exec(
		"UPDATE milestones SET date = "+expr+" WHERE roadmap_id = ?",
		startDay, dayZero, tz, roadmapID,
	).Error; err != nil {
		return err
	}
	return tx.Exec(
		"UPDATE tasks SET date = "+expr+" WHERE roadmap_id = ?",
		startDay, dayZero, tz, roadmapID,
	).Error
}

// StartRoadmapSchedule puts a roadmap on the calendar: it records the start date
// and dates every milestone and task from its Day offset, in one transaction.
//
// This is the write half of PUT /roadmaps/:id/start. The handler keeps the
// ownership check and the response, and passes the start day and dayZero in
// because it already holds the loaded roadmap those come from.
func StartRoadmapSchedule(roadmapID string, startDate time.Time, dayZero int, loc *time.Location) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		// The start date has to land before RecomputeAndSaveStatuses runs, which
		// is what lets the status computation see the roadmap as started.
		if err := tx.Model(&types.Roadmap{}).
			Where("roadmap_id = ?", roadmapID).
			Update("start_date", startDate).Error; err != nil {
			return err
		}
		return scheduleRoadmapDates(tx, roadmapID, startDate, dayZero, loc)
	}); err != nil {
		return err
	}

	// Carried over from the UpdateRoadmap call this replaced: a roadmap whose
	// milestones are all already reached is marked complete on start.
	//
	// NOTE: shouldCompleteRoadmap counts *unreached* milestones, so a roadmap
	// with no milestones at all also satisfies it. That is pre-existing
	// behavior, preserved deliberately rather than quietly changed by a
	// performance refactor.
	if shouldCompleteRoadmap(roadmapID) {
		return db.Model(&types.Roadmap{}).
			Where("roadmap_id = ?", roadmapID).
			Update("completed", true).Error
	}
	return nil
}

// RestartRoadmap resets a roadmap to start fresh from today.
// It resets all completion states and recalculates dates from today.
func RestartRoadmap(c *gin.Context, roadmapID string) (types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).
		Preload("Phases.Milestones.Tasks.Steps").
		Preload("Milestones.Tasks.Steps").
		First(&roadmap)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, errors.New("roadmap not found")
		}
		return types.Roadmap{}, result.Error
	}

	if roadmap.StartDate == nil {
		return types.Roadmap{}, errors.New("roadmap has not been started")
	}

	// Anchor the restart to the owner's calendar day, not the server's UTC day —
	// a user restarting on their evening is still restarting today. Resolved
	// before the transaction opens so the lookup isn't a read inside it.
	loc := resolveUserLocation(roadmap.UserID)
	startDate := types.ScheduleDayIn(types.Now(), loc)

	// Day is an offset from the start day, so rebase onto the plan's earliest
	// day; otherwise a plan numbered from day 1 restarts tomorrow. Read from the
	// preloaded roadmap, which carries every milestone and task.
	dayZero := roadmap.MinScheduledDay()

	// Begin transaction
	tx := db.Begin()
	if tx.Error != nil {
		return types.Roadmap{}, tx.Error
	}

	// Reset roadmap
	if err := tx.Model(&types.Roadmap{}).Where("roadmap_id = ?", roadmapID).Updates(map[string]interface{}{
		"completed":  false,
		"start_date": startDate,
		"paused_at":  nil,
	}).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	// Reset all milestones
	if err := tx.Model(&types.Milestone{}).Where("roadmap_id = ?", roadmapID).Updates(map[string]interface{}{
		"reached": false,
		"status":  string(types.StatusNotStarted),
	}).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	// Reset all tasks
	if err := tx.Model(&types.Task{}).Where("roadmap_id = ?", roadmapID).Updates(map[string]interface{}{
		"completed": false,
		"status":    string(types.StatusNotStarted),
	}).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	// Reset all steps
	var taskIDs []string
	tx.Model(&types.Task{}).Where("roadmap_id = ?", roadmapID).Pluck("task_id", &taskIDs)
	if len(taskIDs) > 0 {
		if err := tx.Model(&types.Step{}).Where("task_id IN ?", taskIDs).Updates(map[string]interface{}{
			"completed": false,
			"status":    string(types.StepStatusIncomplete),
		}).Error; err != nil {
			tx.Rollback()
			return types.Roadmap{}, err
		}
	}

	// Recalculate milestone and task dates from the new start date. Two
	// statements, no row-by-row loop — and unlike the loop this replaced, a
	// failure here aborts the restart instead of being silently dropped.
	if err := scheduleRoadmapDates(tx, roadmapID, startDate, dayZero, loc); err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	// Reset phases status
	if err := tx.Model(&types.Phase{}).Where("roadmap_id = ?", roadmapID).Update("status", string(types.StatusNotStarted)).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	if err := tx.Commit().Error; err != nil {
		return types.Roadmap{}, err
	}

	// Recompute and save all statuses
	if err := RecomputeAndSaveStatuses(roadmapID); err != nil {
		log.Printf("RestartRoadmap: failed to recompute statuses: %v", err)
	}

	// Fetch and return the updated roadmap
	return GetRoadmapFullByID(roadmapID)
}

// ShiftRoadmapDates shifts all dates in a roadmap by the specified number of days.
// Positive days shift forward, negative days shift backward.
func ShiftRoadmapDates(c *gin.Context, roadmapID string, days int) (types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).First(&roadmap)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, errors.New("roadmap not found")
		}
		return types.Roadmap{}, result.Error
	}

	if roadmap.StartDate == nil {
		return types.Roadmap{}, errors.New("roadmap has not been started")
	}

	// Begin transaction
	tx := db.Begin()
	if tx.Error != nil {
		return types.Roadmap{}, tx.Error
	}

	// Shift roadmap start date
	newStartDate := roadmap.StartDate.AddDate(0, 0, days)
	if err := tx.Model(&types.Roadmap{}).Where("roadmap_id = ?", roadmapID).Update("start_date", newStartDate).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	// Shift all milestone dates
	if err := tx.Exec("UPDATE milestones SET date = date + INTERVAL '1 day' * ? WHERE roadmap_id = ? AND date IS NOT NULL", days, roadmapID).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	// Shift all task dates
	if err := tx.Exec("UPDATE tasks SET date = date + INTERVAL '1 day' * ? WHERE roadmap_id = ? AND date IS NOT NULL", days, roadmapID).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, err
	}

	if err := tx.Commit().Error; err != nil {
		return types.Roadmap{}, err
	}

	// Recompute statuses
	if err := RecomputeAndSaveStatuses(roadmapID); err != nil {
		log.Printf("ShiftRoadmapDates: failed to recompute statuses: %v", err)
	}

	return GetRoadmapFullByID(roadmapID)
}

// PauseRoadmap freezes a roadmap by setting the paused_at timestamp.
func PauseRoadmap(c *gin.Context, roadmapID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).First(&roadmap)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return errors.New("roadmap not found")
		}
		return result.Error
	}

	if roadmap.StartDate == nil {
		return errors.New("roadmap has not been started")
	}

	if roadmap.PausedAt != nil {
		return errors.New("roadmap is already paused")
	}

	now := time.Now()
	return db.Model(&types.Roadmap{}).Where("roadmap_id = ?", roadmapID).Update("paused_at", now).Error
}

// ResumeRoadmap resumes a paused roadmap by shifting dates forward
// by the duration it was paused.
func ResumeRoadmap(c *gin.Context, roadmapID string) (types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).First(&roadmap)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, errors.New("roadmap not found")
		}
		return types.Roadmap{}, result.Error
	}

	if roadmap.PausedAt == nil {
		return types.Roadmap{}, errors.New("roadmap is not paused")
	}

	// Calculate days paused
	daysPaused := int(time.Since(*roadmap.PausedAt).Hours() / 24)
	if daysPaused < 1 {
		daysPaused = 1 // At least 1 day shift
	}

	// Clear paused_at
	if err := db.Model(&types.Roadmap{}).Where("roadmap_id = ?", roadmapID).Update("paused_at", nil).Error; err != nil {
		return types.Roadmap{}, err
	}

	// Shift all dates forward by the paused duration
	return ShiftRoadmapDates(c, roadmapID, daysPaused)
}

// ArchiveRoadmap sets the archived flag to true.
func ArchiveRoadmap(c *gin.Context, roadmapID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Model(&types.Roadmap{}).
		Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).
		Update("archived", true)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("roadmap not found")
	}
	return nil
}

// RestoreRoadmap sets the archived flag to false.
// If startFromToday is true, it also restarts the roadmap.
func RestoreRoadmap(c *gin.Context, roadmapID string, startFromToday bool) (types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).First(&roadmap)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, errors.New("roadmap not found")
		}
		return types.Roadmap{}, result.Error
	}

	if !roadmap.Archived {
		return types.Roadmap{}, errors.New("roadmap is not archived")
	}

	// Unarchive the roadmap
	if err := db.Model(&types.Roadmap{}).Where("roadmap_id = ?", roadmapID).Update("archived", false).Error; err != nil {
		return types.Roadmap{}, err
	}

	// If start_from_today, restart the roadmap
	if startFromToday && roadmap.StartDate != nil {
		return RestartRoadmap(c, roadmapID)
	}

	return GetRoadmapFullByID(roadmapID)
}

// GenerateShareToken generates a secure share token for a roadmap.
// Returns the token and the full shareable URL.
func GenerateShareToken(c *gin.Context, roadmapID string) (string, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return "", errors.New("missing or invalid user_id")
	}

	// Verify roadmap exists and belongs to user
	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).First(&roadmap)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return "", errors.New("roadmap not found")
		}
		return "", result.Error
	}

	// Generate a secure random token (24 bytes = 32 chars base64)
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Save the token
	if err := db.Model(&types.Roadmap{}).Where("roadmap_id = ?", roadmapID).Update("share_token", token).Error; err != nil {
		return "", err
	}

	return token, nil
}

// GetRoadmapByShareToken returns a roadmap by its share token.
// This is used for public access to shared roadmaps.
func GetRoadmapByShareToken(shareToken string) (types.Roadmap, error) {
	var roadmap types.Roadmap
	result := db.Where("share_token = ?", shareToken).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		First(&roadmap)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, errors.New("shared roadmap not found")
		}
		return types.Roadmap{}, result.Error
	}

	return roadmap, nil
}

// CopyRoadmapForUser creates a copy of a shared roadmap for the authenticated user.
// The copy is independent of the original and starts in an unstarted state.
func CopyRoadmapForUser(c *gin.Context, shareToken string) (types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	// Get the original roadmap
	original, err := GetRoadmapByShareToken(shareToken)
	if err != nil {
		return types.Roadmap{}, err
	}

	// Create a deep copy with new UUIDs
	newRoadmap := types.Roadmap{
		RoadmapID:         uuid.NewString(),
		UserID:            userID.(string),
		Goal:              original.Goal,
		Description:       original.Description,
		Days:              original.Days,
		Completed:         false,
		StartDate:         nil, // Not started
		Type:              original.Type,
		CurrentSkillLevel: original.CurrentSkillLevel,
		GenerationStatus:  string(types.GenerationStatusCompleted),
		Archived:          false,
		PausedAt:          nil,
		ShareToken:        nil,
	}

	// Copy phases and their milestones
	for _, phase := range original.Phases {
		newPhase := types.Phase{
			PhaseID:       uuid.NewString(),
			RoadmapID:     newRoadmap.RoadmapID,
			Name:          phase.Name,
			Description:   phase.Description,
			OrderIndex:    phase.OrderIndex,
			DurationWeeks: phase.DurationWeeks,
			Status:        string(types.StatusNotStarted),
		}

		// Copy milestones in phase
		for _, milestone := range phase.Milestones {
			newMilestone := copyMilestone(milestone, newRoadmap.RoadmapID, userID.(string), &newPhase.PhaseID)
			newPhase.Milestones = append(newPhase.Milestones, newMilestone)
		}

		newRoadmap.Phases = append(newRoadmap.Phases, newPhase)
	}

	// Copy top-level milestones
	for _, milestone := range original.Milestones {
		newMilestone := copyMilestone(milestone, newRoadmap.RoadmapID, userID.(string), nil)
		newRoadmap.Milestones = append(newRoadmap.Milestones, newMilestone)
	}

	// Save the new roadmap
	if err := db.Create(&newRoadmap).Error; err != nil {
		return types.Roadmap{}, err
	}

	return GetRoadmapFullByID(newRoadmap.RoadmapID)
}

// copyMilestone creates a deep copy of a milestone with new UUIDs.
func copyMilestone(original types.Milestone, roadmapID, userID string, phaseID *string) types.Milestone {
	newMilestone := types.Milestone{
		MilestoneID:       uuid.NewString(),
		RoadmapID:         roadmapID,
		UserID:            userID,
		PhaseID:           phaseID,
		SubGoal:           original.SubGoal,
		Description:       original.Description,
		Day:               original.Day,
		Reached:           false,
		Date:              nil,
		Status:            string(types.StatusNotStarted),
		AcceptanceTest:    original.AcceptanceTest,
		DurationDaysMin:   original.DurationDaysMin,
		DurationDaysMax:   original.DurationDaysMax,
		OrderIndex:        original.OrderIndex,
		ResourcesForecast: original.ResourcesForecast,
	}

	// Copy spikes
	for _, spike := range original.Spikes {
		newMilestone.Spikes = append(newMilestone.Spikes, types.Spike{
			SpikeID:     uuid.NewString(),
			MilestoneID: newMilestone.MilestoneID,
			Description: spike.Description,
			TimeboxDays: spike.TimeboxDays,
		})
	}

	// Copy risks
	for _, risk := range original.Risks {
		newMilestone.Risks = append(newMilestone.Risks, types.Risk{
			RiskID:      uuid.NewString(),
			MilestoneID: newMilestone.MilestoneID,
			Description: risk.Description,
			Severity:    risk.Severity,
		})
	}

	// Copy tasks
	for _, task := range original.Tasks {
		newTask := types.Task{
			TaskID:       uuid.NewString(),
			RoadmapID:    roadmapID,
			MilestoneID:  newMilestone.MilestoneID,
			UserID:       userID,
			Title:        task.Title,
			Description:  task.Description,
			Status:       string(types.StatusNotStarted),
			Rationale:    task.Rationale,
			EstMinutes:   task.EstMinutes,
			Dependencies: task.Dependencies,
			Confidence:   task.Confidence,
			Day:          task.Day,
			Completed:    false,
			Date:         nil,
		}

		// Copy steps
		for _, step := range task.Steps {
			newTask.Steps = append(newTask.Steps, types.Step{
				StepID:      uuid.NewString(),
				TaskID:      newTask.TaskID,
				Description: step.Description,
				Completed:   false,
				Status:      string(types.StepStatusIncomplete),
				Number:      step.Number,
			})
		}

		newMilestone.Tasks = append(newMilestone.Tasks, newTask)
	}

	return newMilestone
}

// RealignRoadmapDates realigns incomplete task dates so the first incomplete task
// is scheduled for today + offsetDays. Only affects incomplete tasks and their
// associated milestones. Completed tasks retain their original dates.
//
// "Today" is the owner's calendar day, not the server's UTC day, so realigning
// in the evening doesn't land the first task on tomorrow.
//
// Shifted dates are re-anchored to types.ScheduleAnchorHour in the owner's
// timezone. That happens even when the shift is zero, which heals rows written
// before dates were anchored — they carry whatever wall-clock time the roadmap
// was started at, which can read as the neighbouring day on the client.
//
// offsetDays: 0 = first task due today, 2 = first task due in 2 days, etc.
func RealignRoadmapDates(c *gin.Context, roadmapID string, offsetDays int) (types.Roadmap, int, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Roadmap{}, 0, errors.New("missing or invalid user_id")
	}

	var roadmap types.Roadmap
	result := db.Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).First(&roadmap)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, 0, errors.New("roadmap not found")
		}
		return types.Roadmap{}, 0, result.Error
	}

	if roadmap.StartDate == nil {
		return types.Roadmap{}, 0, errors.New("roadmap has not been started")
	}

	// Find first incomplete task ordered by date
	var firstTask types.Task
	result = db.Where("roadmap_id = ? AND completed = ?", roadmapID, false).
		Where("date IS NOT NULL").
		Order("date ASC").
		First(&firstTask)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.Roadmap{}, 0, errors.New("no incomplete tasks found")
		}
		return types.Roadmap{}, 0, result.Error
	}

	// Calculate days to shift, counting calendar days in the owner's timezone so
	// the boundary is their local midnight rather than the server's.
	loc := resolveUserLocation(roadmap.UserID)
	targetDate := types.ScheduleDayIn(types.Now(), loc).AddDate(0, 0, offsetDays)
	daysToShift := types.CalendarDaysBetween(*firstTask.Date, targetDate, loc)

	// Begin transaction
	tx := db.Begin()
	if tx.Error != nil {
		return types.Roadmap{}, 0, tx.Error
	}

	// Shift by whole days in the owner's local wall time, then re-anchor to the
	// scheduling hour. Converting out of timestamptz first keeps a DST boundary
	// inside the shifted range from dragging dates onto a neighbouring day, and
	// the re-anchor normalizes any legacy time-of-day.
	shiftExpr := fmt.Sprintf(
		"(date_trunc('day', (date AT TIME ZONE ?) + (? * INTERVAL '1 day')) + INTERVAL '%d hours') AT TIME ZONE ?",
		types.ScheduleAnchorHour,
	)
	tz := loc.String()

	// Shift only incomplete task dates
	if err := tx.Exec(
		"UPDATE tasks SET date = "+shiftExpr+" WHERE roadmap_id = ? AND completed = false AND date IS NOT NULL",
		tz, daysToShift, tz, roadmapID,
	).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, 0, err
	}

	// Shift milestone dates for milestones that have incomplete tasks
	// First, get milestone IDs that have at least one incomplete task
	var milestoneIDs []string
	if err := tx.Model(&types.Task{}).
		Where("roadmap_id = ? AND completed = false", roadmapID).
		Distinct().
		Pluck("milestone_id", &milestoneIDs).Error; err != nil {
		tx.Rollback()
		return types.Roadmap{}, 0, err
	}

	if len(milestoneIDs) > 0 {
		if err := tx.Exec(
			"UPDATE milestones SET date = "+shiftExpr+" WHERE milestone_id IN ? AND date IS NOT NULL",
			tz, daysToShift, tz, milestoneIDs,
		).Error; err != nil {
			tx.Rollback()
			return types.Roadmap{}, 0, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return types.Roadmap{}, 0, err
	}

	// Recompute statuses
	if err := RecomputeAndSaveStatuses(roadmapID); err != nil {
		log.Printf("RealignRoadmapDates: failed to recompute statuses: %v", err)
	}

	rm, err := GetRoadmapFullByID(roadmapID)
	return rm, daysToShift, err
}

// RevokeShareToken removes the share token from a roadmap.
func RevokeShareToken(c *gin.Context, roadmapID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Model(&types.Roadmap{}).
		Where("user_id = ? AND roadmap_id = ?", userID, roadmapID).
		Update("share_token", nil)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("roadmap not found")
	}
	return nil
}

// GetRoadmapFullByID retrieves a full roadmap by ID without user validation.
// Used internally after operations that need to return the updated roadmap.
func GetRoadmapFullByID(roadmapID string) (types.Roadmap, error) {
	var roadmap types.Roadmap
	result := db.Where("roadmap_id = ?", roadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones.Spikes").
		Preload("Phases.Milestones.Risks").
		Preload("Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Spikes").
		Preload("Milestones.Risks").
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		First(&roadmap)

	if result.Error != nil {
		return types.Roadmap{}, result.Error
	}

	// Compute all statuses, judging overdue against the owner's local day.
	roadmap.ComputeAllStatusesInLocation(resolveUserLocation(roadmap.UserID))

	return roadmap, nil
}
