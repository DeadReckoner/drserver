package database

import (
	"errors"
	"time"

	"dr/server/log"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Multi-step recurring habits/tasks.
//
// The roadmap equivalent (database/task.go UpdateStep) keeps completion state
// on the Step row itself, because a roadmap task happens exactly once. A habit
// repeats, so the same trick would leak yesterday's checkmarks into today.
// Instead:
//
//	habit_steps            step DEFINITIONS on the template
//	habit_step_completions step STATE for one occurrence, (habit_id, date)
//
// Occurrence rows are materialised lazily -- an occurrence nobody has touched
// has no rows and is projected off the current template as all-incomplete.
// That is what makes "the next occurrence starts fresh" fall out for free, and
// what lets a template edit reach future occurrences without rewriting past
// ones.
//
// Parent completion is the same fact it has always been: a habit_completions
// row for (habit_id, date). Steps drive that row; they do not replace it. So
// streaks, stats, history and the notification scheduler keep reading exactly
// what they read before.

// occurrenceDate normalises to UTC midnight, matching CompleteHabit and
// GetHabitsForDate so every part of the habit model agrees on what a date is.
func occurrenceDate(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
}

// habitForUser loads a habit and proves the caller owns it. Every exported
// function here goes through it first: without this an authenticated user
// could toggle steps on someone else's habit just by knowing its UUID.
func habitForUser(c *gin.Context, habitID string) (types.Habit, string, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Habit{}, "", types.ErrHabitNotFound
	}
	uid, _ := userID.(string)

	var habit types.Habit
	err := db.Where("habit_id = ? AND user_id = ?", habitID, uid).First(&habit).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.Habit{}, "", types.ErrHabitNotFound
		}
		return types.Habit{}, "", err
	}
	return habit, uid, nil
}

// =============================================================================
// TEMPLATE STEP DEFINITIONS
// =============================================================================

// GetHabitSteps returns the template's step definitions in display order.
func GetHabitSteps(c *gin.Context, habitID string) ([]types.HabitStep, error) {
	if _, _, err := habitForUser(c, habitID); err != nil {
		return nil, err
	}

	steps := []types.HabitStep{}
	if err := db.Where("habit_id = ?", habitID).
		Order("number ASC, created_at ASC").
		Find(&steps).Error; err != nil {
		return nil, err
	}
	return steps, nil
}

// CreateHabitStep appends a step definition. When Number is not supplied the
// step lands at the end, so callers can just POST descriptions in order.
func CreateHabitStep(c *gin.Context, habitID string, req types.CreateHabitStepRequest) (types.HabitStep, error) {
	habit, userID, err := habitForUser(c, habitID)
	if err != nil {
		return types.HabitStep{}, err
	}

	number := req.Number
	if number == 0 {
		var maxNumber *int
		db.Model(&types.HabitStep{}).
			Where("habit_id = ?", habitID).
			Select("MAX(number)").
			Scan(&maxNumber)
		if maxNumber != nil {
			number = *maxNumber + 1
		} else {
			number = 1
		}
	}

	step := types.HabitStep{
		HabitID:     habitID,
		UserID:      userID,
		Description: req.Description,
		Number:      number,
	}
	if err := db.Create(&step).Error; err != nil {
		return types.HabitStep{}, err
	}

	// A new step must show up on the occurrence the user is looking at, not
	// only on tomorrow's. Same rule the other template fields follow.
	if err := syncActiveOccurrence(db, habit, time.Now()); err != nil {
		log.Error(c.Request.Context(), "CreateHabitStep: active occurrence sync failed", err)
	}

	return step, nil
}

// UpdateHabitStep edits a step DEFINITION (label/order only -- completion is
// per-occurrence and goes through SetHabitStepState).
func UpdateHabitStep(c *gin.Context, habitID, stepID string, req types.UpdateHabitStepRequest) (types.HabitStep, error) {
	habit, _, err := habitForUser(c, habitID)
	if err != nil {
		return types.HabitStep{}, err
	}

	var step types.HabitStep
	if err := db.Where("habit_id = ? AND habit_step_id = ?", habitID, stepID).First(&step).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.HabitStep{}, types.ErrHabitStepNotFound
		}
		return types.HabitStep{}, err
	}

	updates := map[string]any{}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Number != nil {
		updates["number"] = *req.Number
	}
	if len(updates) > 0 {
		if err := db.Model(&step).Updates(updates).Error; err != nil {
			return types.HabitStep{}, err
		}
	}

	if err := syncActiveOccurrence(db, habit, time.Now()); err != nil {
		log.Error(c.Request.Context(), "UpdateHabitStep: active occurrence sync failed", err)
	}

	db.Where("habit_step_id = ?", stepID).First(&step)
	return step, nil
}

