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

func UpdateUser(user_id string) types.User {
	var user types.User
	id, _ := strconv.Atoi(user_id)

	log.Printf("Fetching User: %s\n", user_id)
	db.First(&user, id)

	log.Println(user)

	return user
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

// func CreateTask(task types.Task) *gorm.DB {

// 	var task_id uint = 345

// 	newTask := types.Task{
// 		TaskID: task_id,
// 		UserID: 6,
// 		Title:       task.Title,
// 		Description: task.Description,
// 		Level:       task.Level,
// 		StartDate:   task.StartDate,
// 		EndDate:     task.EndDate,
// 		Completed:   task.Completed,
// 		ParentTaskID: &task_id,
// 	}

// 	tx := db.Create(&newTask)
// 	return tx
// }

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

	// Update fields (example: update goal, description, days, completed, start_date)
	roadmap.Goal = rm.Goal
	roadmap.Description = rm.Description
	roadmap.Days = rm.Days
	roadmap.Completed = rm.Completed
	roadmap.StartDate = rm.StartDate

	// If you want to update milestones/tasks/steps, handle them here as well

	// Save the updated roadmap
	saveResult := db.Save(&roadmap)
	if saveResult.Error != nil {
		log.Println("Error updating roadmap:", saveResult.Error)
	}

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

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).First(&milestone)

	if result.Error != nil {
		log.Println("Error fetching milestone:", result.Error)
		return types.Milestone{}
	}

	milestone.SubGoal = updatedMilestone.SubGoal
	milestone.Description = updatedMilestone.Description
	milestone.Day = updatedMilestone.Day
	milestone.Reached = updatedMilestone.Reached

	saveResult := db.Save(&milestone)
	if saveResult.Error != nil {
		log.Println("Error updating milestone:", saveResult.Error)
	}

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

	result := db.Where("user_id = ?", user_id).Where("task_id = ?", taskID).First(&task)

	if result.Error != nil {
		log.Println("Error fetching task:", result.Error)
		return types.Task{}
	}

	task.Title = updatedTask.Title
	task.Description = updatedTask.Description
	task.Day = updatedTask.Day
	task.Completed = updatedTask.Completed

	saveResult := db.Save(&task)
	if saveResult.Error != nil {
		log.Println("Error updating task:", saveResult.Error)
	}

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
