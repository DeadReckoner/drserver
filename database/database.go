package database

import (
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

// Database init.
func Database() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using environment variables")
	}

	dsn := os.Getenv("DATABASE_URL")
	log.Println("DB: ", dsn)
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	// Connect to the database using GORM
	for attempts := 1; attempts <= 5; attempts++ {
		db, err = gorm.Open(postgres.New(postgres.Config{

			DSN:                  dsn,
			PreferSimpleProtocol: true, // disables statement preparation
		}), &gorm.Config{})

		if err == nil {
			break
		}
		log.Printf("DB connection failed (attempt %d): %v", attempts, err)
		time.Sleep(time.Duration(attempts) * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to the database after retries: %v", err)
	}

	log.Println("Successfully connected to Supabase PostgreSQL")
}

// Create user in Database (upsert: create if not exists, return if exists)
func CreateUser(c *gin.Context, user types.User) *gorm.DB {
	// Handle error later
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return &gorm.DB{Error: errors.New("missing or invalid user_id")}
	}

	user.UserID = user_id.(string)

	// Check if user already exists
	var existingUser types.User
	result := db.Where("user_id = ?", user.UserID).First(&existingUser)

	if result.Error == nil {
		// User exists, return success without error
		return &gorm.DB{Error: nil}
	}

	if result.Error != gorm.ErrRecordNotFound {
		// Some other error occurred
		return &gorm.DB{Error: result.Error}
	}

	// User doesn't exist, create new user
	user.CreatedAt = time.Now()
	tx := db.Create(&user)
	return tx
}

// GetUser from Database
func GetUser(c *gin.Context) types.User {

	var user types.User
	user_id, _ := c.Get("user_id")

	db.Where("user_id = ?", user_id).First(&user)

	log.Println(user)

	return user
}

func UpdateUser(user_id string, user types.User) *gorm.DB {
	var existingUser types.User
	result := db.Where("user_id = ?", user_id).First(&existingUser)

	if result.Error == nil {
		// User exists, return success without error
		tx := db.Model(&existingUser).Updates(user)
		return tx
	} else {
		// Some error occurred
		return &gorm.DB{Error: result.Error}
	}
}

func DeleteUser(c *gin.Context) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Where("user_id = ?", user_id).Delete(&types.User{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// GetTasks by id
func GetTasks(id string) []types.Task {
	var tasks []types.Task
	id_val, _ := strconv.Atoi(id)

	result := db.Debug().Where("user_id = ?", id_val).Find(&tasks)

	if result.Error != nil {
		log.Println("Error:", result.Error)
	} else {
		log.Printf("Found %d tasks\n", len(tasks))
	}

	return tasks
}

func CreateRoadmap(c *gin.Context, rm types.Roadmap) (types.Roadmap, error) {
	// Handle error later
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	userIDStr := user_id.(string)

	// Ensure user exists in database before creating roadmap
	var existingUser types.User
	userResult := db.Where("user_id = ?", userIDStr).First(&existingUser)
	if userResult.Error == gorm.ErrRecordNotFound {
		// User doesn't exist, create a minimal user record
		user := types.User{
			UserID:    userIDStr,
			CreatedAt: time.Now(),
		}
		if err := db.Create(&user).Error; err != nil {
			return types.Roadmap{}, errors.New("failed to create user: " + err.Error())
		}
	} else if userResult.Error != nil {
		return types.Roadmap{}, errors.New("failed to check user existence: " + userResult.Error.Error())
	}

	rm.UserID = userIDStr
	for i := range rm.Milestones {
		rm.Milestones[i].UserID = rm.UserID
		for j := range rm.Milestones[i].Tasks {

			rm.Milestones[i].Tasks[j].UserID = rm.UserID

		}
	}

	tx := db.Create(&rm)
	if tx.Error != nil {
		return types.Roadmap{}, tx.Error
	}

	var createdRoadmap types.Roadmap
	if err := db.Where("roadmap_id = ?", rm.RoadmapID).Preload("Milestones.Tasks.Steps").First(&createdRoadmap).Error; err != nil {
		return types.Roadmap{}, err
	}

	return createdRoadmap, nil
}

func GetRoadmapsByUser(c *gin.Context) []types.Roadmap {
	var roadmaps []types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmaps
	}

	// Query the roadmap table for all roadmaps with this user_id
	result := db.Where("user_id = ?", user_id).Preload("Milestones.Tasks.Steps").Find(&roadmaps)
	if result.Error != nil {
		log.Println("Error fetching roadmaps:", result.Error)
	}

	return roadmaps
}

func GetRoadmap(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	// Query the roadmap table for all roadmaps with this user_id
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Preload("Milestones.Tasks.Steps").Find(&roadmap)
	if result.Error != nil {
		log.Println("Error fetching roadmaps:", result.Error)
	}

	return roadmap
}

func UpdateRoadmap(c *gin.Context, roadmapID string, rm types.Roadmap) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	// Fetch the existing roadmap
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Preload("Milestones.Tasks.Steps").First(&roadmap)
	if result.Error != nil {
		log.Println("Error fetching roadmap:", result.Error)
		return roadmap
	}

	// Use GORM's Updates method to only update non-zero fields
	updateMap := make(map[string]interface{})
	if rm.Goal != "" {
		updateMap["goal"] = rm.Goal
	}
	if rm.Description != "" {
		updateMap["description"] = rm.Description
	}
	if rm.Days != 0 {
		updateMap["days"] = rm.Days
	}
	// Handle boolean field explicitly since Updates ignores zero values
	if rm.Completed != roadmap.Completed {
		updateMap["completed"] = rm.Completed
	}
	if !rm.StartDate.IsZero() {
		updateMap["start_date"] = rm.StartDate
	}

	if len(updateMap) > 0 {
		updateResult := db.Model(&roadmap).Updates(updateMap)
		if updateResult.Error != nil {
			log.Println("Error updating roadmap:", updateResult.Error)
			return roadmap
		}
		// Update the roadmap struct with the new values
		for key, value := range updateMap {
			switch key {
			case "goal":
				roadmap.Goal = value.(string)
			case "description":
				roadmap.Description = value.(string)
			case "days":
				roadmap.Days = value.(int)
			case "completed":
				roadmap.Completed = value.(bool)
			case "start_date":
				roadmap.StartDate = value.(time.Time)
			}
		}
	}

	// Check if all milestones are completed to auto-complete roadmap
	if shouldCompleteRoadmap(roadmap.RoadmapID) {
		db.Model(&roadmap).Update("completed", true)
		roadmap.Completed = true
	}

	// Fetch updated roadmap with associations
	db.Where("roadmap_id = ?", roadmapID).Preload("Milestones.Tasks.Steps").First(&roadmap)

	return roadmap
}

