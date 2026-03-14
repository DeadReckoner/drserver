package database

import (
	"errors"
	"log"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetTasks by id with optional date and roadmap filtering
func GetTasks(c *gin.Context, startDate, endDate, roadmapID string) []types.Task {
	var tasks []types.Task

	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
	}

	query := db.Debug().Where("user_id = ?", user_id)

	// Apply roadmap filtering if roadmap_id is provided
	if roadmapID != "" {
		query = query.Where("roadmap_id = ?", roadmapID)
	}

	// Apply date filtering if start_date is provided
	if startDate != "" {
		parsedStartDate, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			log.Printf("Invalid start_date format: %s", startDate)
		} else {
			if endDate != "" {
				// Date range filtering: start_date <= date <= end_date
				parsedEndDate, err := time.Parse("2006-01-02", endDate)
				if err != nil {
					log.Printf("Invalid end_date format: %s", endDate)
				} else {
					// Add one day to end_date to include the entire end date
					parsedEndDate = parsedEndDate.Add(24 * time.Hour)
					query = query.Where("date >= ? AND date < ?", parsedStartDate, parsedEndDate)
				}
			} else {
				// Single date filtering: date is on the specific day
				nextDay := parsedStartDate.Add(24 * time.Hour)
				query = query.Where("date >= ? AND date < ?", parsedStartDate, nextDay)
			}
		}
	}

	result := query.Order("day ASC").Preload("Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&tasks)

	if result.Error != nil {
		log.Println("Error:", result.Error)
	} else {
		log.Printf("Found %d tasks\n", len(tasks))
	}

	return tasks
}

func CreateTask(c *gin.Context, task types.Task) (types.Task, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Task{}, errors.New("missing or invalid user_id")
	}

	task.UserID = user_id.(string)

	// Get roadmap_id from milestone if not provided
	if task.RoadmapID == "" && task.MilestoneID != "" {
		var milestone types.Milestone
		if err := db.Where("milestone_id = ?", task.MilestoneID).First(&milestone).Error; err == nil {
			task.RoadmapID = milestone.RoadmapID
		}
	}

	for i := range task.Steps {
		task.Steps[i].TaskID = task.TaskID
	}

	tx := db.Create(&task)
	if tx.Error != nil {
		return types.Task{}, tx.Error
	}

	var createdTask types.Task
	if err := db.Where("task_id = ?", task.TaskID).Preload("Steps").First(&createdTask).Error; err != nil {
		return types.Task{}, err
	}

	return createdTask, nil
}

func GetTasksByMilestone(c *gin.Context, milestoneID string) []types.Task {
	var tasks []types.Task
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return []types.Task{}
	}

	results := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Order("day ASC").Preload("Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&tasks)

	if results.Error != nil {
		log.Println("Error fetching tasks:", results.Error)
	}

	return tasks
}

func GetTask(c *gin.Context, taskID string) types.Task {
	var task types.Task
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Task{}
	}

	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).Preload("Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&task)

	if result.Error != nil {
		log.Println("Error fetching task:", result.Error)
	}

	return task
}