// DeleteHabitStep removes a step definition.
//
// Historical occurrences keep their record of it: habit_step_completions.
// habit_step_id is ON DELETE SET NULL, so past rows survive with their snapshot
// label intact. Only the *active* occurrence drops the step, via the resync.
func DeleteHabitStep(c *gin.Context, habitID, stepID string) error {
	habit, _, err := habitForUser(c, habitID)
	if err != nil {
		return err
	}

	res := db.Where("habit_id = ? AND habit_step_id = ?", habitID, stepID).Delete(&types.HabitStep{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return types.ErrHabitStepNotFound
	}

	today := occurrenceDate(time.Now())
	return db.Transaction(func(tx *gorm.DB) error {
		// Drop the now-orphaned row from the active occurrence only; completed
		// and past occurrences keep theirs as history.
		if !occurrenceIsComplete(tx, habitID, today) {
			if err := tx.Where("habit_id = ? AND date = ? AND habit_step_id IS NULL", habitID, today).
				Delete(&types.HabitStepCompletion{}).Error; err != nil {
				return err
			}
		}
		if err := syncActiveOccurrence(tx, habit, time.Now()); err != nil {
			return err
		}
		// Deleting the last incomplete step can be what finally completes the
		// occurrence -- and deleting every step must restore plain
		// single-action completion rather than stranding it complete.
		return reconcileOccurrence(tx, habit, today)
	})
}

// ReorderHabitSteps assigns Number from the given order in one transaction, so
// a drag gesture cannot half-apply. Completion state is untouched: it is keyed
// by habit_step_id, never by position.
func ReorderHabitSteps(c *gin.Context, habitID string, stepIDs []string) ([]types.HabitStep, error) {
	habit, _, err := habitForUser(c, habitID)
	if err != nil {
		return nil, err
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		for i, id := range stepIDs {
			res := tx.Model(&types.HabitStep{}).
				Where("habit_id = ? AND habit_step_id = ?", habitID, id).
				Update("number", i+1)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return types.ErrHabitStepNotFound
			}
		}
		return syncActiveOccurrence(tx, habit, time.Now())
	})
	if err != nil {
		return nil, err
	}

	steps := []types.HabitStep{}
	if err := db.Where("habit_id = ?", habitID).Order("number ASC").Find(&steps).Error; err != nil {
		return nil, err
	}
	return steps, nil
}

// =============================================================================
// OCCURRENCE STEP STATE
// =============================================================================

// occurrenceIsComplete reports whether the parent occurrence is marked done,
// i.e. whether a habit_completions row exists for (habit_id, date).
func occurrenceIsComplete(tx *gorm.DB, habitID string, date time.Time) bool {
	var n int64
	tx.Model(&types.HabitCompletion{}).
		Where("habit_id = ? AND date = ?", habitID, date).
		Count(&n)
	return n > 0
}