func DeleteRoadmap(c *gin.Context, roadmapID string) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	// Delete the roadmap for this user and roadmapID
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Delete(&types.Roadmap{})
	if result.Error != nil {
		log.Println("Error deleting roadmap:", result.Error)
		return result.Error
	}

	return nil
}

func GetMilestonesByRoadmap(c *gin.Context, roadmapID string) []types.Milestone {
	var milestones []types.Milestone
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return []types.Milestone{}
	}

	results := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Preload("Tasks.Steps").Find(&milestones)

	if results.Error != nil {
		log.Println("Error fetching milestones:", results.Error)
	}

	return milestones
}

func GetMilestone(c *gin.Context, milestoneID string) types.Milestone {
	var milestone types.Milestone
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}
	}

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Tasks.Steps").First(&milestone)

	if result.Error != nil {
		log.Println("Error fetching milestone:", result.Error)
	}

	return milestone
}

func UpdateMilestone(c *gin.Context, milestoneID string, updatedMilestone types.Milestone) types.Milestone {
	var milestone types.Milestone
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}
	}

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Tasks.Steps").First(&milestone)

	if result.Error != nil {
		log.Println("Error fetching milestone:", result.Error)
		return types.Milestone{}
	}

	// Use GORM's Updates method to only update non-zero fields
	updateMap := make(map[string]interface{})
	if updatedMilestone.SubGoal != "" {
		updateMap["sub_goal"] = updatedMilestone.SubGoal
	}
	if updatedMilestone.Description != "" {
		updateMap["description"] = updatedMilestone.Description
	}
	if updatedMilestone.Day != 0 {
		updateMap["day"] = updatedMilestone.Day
	}
	// Handle boolean field explicitly since Updates ignores zero values
	if updatedMilestone.Reached != milestone.Reached {
		updateMap["reached"] = updatedMilestone.Reached
	}

	if len(updateMap) > 0 {
		updateResult := db.Model(&milestone).Updates(updateMap)
		if updateResult.Error != nil {
			log.Println("Error updating milestone:", updateResult.Error)
			return types.Milestone{}
		}
		// Update the milestone struct with the new values
		for key, value := range updateMap {
			switch key {
			case "sub_goal":
				milestone.SubGoal = value.(string)
			case "description":
				milestone.Description = value.(string)
			case "day":
				milestone.Day = value.(int)
			case "reached":
				milestone.Reached = value.(bool)
			}
		}
	}

	// Check if all tasks are completed to auto-complete milestone
	if shouldCompleteMilestone(milestone.MilestoneID) {
		db.Model(&milestone).Update("reached", true)
		milestone.Reached = true
		
		// Check if this completion should trigger roadmap completion
		if shouldCompleteRoadmap(milestone.RoadmapID) {
			db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
		}
	}

	// Fetch updated milestone with associations
	db.Where("milestone_id = ?", milestoneID).Preload("Tasks.Steps").First(&milestone)

	return milestone
}