func UpdateTask(c *gin.Context, taskID string, updatedTask types.Task) types.Task {
	var task types.Task
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Task{}
	}

	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).Preload("Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&task)

	if result.Error != nil {
		log.Println("Error fetching task:", result.Error)
		return types.Task{}
	}

	// Use GORM's Updates method to only update non-zero fields
	updateMap := make(map[string]interface{})
	if updatedTask.Title != "" {
		updateMap["title"] = updatedTask.Title
	}
	if updatedTask.Description != "" {
		updateMap["description"] = updatedTask.Description
	}
	if updatedTask.Day != 0 {
		updateMap["day"] = updatedTask.Day
	}
	// Handle boolean field explicitly since Updates ignores zero values
	if updatedTask.Completed != task.Completed {
		updateMap["completed"] = updatedTask.Completed
	}
	if updatedTask.Date != task.Date {
		updateMap["date"] = updatedTask.Date
	}

	if len(updateMap) > 0 {
		updateResult := db.Model(&task).Updates(updateMap)
		if updateResult.Error != nil {
			log.Println("Error updating task:", updateResult.Error)
			return types.Task{}
		}
		// Update the task struct with the new values
		for key, value := range updateMap {
			switch key {
			case "title":
				task.Title = value.(string)
			case "description":
				task.Description = value.(string)
			case "day":
				task.Day = value.(int)
			case "completed":
				task.Completed = value.(bool)
			case "date":
				task.Date = value.(*time.Time)
			}
		}

		// Recompute status if completed or date changed (Updates doesn't trigger hooks)
		if _, hasCompleted := updateMap["completed"]; hasCompleted {
			task.Status = task.ComputeStatus()
			db.Model(&task).Update("status", task.Status)
		} else if _, hasDate := updateMap["date"]; hasDate {
			task.Status = task.ComputeStatus()
			db.Model(&task).Update("status", task.Status)
		}
	}

	// Check if all steps are completed to auto-complete task
	if shouldCompleteTask(task.TaskID) {
		task.Completed = true
		task.Status = task.ComputeStatus()
		db.Model(&task).Updates(map[string]interface{}{"completed": true, "status": task.Status})

		// Check if this completion should trigger milestone completion
		if shouldCompleteMilestone(task.MilestoneID) {
			var milestone types.Milestone
			db.Where("milestone_id = ?", task.MilestoneID).First(&milestone)
			milestone.Reached = true
			milestone.Status = milestone.ComputeStatus()
			db.Model(&milestone).Updates(map[string]interface{}{"reached": true, "status": milestone.Status})

			// Check if this completion should trigger roadmap completion
			if shouldCompleteRoadmap(milestone.RoadmapID) {
				db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
			}
		}
	}

	// Fetch updated task with associations
	db.Where("task_id = ?", taskID).Preload("Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&task)

	return task
}

func DeleteTask(c *gin.Context, taskID string) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).Delete(&types.Task{})
	if result.Error != nil {
		log.Println("Error deleting task:", result.Error)
		return result.Error
	}

	return nil
}

func CreateStep(c *gin.Context, taskID string, step types.Step) (types.Step, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Step{}, errors.New("missing or invalid user_id")
	}

	var task types.Task
	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).First(&task)
	if result.Error != nil {
		return types.Step{}, errors.New("task not found or access denied")
	}

	step.TaskID = taskID

	tx := db.Create(&step)
	if tx.Error != nil {
		return types.Step{}, tx.Error
	}

	var createdStep types.Step
	if err := db.Where("step_id = ?", step.StepID).First(&createdStep).Error; err != nil {
		return types.Step{}, err
	}

	return createdStep, nil
}

func UpdateStep(c *gin.Context, taskID string, stepID string, updatedStep types.Step) types.Step {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Step{}
	}

	var task types.Task
	taskResult := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).First(&task)
	if taskResult.Error != nil {
		return types.Step{}
	}

	var step types.Step
	result := db.Where("task_id = ?", taskID).Where("step_id = ?", stepID).First(&step)

	if result.Error != nil {
		log.Println("Error fetching step:", result.Error)
		return types.Step{}
	}

	// Use GORM's Updates method to only update non-zero fields
	updateMap := make(map[string]interface{})
	if updatedStep.Description != "" {
		updateMap["description"] = updatedStep.Description
	}
	if updatedStep.Number != 0 {
		updateMap["number"] = updatedStep.Number
	}
	// Handle boolean field explicitly since Updates ignores zero values
	if updatedStep.Completed != step.Completed {
		updateMap["completed"] = updatedStep.Completed
	}

	if len(updateMap) > 0 {
		updateResult := db.Model(&step).Updates(updateMap)
		if updateResult.Error != nil {
			log.Println("Error updating step:", updateResult.Error)
			return types.Step{}
		}
		// Update the step struct with the new values
		for key, value := range updateMap {
			switch key {
			case "description":
				step.Description = value.(string)
			case "number":
				step.Number = value.(int)
			case "completed":
				step.Completed = value.(bool)
			}
		}
	}

	// Handle cascade completion/un-completion based on step state
	if updatedStep.Completed {
		// Check if all steps are completed to auto-complete task
		if shouldCompleteTask(taskID) {
			task.Completed = true
			task.Status = task.ComputeStatus()
			db.Model(&task).Updates(map[string]interface{}{"completed": true, "status": task.Status})

			// Check if this completion should trigger milestone completion
			if shouldCompleteMilestone(task.MilestoneID) {
				var milestone types.Milestone
				db.Where("milestone_id = ?", task.MilestoneID).First(&milestone)
				milestone.Reached = true
				milestone.Status = milestone.ComputeStatus()
				db.Model(&milestone).Updates(map[string]interface{}{"reached": true, "status": milestone.Status})

				// Check if this completion should trigger roadmap completion
				if shouldCompleteRoadmap(milestone.RoadmapID) {
					db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
				}
			}
		}
	} else {
		// Step was unchecked - reverse cascade: un-complete task if it was completed
		if task.Completed {
			task.Completed = false
			task.Status = task.ComputeStatus()
			db.Model(&task).Updates(map[string]interface{}{"completed": false, "status": task.Status})

			// Un-complete the milestone if it was reached
			var milestone types.Milestone
			db.Where("milestone_id = ?", task.MilestoneID).First(&milestone)
			if milestone.Reached {
				milestone.Reached = false
				milestone.Status = milestone.ComputeStatus()
				db.Model(&milestone).Updates(map[string]interface{}{"reached": false, "status": milestone.Status})

				// Un-complete the roadmap if it was completed
				db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", false)
			}
		}
	}

	return step
}