// materialiseOccurrence writes the template's steps into the occurrence as
// all-incomplete, if and only if the occurrence has no rows yet.
//
// This is the moment a "virtual" occurrence becomes a real, snapshotted one.
// Doing it lazily (on first write) rather than eagerly (on read) keeps GETs
// side-effect free and means an untouched future occurrence automatically
// tracks the latest template.
func materialiseOccurrence(tx *gorm.DB, habit types.Habit, date time.Time) error {
	var existing int64
	if err := tx.Model(&types.HabitStepCompletion{}).
		Where("habit_id = ? AND date = ?", habit.HabitID, date).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	var templates []types.HabitStep
	if err := tx.Where("habit_id = ?", habit.HabitID).
		Order("number ASC, created_at ASC").
		Find(&templates).Error; err != nil {
		return err
	}
	if len(templates) == 0 {
		return nil
	}

	rows := make([]types.HabitStepCompletion, 0, len(templates))
	for _, t := range templates {
		stepID := t.HabitStepID
		rows = append(rows, types.HabitStepCompletion{
			HabitID:     habit.HabitID,
			UserID:      habit.UserID,
			Date:        date,
			HabitStepID: &stepID,
			Description: t.Description,
			Number:      t.Number,
			Status:      string(types.StepStatusIncomplete),
			Completed:   false,
		})
	}

	// DoNothing on the (habit_id, date, habit_step_id) unique index: two
	// devices materialising the same occurrence at once converge instead of
	// duplicating the checklist.
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// syncActiveOccurrence pushes template edits onto the occurrence in progress.
//
// The rule (and the least surprising one, since every other habit field edit
// takes effect immediately): future occurrences always follow the template
// because they are projected from it, and the *current, still-incomplete*
// occurrence is brought back in line. Completed occurrences are never touched,
// so history stays immutable.
func syncActiveOccurrence(tx *gorm.DB, habit types.Habit, now time.Time) error {
	date := occurrenceDate(now)

	// Nothing materialised yet -> nothing to sync; it will project off the
	// template when it is first read or written.
	var existing []types.HabitStepCompletion
	if err := tx.Where("habit_id = ? AND date = ?", habit.HabitID, date).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}
	// Never rewrite a finished occurrence.
	if occurrenceIsComplete(tx, habit.HabitID, date) {
		return nil
	}

	var templates []types.HabitStep
	if err := tx.Where("habit_id = ?", habit.HabitID).
		Order("number ASC, created_at ASC").
		Find(&templates).Error; err != nil {
		return err
	}

	byStepID := map[string]types.HabitStepCompletion{}
	for _, e := range existing {
		if e.HabitStepID != nil {
			byStepID[*e.HabitStepID] = e
		}
	}

	live := map[string]bool{}
	for _, t := range templates {
		live[t.HabitStepID] = true
		stepID := t.HabitStepID

		if cur, ok := byStepID[stepID]; ok {
			// Refresh the snapshot; preserve this occurrence's completion.
			if cur.Description != t.Description || cur.Number != t.Number {
				if err := tx.Model(&types.HabitStepCompletion{}).
					Where("habit_step_completion_id = ?", cur.HabitStepCompletionID).
					Updates(map[string]any{"description": t.Description, "number": t.Number}).Error; err != nil {
					return err
				}
			}
			continue
		}

		// Step added to the template since this occurrence materialised.
		row := types.HabitStepCompletion{
			HabitID:     habit.HabitID,
			UserID:      habit.UserID,
			Date:        date,
			HabitStepID: &stepID,
			Description: t.Description,
			Number:      t.Number,
			Status:      string(types.StepStatusIncomplete),
			Completed:   false,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}

	// Step removed from the template -> drop it from the active occurrence.
	for _, e := range existing {
		if e.HabitStepID == nil || live[*e.HabitStepID] {
			continue
		}
		if err := tx.Where("habit_step_completion_id = ?", e.HabitStepCompletionID).
			Delete(&types.HabitStepCompletion{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// occurrenceShouldComplete mirrors shouldCompleteTask exactly: an occurrence
// with no steps never auto-completes (it keeps the single-action path), and one
// with steps completes when nothing is left "incomplete" -- "skipped" counts as
// done, just as it does for roadmap steps.
func occurrenceShouldComplete(tx *gorm.DB, habitID string, date time.Time) bool {
	var total int64
	tx.Model(&types.HabitStepCompletion{}).
		Where("habit_id = ? AND date = ?", habitID, date).
		Count(&total)
	if total == 0 {
		return false
	}

	var incomplete int64
	tx.Model(&types.HabitStepCompletion{}).
		Where("habit_id = ? AND date = ? AND status = ?", habitID, date, string(types.StepStatusIncomplete)).
		Count(&incomplete)
	return incomplete == 0
}

// reconcileOccurrence makes the parent completion agree with the steps.
//
// This is the single place parent completion is created or removed by step
// activity, which is what keeps side effects exactly-once: the insert relies on
// the habit_completions (habit_id, date) unique index and DoNothing, so N
// concurrent "final step" taps produce one row, one completion, one streak
// bump. Reopening is the mirror image and matches how unchecking a roadmap step
// un-completes its task.
func reconcileOccurrence(tx *gorm.DB, habit types.Habit, date time.Time) error {
	var stepCount int64
	if err := tx.Model(&types.HabitStepCompletion{}).
		Where("habit_id = ? AND date = ?", habit.HabitID, date).
		Count(&stepCount).Error; err != nil {
		return err
	}
	// No steps on this occurrence: leave the parent exactly as the plain
	// complete/uncomplete endpoints left it.
	if stepCount == 0 {
		return nil
	}

	if occurrenceShouldComplete(tx, habit.HabitID, date) {
		completion := types.HabitCompletion{
			HabitID:     habit.HabitID,
			UserID:      habit.UserID,
			CompletedAt: time.Now(),
			Date:        date,
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&completion).Error
	}

	return tx.Where("habit_id = ? AND date = ?", habit.HabitID, date).
		Delete(&types.HabitCompletion{}).Error
}

// SetHabitStepState toggles one step on one occurrence and reconciles the
// parent, all inside one transaction so a rapid double-tap cannot interleave
// into a half-applied state.
func SetHabitStepState(c *gin.Context, habitID, stepID string, date time.Time, status string) (types.HabitOccurrenceSteps, error) {
	habit, _, err := habitForUser(c, habitID)
	if err != nil {
		return types.HabitOccurrenceSteps{}, err
	}

	day := occurrenceDate(date)

	err = db.Transaction(func(tx *gorm.DB) error {
		// The step has to exist on the template to be toggled. Historical rows
		// whose template step was deleted are read-only by construction.
		var template types.HabitStep
		if err := tx.Where("habit_id = ? AND habit_step_id = ?", habitID, stepID).First(&template).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return types.ErrHabitStepNotFound
			}
			return err
		}

		if err := materialiseOccurrence(tx, habit, day); err != nil {
			return err
		}

		completed := status == string(types.StepStatusCompleted)
		updates := map[string]any{
			"status":    status,
			"completed": completed,
		}
		if completed {
			now := time.Now()
			updates["completed_at"] = &now
		} else {
			updates["completed_at"] = nil
		}

		res := tx.Model(&types.HabitStepCompletion{}).
			Where("habit_id = ? AND date = ? AND habit_step_id = ?", habitID, day, stepID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// materialiseOccurrence is a no-op when rows already exist, so a
			// step added to the template after this occurrence materialised
			// has no row yet. Bring it in, then apply.
			if err := syncActiveOccurrence(tx, habit, date); err != nil {
				return err
			}
			if err := tx.Model(&types.HabitStepCompletion{}).
				Where("habit_id = ? AND date = ? AND habit_step_id = ?", habitID, day, stepID).
				Updates(updates).Error; err != nil {
				return err
			}
		}

		return reconcileOccurrence(tx, habit, day)
	})
	if err != nil {
		return types.HabitOccurrenceSteps{}, err
	}

	return GetOccurrenceSteps(c, habitID, day)
}

// GetOccurrenceSteps returns the step state of one occurrence.
//
// Materialised rows win (they are the historical truth for that date). With no
// rows, the current template is projected as all-incomplete -- which is what
// makes every not-yet-started occurrence, including the next recurrence, begin
// fresh without any reset job.
func GetOccurrenceSteps(c *gin.Context, habitID string, date time.Time) (types.HabitOccurrenceSteps, error) {
	if _, _, err := habitForUser(c, habitID); err != nil {
		return types.HabitOccurrenceSteps{}, err
	}
	day := occurrenceDate(date)

	out := types.HabitOccurrenceSteps{
		HabitID:     habitID,
		Date:        day.Format("2006-01-02"),
		Steps:       []types.HabitOccurrenceStep{},
		IsCompleted: occurrenceIsComplete(db, habitID, day),
	}

	var rows []types.HabitStepCompletion
	if err := db.Where("habit_id = ? AND date = ?", habitID, day).
		Order("number ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return types.HabitOccurrenceSteps{}, err
	}

	if len(rows) > 0 {
		for _, r := range rows {
			stepID := ""
			if r.HabitStepID != nil {
				stepID = *r.HabitStepID
			}
			out.Steps = append(out.Steps, types.HabitOccurrenceStep{
				HabitStepID: stepID,
				Description: r.Description,
				Number:      r.Number,
				Status:      r.Status,
				Completed:   r.Completed,
				CompletedAt: r.CompletedAt,
			})
			if r.IsDone() {
				out.StepsCompleted++
			}
		}
		out.StepsTotal = len(out.Steps)
		return out, nil
	}

	var templates []types.HabitStep
	if err := db.Where("habit_id = ?", habitID).
		Order("number ASC, created_at ASC").
		Find(&templates).Error; err != nil {
		return types.HabitOccurrenceSteps{}, err
	}
	for _, t := range templates {
		out.Steps = append(out.Steps, types.HabitOccurrenceStep{
			HabitStepID: t.HabitStepID,
			Description: t.Description,
			Number:      t.Number,
			Status:      string(types.StepStatusIncomplete),
			Completed:   false,
		})
	}
	out.StepsTotal = len(out.Steps)
	return out, nil
}

// =============================================================================
// INTEGRATION WITH DIRECT COMPLETE / UNCOMPLETE
// =============================================================================

// MarkOccurrenceStepsComplete marks every step of an occurrence done.
//
// Called when a stepped item is completed through the plain complete endpoint.
// Roadmap allows the same shortcut (UpdateTask sets completed directly, and
// CompleteAllSteps exists precisely to keep steps consistent when it happens),
// so habits match rather than inventing a stricter rule -- but the steps are
// brought along so the checklist never contradicts the parent.
func MarkOccurrenceStepsComplete(habit types.Habit, date time.Time) error {
	day := occurrenceDate(date)
	return db.Transaction(func(tx *gorm.DB) error {
		if err := materialiseOccurrence(tx, habit, day); err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&types.HabitStepCompletion{}).
			Where("habit_id = ? AND date = ? AND status = ?", habit.HabitID, day, string(types.StepStatusIncomplete)).
			Updates(map[string]any{
				"status":       string(types.StepStatusCompleted),
				"completed":    true,
				"completed_at": &now,
			}).Error
	})
}

// ResetOccurrenceSteps returns every step of an occurrence to incomplete, the
// mirror of MarkOccurrenceStepsComplete for the uncomplete path. Matches the
// roadmap reverse cascade, where un-completing the parent leaves nothing
// claiming to be done underneath it.
func ResetOccurrenceSteps(habitID string, date time.Time) error {
	day := occurrenceDate(date)
	return db.Model(&types.HabitStepCompletion{}).
		Where("habit_id = ? AND date = ?", habitID, day).
		Updates(map[string]any{
			"status":       string(types.StepStatusIncomplete),
			"completed":    false,
			"completed_at": nil,
		}).Error
}

// HabitHasSteps reports whether a template defines any steps. Used to keep the
// stepless path byte-for-byte what it was before this feature.
func HabitHasSteps(habitID string) bool {
	var n int64
	db.Model(&types.HabitStep{}).Where("habit_id = ?", habitID).Count(&n)
	return n > 0
}

// OccurrenceProgress is the (completed, total) pair for one occurrence, used to
// decorate list responses without fetching every checklist.
type OccurrenceProgress struct {
	Total     int
	Completed int
}

// OccurrenceProgressFor returns per-habit step progress for a single date in
// two queries, so the today/date/range list endpoints stay O(1) in round trips
// rather than O(habits).
func OccurrenceProgressFor(habitIDs []string, date time.Time) map[string]OccurrenceProgress {
	out := map[string]OccurrenceProgress{}
	if len(habitIDs) == 0 {
		return out
	}
	day := occurrenceDate(date)

	// Materialised occurrences: count real state.
	type row struct {
		HabitID string
		Total   int
		Done    int
	}
	var rows []row
	db.Model(&types.HabitStepCompletion{}).
		Select("habit_id, COUNT(*) as total, COUNT(*) FILTER (WHERE status <> 'incomplete') as done").
		Where("habit_id IN ? AND date = ?", habitIDs, day).
		Group("habit_id").
		Scan(&rows)
	for _, r := range rows {
		out[r.HabitID] = OccurrenceProgress{Total: r.Total, Completed: r.Done}
	}

	// Untouched occurrences: project the template, all incomplete.
	var missing []string
	for _, id := range habitIDs {
		if _, ok := out[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		type trow struct {
			HabitID string
			Total   int
		}
		var trows []trow
		db.Model(&types.HabitStep{}).
			Select("habit_id, COUNT(*) as total").
			Where("habit_id IN ?", missing).
			Group("habit_id").
			Scan(&trows)
		for _, r := range trows {
			out[r.HabitID] = OccurrenceProgress{Total: r.Total, Completed: 0}
		}
	}

	return out
}