func CreateTask(c *gin.Context, task types.Task) (types.Task, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Task{}, errors.New("missing or invalid user_id")
	}

	task.UserID = user_id.(string)

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

	results := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Steps").Find(&tasks)

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

	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).Preload("Steps").First(&task)

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

	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).Preload("Steps").First(&task)

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
			}
		}
	}

	// Check if all steps are completed to auto-complete task
	if shouldCompleteTask(task.TaskID) {
		db.Model(&task).Update("completed", true)
		task.Completed = true
		
		// Check if this completion should trigger milestone completion
		if shouldCompleteMilestone(task.MilestoneID) {
			db.Model(&types.Milestone{}).Where("milestone_id = ?", task.MilestoneID).Update("reached", true)
			
			// Check if this completion should trigger roadmap completion
			var milestone types.Milestone
			db.Where("milestone_id = ?", task.MilestoneID).First(&milestone)
			if shouldCompleteRoadmap(milestone.RoadmapID) {
				db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
			}
		}
	}

	// Fetch updated task with associations
	db.Where("task_id = ?", taskID).Preload("Steps").First(&task)

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

func CreateMilestone(c *gin.Context, roadmapID string, milestone types.Milestone) (types.Milestone, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}, errors.New("missing or invalid user_id")
	}

	milestone.UserID = user_id.(string)
	milestone.RoadmapID = roadmapID

	for i := range milestone.Tasks {
		milestone.Tasks[i].UserID = milestone.UserID
		milestone.Tasks[i].MilestoneID = milestone.MilestoneID
		for j := range milestone.Tasks[i].Steps {
			milestone.Tasks[i].Steps[j].TaskID = milestone.Tasks[i].TaskID
		}
	}

	tx := db.Create(&milestone)
	if tx.Error != nil {
		return types.Milestone{}, tx.Error
	}

	var createdMilestone types.Milestone
	if err := db.Where("milestone_id = ?", milestone.MilestoneID).Preload("Tasks.Steps").First(&createdMilestone).Error; err != nil {
		return types.Milestone{}, err
	}

	return createdMilestone, nil
}

func DeleteMilestone(c *gin.Context, milestoneID string) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Delete(&types.Milestone{})
	if result.Error != nil {
		log.Println("Error deleting milestone:", result.Error)
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

	// If step was completed, check cascade completion
	if updatedStep.Completed {
		// Check if all steps are completed to auto-complete task
		if shouldCompleteTask(taskID) {
			db.Model(&types.Task{}).Where("task_id = ?", taskID).Update("completed", true)
			
			// Check if this completion should trigger milestone completion
			if shouldCompleteMilestone(task.MilestoneID) {
				db.Model(&types.Milestone{}).Where("milestone_id = ?", task.MilestoneID).Update("reached", true)
				
				// Check if this completion should trigger roadmap completion
				var milestone types.Milestone
				db.Where("milestone_id = ?", task.MilestoneID).First(&milestone)
				if shouldCompleteRoadmap(milestone.RoadmapID) {
					db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
				}
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

// Helper functions for checking completion status

// shouldCompleteTask checks if all steps in a task are completed
func shouldCompleteTask(taskID string) bool {
	var incompleteSteps int64
	db.Model(&types.Step{}).Where("task_id = ? AND completed = ?", taskID, false).Count(&incompleteSteps)
	return incompleteSteps == 0
}

// shouldCompleteMilestone checks if all tasks in a milestone are completed
func shouldCompleteMilestone(milestoneID string) bool {
	var incompleteTasks int64
	db.Model(&types.Task{}).Where("milestone_id = ? AND completed = ?", milestoneID, false).Count(&incompleteTasks)
	return incompleteTasks == 0
}

// shouldCompleteRoadmap checks if all milestones in a roadmap are completed
func shouldCompleteRoadmap(roadmapID string) bool {
	var incompleteMilestones int64
	db.Model(&types.Milestone{}).Where("roadmap_id = ? AND reached = ?", roadmapID, false).Count(&incompleteMilestones)
	return incompleteMilestones == 0
}