func DeleteStep(c *gin.Context, taskID string, stepID string) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	var task types.Task
	taskResult := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).First(&task)
	if taskResult.Error != nil {
		return errors.New("task not found or access denied")
	}

	result := db.Where("task_id = ?", taskID).Where("step_id = ?", stepID).Delete(&types.Step{})
	if result.Error != nil {
		log.Println("Error deleting step:", result.Error)
		return result.Error
	}

	return nil
}

// CompleteAllSteps marks all steps of a task as complete in a single operation.
// Also triggers cascade completion for task, milestone, and roadmap if applicable.
// Returns the number of steps that were marked complete.
func CompleteAllSteps(c *gin.Context, taskID string) (int64, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return 0, errors.New("missing or invalid user_id")
	}

	// Verify task exists and belongs to user
	var task types.Task
	taskResult := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).First(&task)
	if taskResult.Error != nil {
		if taskResult.Error == gorm.ErrRecordNotFound {
			return 0, errors.New("task not found")
		}
		return 0, taskResult.Error
	}

	// Mark all incomplete steps as complete
	result := db.Model(&types.Step{}).
		Where("task_id = ? AND completed = ?", taskID, false).
		Update("completed", true)

	if result.Error != nil {
		log.Println("Error completing all steps:", result.Error)
		return 0, result.Error
	}

	stepsCompleted := result.RowsAffected

	// Trigger cascade completion checks
	// Mark task as complete with updated status
	task.Completed = true
	task.Status = task.ComputeStatus()
	db.Model(&task).Updates(map[string]interface{}{"completed": true, "status": task.Status})

	// Check if this completion should trigger milestone completion
	if shouldCompleteMilestone(task.MilestoneID) {
		var milestone types.Milestone
		db.Where("milestone_id = ?", task.MilestoneID).First(&milestone)
		milestone.Reached = true
		milestone.Status = milestone.ComputeStatus()
		db.Model(&milestone).Updates(map[string]interface{}{"reached": true, "status": milestone.Status})

		// Check if this completion should trigger roadmap completion
		if shouldCompleteRoadmap(milestone.RoadmapID) {
			db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
		}
	}

	log.Printf("Completed all steps for task %s: %d steps marked complete", taskID, stepsCompleted)
	return stepsCompleted, nil
}

// UpdateAllStatuses updates the status field for all tasks and milestones based on
// their date and completed/reached fields. This is intended to be called by a daily job
// to catch status changes as dates pass (e.g., tasks becoming overdue).
// Returns the number of tasks and milestones updated.
func UpdateAllStatuses() (tasksUpdated int64, milestonesUpdated int64, err error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Update tasks: set status based on completed and date
	// Using raw SQL for efficiency since GORM hooks don't run on batch updates

	// Tasks that are completed -> "completed"
	result := db.Model(&types.Task{}).
		Where("completed = ? AND status != ?", true, string(types.StatusCompleted)).
		Update("status", string(types.StatusCompleted))
	if result.Error != nil {
		return 0, 0, result.Error
	}
	tasksUpdated += result.RowsAffected

	// Tasks with date in the past (not completed) -> "overdue"
	result = db.Model(&types.Task{}).
		Where("completed = ? AND date < ? AND status != ?", false, today, string(types.StatusOverdue)).
		Update("status", string(types.StatusOverdue))
	if result.Error != nil {
		return tasksUpdated, 0, result.Error
	}
	tasksUpdated += result.RowsAffected

	// Tasks with date = today (not completed) -> "in_progress"
	tomorrow := today.Add(24 * time.Hour)
	result = db.Model(&types.Task{}).
		Where("completed = ? AND date >= ? AND date < ? AND status != ?", false, today, tomorrow, string(types.StatusInProgress)).
		Update("status", string(types.StatusInProgress))
	if result.Error != nil {
		return tasksUpdated, 0, result.Error
	}
	tasksUpdated += result.RowsAffected

	// Tasks with date in the future (not completed) -> "not_started"
	result = db.Model(&types.Task{}).
		Where("completed = ? AND date >= ? AND status != ?", false, tomorrow, string(types.StatusNotStarted)).
		Update("status", string(types.StatusNotStarted))
	if result.Error != nil {
		return tasksUpdated, 0, result.Error
	}
	tasksUpdated += result.RowsAffected

	// Tasks with no date (not completed) -> "not_started"
	result = db.Model(&types.Task{}).
		Where("completed = ? AND date IS NULL AND status != ?", false, string(types.StatusNotStarted)).
		Update("status", string(types.StatusNotStarted))
	if result.Error != nil {
		return tasksUpdated, 0, result.Error
	}
	tasksUpdated += result.RowsAffected

	// Update milestones: same logic but using "reached" instead of "completed"

	// Milestones that are reached -> "completed"
	result = db.Model(&types.Milestone{}).
		Where("reached = ? AND status != ?", true, string(types.StatusCompleted)).
		Update("status", string(types.StatusCompleted))
	if result.Error != nil {
		return tasksUpdated, 0, result.Error
	}
	milestonesUpdated += result.RowsAffected

	// Milestones with date in the past (not reached) -> "overdue"
	result = db.Model(&types.Milestone{}).
		Where("reached = ? AND date < ? AND status != ?", false, today, string(types.StatusOverdue)).
		Update("status", string(types.StatusOverdue))
	if result.Error != nil {
		return tasksUpdated, milestonesUpdated, result.Error
	}
	milestonesUpdated += result.RowsAffected

	// Milestones with date = today (not reached) -> "in_progress"
	result = db.Model(&types.Milestone{}).
		Where("reached = ? AND date >= ? AND date < ? AND status != ?", false, today, tomorrow, string(types.StatusInProgress)).
		Update("status", string(types.StatusInProgress))
	if result.Error != nil {
		return tasksUpdated, milestonesUpdated, result.Error
	}
	milestonesUpdated += result.RowsAffected

	// Milestones with date in the future (not reached) -> "not_started"
	result = db.Model(&types.Milestone{}).
		Where("reached = ? AND date >= ? AND status != ?", false, tomorrow, string(types.StatusNotStarted)).
		Update("status", string(types.StatusNotStarted))
	if result.Error != nil {
		return tasksUpdated, milestonesUpdated, result.Error
	}
	milestonesUpdated += result.RowsAffected

	// Milestones with no date (not reached) -> "not_started"
	result = db.Model(&types.Milestone{}).
		Where("reached = ? AND date IS NULL AND status != ?", false, string(types.StatusNotStarted)).
		Update("status", string(types.StatusNotStarted))
	if result.Error != nil {
		return tasksUpdated, milestonesUpdated, result.Error
	}
	milestonesUpdated += result.RowsAffected

	log.Printf("UpdateAllStatuses: updated %d tasks and %d milestones", tasksUpdated, milestonesUpdated)
	return tasksUpdated, milestonesUpdated, nil
}
