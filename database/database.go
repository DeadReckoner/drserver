package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

	// Configure connection pool to prevent connection timeouts and exhaustion
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying SQL DB: %v", err)
	}

	// SetMaxIdleConns sets the maximum number of connections in the idle connection pool
	sqlDB.SetMaxIdleConns(10)

	// SetMaxOpenConns sets the maximum number of open connections to the database
	sqlDB.SetMaxOpenConns(100)

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused
	sqlDB.SetConnMaxLifetime(time.Hour)

	// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

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

// generateUUIDs generates UUIDs for all entities in the roadmap.
// The frontend sends empty strings for UUIDs, but GORM/PostgreSQL can't handle empty strings for UUID columns.
// We generate UUIDs in Go and set all foreign keys before inserting.
func generateUUIDs(rm *types.Roadmap) {
	rm.RoadmapID = uuid.NewString()

	if rm.MacroMap != nil {
		rm.MacroMap.MacroMapID = uuid.NewString()
		rm.MacroMap.RoadmapID = rm.RoadmapID

		for i := range rm.MacroMap.Phases {
			rm.MacroMap.Phases[i].PhaseID = uuid.NewString()
			rm.MacroMap.Phases[i].MacroMapID = rm.MacroMap.MacroMapID

			for j := range rm.MacroMap.Phases[i].MacroMilestones {
				rm.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID = uuid.NewString()
				rm.MacroMap.Phases[i].MacroMilestones[j].PhaseID = rm.MacroMap.Phases[i].PhaseID

				for k := range rm.MacroMap.Phases[i].MacroMilestones[j].Spikes {
					rm.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].SpikeID = uuid.NewString()
					rm.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].MacroMilestoneID = rm.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}

				for k := range rm.MacroMap.Phases[i].MacroMilestones[j].Risks {
					rm.MacroMap.Phases[i].MacroMilestones[j].Risks[k].RiskID = uuid.NewString()
					rm.MacroMap.Phases[i].MacroMilestones[j].Risks[k].MacroMilestoneID = rm.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}
			}
		}
	}

	for i := range rm.Milestones {
		rm.Milestones[i].MilestoneID = uuid.NewString()
		rm.Milestones[i].RoadmapID = rm.RoadmapID

		for j := range rm.Milestones[i].Tasks {
			rm.Milestones[i].Tasks[j].TaskID = uuid.NewString()
			rm.Milestones[i].Tasks[j].MilestoneID = rm.Milestones[i].MilestoneID
			rm.Milestones[i].Tasks[j].RoadmapID = rm.RoadmapID

			for k := range rm.Milestones[i].Tasks[j].Steps {
				rm.Milestones[i].Tasks[j].Steps[k].StepID = uuid.NewString()
				rm.Milestones[i].Tasks[j].Steps[k].TaskID = rm.Milestones[i].Tasks[j].TaskID
			}
		}
	}
}

func CreateRoadmap(c *gin.Context, rm types.Roadmap) (types.Roadmap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	userIDStr := user_id.(string)

	// Ensure user exists in database before creating roadmap
	var existingUser types.User
	userResult := db.Where("user_id = ?", userIDStr).First(&existingUser)
	if userResult.Error == gorm.ErrRecordNotFound {
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

	// Generate UUIDs for all entities and set foreign keys
	generateUUIDs(&rm)

	// Set user IDs for all nested entities
	rm.UserID = userIDStr
	for i := range rm.Milestones {
		rm.Milestones[i].UserID = userIDStr
		for j := range rm.Milestones[i].Tasks {
			rm.Milestones[i].Tasks[j].UserID = userIDStr
		}
	}

	tx := db.Create(&rm)
	if tx.Error != nil {
		return types.Roadmap{}, tx.Error
	}

	var createdRoadmap types.Roadmap
	query := db.Where("roadmap_id = ?", rm.RoadmapID).
		Preload("Milestones.Tasks.Steps")

	// Also preload macro map if it was created
	if rm.MacroMap != nil {
		query = query.
			Preload("MacroMap.Phases.MacroMilestones.Spikes").
			Preload("MacroMap.Phases.MacroMilestones.Risks")
	}

	if err := query.First(&createdRoadmap).Error; err != nil {
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
	result := db.Where("user_id = ?", user_id).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&roadmaps)
	if result.Error != nil {
		log.Println("Error fetching roadmaps:", result.Error)
	}

	return roadmaps
}

// GetRoadmapWithMacroMap loads only the Macro Map (strategic overview) for a roadmap
func GetRoadmapWithMacroMap(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones.Spikes").
		Preload("MacroMap.Phases.MacroMilestones.Risks").
		First(&roadmap)

	if result.Error != nil {
		log.Println("Error fetching roadmap macro map:", result.Error)
	}

	return roadmap
}

// GetRoadmapWithMacroMapAndMilestones loads both Macro Map and Milestone Map (complete view)
func GetRoadmapWithMacroMapAndMilestones(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones.Spikes").
		Preload("MacroMap.Phases.MacroMilestones.Risks").
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
		log.Println("Error fetching full roadmap:", result.Error)
	}

	return roadmap
}

func GetRoadmap(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	// Query the roadmap table for all roadmaps with this user_id
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&roadmap)
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
	if rm.StartDate != roadmap.StartDate {
		updateMap["start_date"] = rm.StartDate
	}
	// if !rm.StartDate.IsZero() {
	// 	updateMap["start_date"] = rm.StartDate
	// }

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
	db.Where("roadmap_id = ?", roadmapID).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&roadmap)

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

	results := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Order("day ASC").Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&milestones)

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

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&milestone)

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

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&milestone)

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
	if updatedMilestone.Date != milestone.Date {
		updateMap["date"] = updatedMilestone.Date
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
	db.Where("milestone_id = ?", milestoneID).Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&milestone)

	return milestone
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

func CreateMilestone(c *gin.Context, roadmapID string, milestone types.Milestone) (types.Milestone, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}, errors.New("missing or invalid user_id")
	}

	milestone.UserID = user_id.(string)
	milestone.RoadmapID = roadmapID

	for i := range milestone.Tasks {
		milestone.Tasks[i].UserID = milestone.UserID
		milestone.Tasks[i].RoadmapID = milestone.RoadmapID
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

func SearchUsersByEmail(email string) []types.User {
	var users []types.User

	// With current RLS policy, this will only return the current user if their email matches
	// To search other users, you need to either:
	// 1. Add a new RLS policy allowing user search, or
	// 2. Use service role credentials that bypass RLS
	result := db.Where("email ILIKE ?", "%"+email+"%").Find(&users)
	if result.Error != nil {
		log.Println("Error searching users by email:", result.Error)
		return []types.User{}
	}

	return users
}

// GetFriendshipStatus returns the friendship status between two users
// Returns "accepted", "pending", or "none"
func GetFriendshipStatus(userID1, userID2 string) string {
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status IN (?, ?)",
		userID1, userID2, userID2, userID1, "pending", "accepted",
	).First(&friendship)

	if result.Error != nil {
		return "none"
	}

	return friendship.Status
}

// CreateFriendRequest creates a new friend request
func CreateFriendRequest(c *gin.Context, addresseeID string) (types.Friendship, error) {
	requesterID, exists := c.Get("user_id")
	if !exists || requesterID == "" {
		return types.Friendship{}, errors.New("missing or invalid user_id")
	}

	requesterIDStr := requesterID.(string)

	// Validate requester exists
	var requester types.User
	if err := db.Where("user_id = ?", requesterIDStr).First(&requester).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.Friendship{}, errors.New("requester user not found - please create your profile first")
		}
		return types.Friendship{}, err
	}

	// Validate addressee exists
	var addressee types.User
	if err := db.Where("user_id = ?", addresseeID).First(&addressee).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.Friendship{}, errors.New("addressee user not found")
		}
		return types.Friendship{}, err
	}

	// Check if trying to friend yourself
	if requesterIDStr == addresseeID {
		return types.Friendship{}, errors.New("cannot send friend request to yourself")
	}

	// Check if friendship already exists (in either direction)
	var existingFriendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status IN (?, ?)",
		requesterIDStr, addresseeID, addresseeID, requesterIDStr, "pending", "accepted",
	).First(&existingFriendship)

	if result.Error == nil {
		// Friendship already exists
		if existingFriendship.Status == "accepted" {
			return types.Friendship{}, errors.New("users are already friends")
		}
		return types.Friendship{}, errors.New("friend request already exists")
	}

	// Create new friend request
	friendship := types.Friendship{
		RequesterID: requesterIDStr,
		AddresseeID: addresseeID,
		Status:      "pending",
	}

	if err := db.Create(&friendship).Error; err != nil {
		return types.Friendship{}, err
	}

	return friendship, nil
}

// GetFriendRequests returns all pending friend requests for the user (both sent and received)
func GetFriendRequests(c *gin.Context) []types.Friendship {
	var friendships []types.Friendship
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Println("missing or invalid user_id")
		return []types.Friendship{}
	}

	result := db.Where("(requester_id = ? OR addressee_id = ?) AND status = ?", userID, userID, "pending").
		Preload("Requester").
		Preload("Addressee").
		Order("created_at DESC").
		Find(&friendships)

	if result.Error != nil {
		log.Println("Error fetching friend requests:", result.Error)
		return []types.Friendship{}
	}

	return friendships
}

// GetFriends returns all accepted friendships for the user
func GetFriends(c *gin.Context) []types.Friendship {
	var friendships []types.Friendship
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Println("missing or invalid user_id")
		return []types.Friendship{}
	}

	result := db.Where("(requester_id = ? OR addressee_id = ?) AND status = ?", userID, userID, "accepted").
		Preload("Requester").
		Preload("Addressee").
		Order("created_at DESC").
		Find(&friendships)

	if result.Error != nil {
		log.Println("Error fetching friends:", result.Error)
		return []types.Friendship{}
	}

	return friendships
}

// GetFriendProfile returns a friend's profile with allowed roadmaps
func GetFriendProfile(c *gin.Context, friendUserID string) (types.User, []types.Roadmap, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.User{}, nil, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Fetch the friendship between current user and friend
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status = ?",
		userIDStr, friendUserID, friendUserID, userIDStr, "accepted",
	).First(&friendship)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return types.User{}, nil, errors.New("you are not friends with this user")
		}
		return types.User{}, nil, result.Error
	}

	// Get friend's user info
	var friendUser types.User
	if err := db.Where("user_id = ?", friendUserID).First(&friendUser).Error; err != nil {
		return types.User{}, nil, err
	}

	// Get roadmaps where the current user is the anchor
	// Friends can only see roadmaps they are anchoring for accountability
	var roadmaps []types.Roadmap
	db.Where("user_id = ? AND anchor_id = ?", friendUserID, userIDStr).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Find(&roadmaps)

	return friendUser, roadmaps, nil
}

// RemoveFriend deletes a friendship between the current user and the specified friend
func RemoveFriend(c *gin.Context, friendUserID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Find the friendship in either direction
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?))",
		userIDStr, friendUserID, friendUserID, userIDStr,
	).First(&friendship)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return errors.New("friendship not found")
		}
		return result.Error
	}

	// Delete the friendship
	if err := db.Delete(&friendship).Error; err != nil {
		return err
	}

	return nil
}

// UpdateFriendshipStatus updates the status of a friend request (accept or reject)
func UpdateFriendshipStatus(c *gin.Context, friendshipID string, newStatus string) (types.Friendship, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.Friendship{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Validate status
	if newStatus != "accepted" && newStatus != "rejected" {
		return types.Friendship{}, errors.New("invalid status: must be 'accepted' or 'rejected'")
	}

	// Fetch the friendship
	var friendship types.Friendship
	if err := db.Where("id = ?", friendshipID).First(&friendship).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.Friendship{}, errors.New("friendship not found")
		}
		return types.Friendship{}, err
	}

	// Verify current user is the addressee
	if friendship.AddresseeID != userIDStr {
		return types.Friendship{}, errors.New("only the addressee can respond to a friend request")
	}

	// Verify status is pending
	if friendship.Status != "pending" {
		return types.Friendship{}, errors.New("friend request is not pending")
	}

	// If rejected, delete the friendship
	if newStatus == "rejected" {
		if err := db.Delete(&friendship).Error; err != nil {
			return types.Friendship{}, err
		}
		// Return the friendship with rejected status for the response
		friendship.Status = "rejected"
		return friendship, nil
	}

	// If accepted, update status
	now := time.Now()
	friendship.Status = "accepted"
	friendship.UpdatedAt = &now

	if err := db.Save(&friendship).Error; err != nil {
		return types.Friendship{}, err
	}

	// Auto-create a thread between the two users
	thread, err := CreateThread(c, friendship.RequesterID, friendship.AddresseeID)
	if err != nil {
		// Log the error but don't fail the friendship acceptance
		// The thread can be created later if needed
		log.Printf("Warning: Failed to create thread for friendship %s: %v", friendshipID, err)
	} else {
		log.Printf("Thread %s created for friendship between %s and %s", thread.ThreadID, friendship.RequesterID, friendship.AddresseeID)
	}

	return friendship, nil
}

// CreateMacroMap creates a macro map with all its phases, milestones, spikes, and risks
func CreateMacroMap(c *gin.Context, roadmapID string, macroMap types.MacroMap) (types.MacroMap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.MacroMap{}, errors.New("missing or invalid user_id")
	}

	// Verify roadmap exists and belongs to user
	var roadmap types.Roadmap
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).First(&roadmap)
	if result.Error != nil {
		return types.MacroMap{}, errors.New("roadmap not found or access denied")
	}

	macroMap.RoadmapID = roadmapID

	// Create the macro map with all nested data
	tx := db.Create(&macroMap)
	if tx.Error != nil {
		return types.MacroMap{}, tx.Error
	}

	// Fetch the created macro map with all associations
	var createdMacroMap types.MacroMap
	if err := db.Where("macro_map_id = ?", macroMap.MacroMapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones.Spikes").
		Preload("Phases.MacroMilestones.Risks").
		First(&createdMacroMap).Error; err != nil {
		return types.MacroMap{}, err
	}

	return createdMacroMap, nil
}

// GetMacroMap fetches a macro map by roadmap ID
func GetMacroMap(c *gin.Context, roadmapID string) (types.MacroMap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.MacroMap{}, errors.New("missing or invalid user_id")
	}

	// Verify roadmap exists and belongs to user
	var roadmap types.Roadmap
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).First(&roadmap)
	if result.Error != nil {
		return types.MacroMap{}, errors.New("roadmap not found or access denied")
	}

	var macroMap types.MacroMap
	err := db.Where("roadmap_id = ?", roadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones.Spikes").
		Preload("Phases.MacroMilestones.Risks").
		First(&macroMap).Error

	if err != nil {
		return types.MacroMap{}, err
	}

	return macroMap, nil
}

// ============================================================================
// Thread & Message Management Functions
// ============================================================================

// CreateThread creates a new thread between two users and adds them as participants
func CreateThread(c *gin.Context, userID1, userID2 string) (types.Thread, error) {
	// Validate both users exist
	var user1, user2 types.User
	if err := db.Where("user_id = ?", userID1).First(&user1).Error; err != nil {
		return types.Thread{}, errors.New("user1 not found")
	}
	if err := db.Where("user_id = ?", userID2).First(&user2).Error; err != nil {
		return types.Thread{}, errors.New("user2 not found")
	}

	// Check if a thread already exists between these two users
	var existingThread types.Thread
	err := db.Raw(`
		SELECT t.* FROM threads t
		INNER JOIN thread_participants tp1 ON t.thread_id = tp1.thread_id AND tp1.user_id = ?
		INNER JOIN thread_participants tp2 ON t.thread_id = tp2.thread_id AND tp2.user_id = ?
		WHERE tp1.left_at IS NULL AND tp2.left_at IS NULL
		LIMIT 1
	`, userID1, userID2).Scan(&existingThread).Error

	if err == nil && existingThread.ThreadID != "" {
		// Thread already exists, return it
		return existingThread, nil
	}

	// Create new thread
	thread := types.Thread{
		ThreadID:  uuid.NewString(),
		Name:      "", // Direct messages don't need a name
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&thread).Error; err != nil {
		return types.Thread{}, err
	}

	// Add both users as participants
	participants := []types.ThreadParticipants{
		{
			ThreadPID: uuid.NewString(),
			ThreadID:  thread.ThreadID,
			UserID:    userID1,
			JoinedAt:  time.Now(),
			Role:      "member",
		},
		{
			ThreadPID: uuid.NewString(),
			ThreadID:  thread.ThreadID,
			UserID:    userID2,
			JoinedAt:  time.Now(),
			Role:      "member",
		},
	}

	if err := db.Create(&participants).Error; err != nil {
		// Rollback thread creation if participants fail
		db.Where("thread_id = ?", thread.ThreadID).Delete(&types.Thread{})
		return types.Thread{}, err
	}

	return thread, nil
}

// GetThreadsByUser returns all threads for a given user
func GetThreadsByUser(c *gin.Context, userID string) ([]types.Thread, error) {
	var threads []types.Thread

	// Debug: Check if user has any thread_participants records
	var participantCount int64
	db.Model(&types.ThreadParticipants{}).Where("user_id = ?", userID).Count(&participantCount)
	log.Printf("DEBUG: User %s has %d thread_participants records", userID, participantCount)

	// Debug: Check thread_participants with left_at info
	var allParticipants []types.ThreadParticipants
	db.Where("user_id = ?", userID).Find(&allParticipants)
	for _, p := range allParticipants {
		log.Printf("DEBUG: Participant record - ThreadID: %s, LeftAt: %v", p.ThreadID, p.LeftAt)
	}

	// Get all threads where user is a participant and hasn't left
	err := db.Raw(`
		SELECT DISTINCT t.* FROM threads t
		INNER JOIN thread_participants tp ON t.thread_id = tp.thread_id
		WHERE tp.user_id = ? AND tp.left_at IS NULL
		ORDER BY t.updated_at DESC
	`, userID).Scan(&threads).Error

	if err != nil {
		return nil, err
	}

	log.Printf("DEBUG: Found %d threads for user %s", len(threads), userID)

	return threads, nil
}

// VerifyThreadParticipant checks if a user is a participant of a thread
func VerifyThreadParticipant(c *gin.Context, threadID, userID string) error {
	var participant types.ThreadParticipants
	err := db.Where("thread_id = ? AND user_id = ? AND left_at IS NULL", threadID, userID).
		First(&participant).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("access denied: user is not a participant of this thread")
		}
		return err
	}
	return nil
}

// GetThreadMessages retrieves message history for a thread
// Verifies the user is a participant before returning messages
func GetThreadMessages(c *gin.Context, threadID, userID string) ([]types.MessageResponse, error) {
	// Verify user is a participant
	if err := VerifyThreadParticipant(c, threadID, userID); err != nil {
		return nil, err
	}

	// Get messages with user information
	var messages []types.Messages
	err := db.Where("thread_id = ? AND deleted_at IS NULL", threadID).
		Order("created_at ASC").
		Find(&messages).Error

	if err != nil {
		return nil, err
	}

	// Build response with user information
	var response []types.MessageResponse
	for _, msg := range messages {
		var user types.User
		if err := db.Where("user_id = ?", msg.UserID).First(&user).Error; err == nil {
			response = append(response, types.MessageResponse{
				MessageID: msg.MessageID,
				ThreadID:  msg.ThreadID,
				UserID:    msg.UserID,
				Content:   msg.Content,
				CreatedAt: msg.CreatedAt,
				User:      &user,
			})
		} else {
			// If user not found, still include message but without user object
			response = append(response, types.MessageResponse{
				MessageID: msg.MessageID,
				ThreadID:  msg.ThreadID,
				UserID:    msg.UserID,
				Content:   msg.Content,
				CreatedAt: msg.CreatedAt,
				User:      nil,
			})
		}
	}

	return response, nil
}

// CreateMessage saves a new message to the database
func CreateMessage(c *gin.Context, threadID, userID, content string) (types.Messages, error) {
	// Verify user is a participant
	if err := VerifyThreadParticipant(c, threadID, userID); err != nil {
		return types.Messages{}, err
	}

	// Create message
	message := types.Messages{
		MessageID: uuid.NewString(),
		ThreadID:  threadID,
		UserID:    userID,
		Content:   content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&message).Error; err != nil {
		return types.Messages{}, err
	}

	// Update thread's updated_at timestamp
	db.Model(&types.Thread{}).Where("thread_id = ?", threadID).Update("updated_at", time.Now())

	return message, nil
}

// GetThreadParticipants returns all participants of a thread
func GetThreadParticipants(c *gin.Context, threadID string) ([]types.User, error) {
	var users []types.User

	err := db.Raw(`
		SELECT u.* FROM users u
		INNER JOIN thread_participants tp ON u.user_id = tp.user_id
		WHERE tp.thread_id = ? AND tp.left_at IS NULL
	`, threadID).Scan(&users).Error

	if err != nil {
		return nil, err
	}

	return users, nil
}


func CreateBackgroundJob(c *gin.Context, job *types.BackgroundJob) error {

	return db.Create(job).Error
}

func GetBackgroundJob(c *gin.Context, jobID string, userID string) (*types.BackgroundJob, error){
	var job types.BackgroundJob
	err := db.Where("job_id = ? AND user_id = ?", jobID, userID).First(&job).Error

		return &job, err
}

func UpdateBackgroundJob(ctx context.Context, job *types.BackgroundJob) error{
	updates := map[string]any{}
	if job.Progress > 0 {
		updates["progress"] = job.Progress
	}
	if job.Status != ""{
		updates["status"] = job.Status
	}
	if job.Error != "" {
		updates["error"] = job.Error
	}
	if job.StartedAt != nil{
		updates["started_at"] = job.StartedAt
	}
	if job.CompletedAt != nil {
		updates["completed_at"] = job.CompletedAt
	}
	if job.Result != nil {
		// Manually serialize to JSON for JSONB column since Updates() doesn't use serializer tags
		resultJSON, err := json.Marshal(job.Result)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}
		// Use gorm.Expr to tell PostgreSQL to cast the JSON string to JSONB type
		updates["result"] = gorm.Expr("?::jsonb", string(resultJSON))
	}
	updates["updated_at"] = time.Now()
	return db.Model(&types.BackgroundJob{}).Where("job_id = ?", job.JobID).Updates(updates).Error
}

func CreateDraftRoadmap(c *gin.Context, draft *types.Roadmap) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	userIDStr := user_id.(string)

	// Ensure user exists in database
	var existingUser types.User
	userResult := db.Where("user_id = ?", userIDStr).First(&existingUser)
	if userResult.Error == gorm.ErrRecordNotFound {
		user := types.User{
			UserID:    userIDStr,
			CreatedAt: time.Now(),
		}
		if err := db.Create(&user).Error; err != nil {
			return errors.New("failed to create user: " + err.Error())
		}
	} else if userResult.Error != nil {
		return errors.New("failed to check user existence: " + userResult.Error.Error())
	}

	// Set user ID (should already be set, but ensure it)
	draft.UserID = userIDStr

	// Create the draft roadmap (no nested structures yet)
	return db.Create(draft).Error
}

func UpdateRoadmapWithGeneration(ctx context.Context, roadmap *types.Roadmap) error {
	// Generate UUIDs for all nested entities (MacroMap, Phases, etc.)
	// Note: RoadmapID should already be set from the draft
	if roadmap.MacroMap != nil {
		roadmap.MacroMap.MacroMapID = uuid.NewString()
		roadmap.MacroMap.RoadmapID = roadmap.RoadmapID

		for i := range roadmap.MacroMap.Phases {
			roadmap.MacroMap.Phases[i].PhaseID = uuid.NewString()
			roadmap.MacroMap.Phases[i].MacroMapID = roadmap.MacroMap.MacroMapID

			for j := range roadmap.MacroMap.Phases[i].MacroMilestones {
				roadmap.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID = uuid.NewString()
				roadmap.MacroMap.Phases[i].MacroMilestones[j].PhaseID = roadmap.MacroMap.Phases[i].PhaseID

				for k := range roadmap.MacroMap.Phases[i].MacroMilestones[j].Spikes {
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].SpikeID = uuid.NewString()
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].MacroMilestoneID = roadmap.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}

				for k := range roadmap.MacroMap.Phases[i].MacroMilestones[j].Risks {
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Risks[k].RiskID = uuid.NewString()
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Risks[k].MacroMilestoneID = roadmap.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}
			}
		}
	}

	// Handle milestones if they exist (though async generation currently only creates MacroMap)
	for i := range roadmap.Milestones {
		roadmap.Milestones[i].MilestoneID = uuid.NewString()
		roadmap.Milestones[i].RoadmapID = roadmap.RoadmapID
		roadmap.Milestones[i].UserID = roadmap.UserID

		for j := range roadmap.Milestones[i].Tasks {
			roadmap.Milestones[i].Tasks[j].TaskID = uuid.NewString()
			roadmap.Milestones[i].Tasks[j].MilestoneID = roadmap.Milestones[i].MilestoneID
			roadmap.Milestones[i].Tasks[j].RoadmapID = roadmap.RoadmapID
			roadmap.Milestones[i].Tasks[j].UserID = roadmap.UserID

			for k := range roadmap.Milestones[i].Tasks[j].Steps {
				roadmap.Milestones[i].Tasks[j].Steps[k].StepID = uuid.NewString()
				roadmap.Milestones[i].Tasks[j].Steps[k].TaskID = roadmap.Milestones[i].Tasks[j].TaskID
			}
		}
	}

	// Update the roadmap with full save of all associations
	// FullSaveAssociations ensures nested entities (MacroMap, Phases, etc.) are saved
	return db.Session(&gorm.Session{FullSaveAssociations: true}).Updates(roadmap).Error
}

func UpdateRoadmapGenerationStatus(ctx context.Context, roadmapID, status string) error {
    return db.Model(&types.Roadmap{}).
        Where("roadmap_id = ?", roadmapID).
        Update("generation_status", status).Error
}

// Push token management
func CreateOrUpdatePushToken(ctx context.Context, token *types.PushToken) error {
	// Try to find existing token by expo_token (which has unique constraint)
	var existing types.PushToken
	result := db.Where("expo_token = ?", token.ExpoToken).First(&existing)

	if result.Error == gorm.ErrRecordNotFound {
		// Create new token
		token.TokenID = uuid.NewString()
		token.CreatedAt = time.Now()
		token.LastUsedAt = time.Now()
		token.IsActive = true
		return db.Create(token).Error
	} else if result.Error != nil {
		return result.Error
	}

	// Update existing token (refresh platform, device_id, last_used_at, reactivate if needed)
	existing.Platform = token.Platform
	existing.DeviceID = token.DeviceID
	existing.IsActive = true
	existing.LastUsedAt = time.Now()
	return db.Save(&existing).Error
}

func GetActivePushTokens(ctx context.Context, userID string) []types.PushToken {
    var tokens []types.PushToken
    db.Where("user_id = ? AND is_active = ?", userID, true).Find(&tokens)
    return tokens
}

func DeactivatePushToken(ctx context.Context, expoToken string) error {
    return db.Model(&types.PushToken{}).
        Where("expo_token = ?", expoToken).
        Update("is_active", false).Error
}

// Notification management
func CreateNotification(ctx context.Context, notif *types.Notification) error {
    return db.Create(notif).Error
}

func GetUnreadNotifications(c *gin.Context, userID string) []types.Notification {
    var notifs []types.Notification
    db.Where("user_id = ? AND read_at IS NULL", userID).
        Order("created_at DESC").
        Find(&notifs)
    return notifs
}

func MarkNotificationRead(ctx context.Context, notifID string) error {
    now := time.Now()
    return db.Model(&types.Notification{}).
        Where("notification_id = ?", notifID).
        Update("read_at", now).Error
}

func UpdateNotificationStatus(ctx context.Context, notif *types.Notification, updates map[string]any) error {
    return db.Model(&types.Notification{}).
        Where("notification_id = ?", notif.NotificationID).
        Updates(updates).Error
}

// Anchor request management

// CheckAreFriends checks if two users are friends (accepted status)
func CheckAreFriends(userID1, userID2 string) bool {
	var friendship types.Friendship
	result := db.Where(
		"((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status = ?",
		userID1, userID2, userID2, userID1, "accepted",
	).First(&friendship)

	return result.Error == nil
}

// CreateAnchorRequest creates a new anchor request for a roadmap
func CreateAnchorRequest(c *gin.Context, roadmapID string, anchorUserID string) (types.AnchorRequest, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.AnchorRequest{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Verify the roadmap exists and belongs to the requester
	var roadmap types.Roadmap
	if err := db.Where("roadmap_id = ? AND user_id = ?", roadmapID, userIDStr).First(&roadmap).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.AnchorRequest{}, errors.New("roadmap not found or you don't have permission")
		}
		return types.AnchorRequest{}, err
	}

	// Check if they are friends
	if !CheckAreFriends(userIDStr, anchorUserID) {
		return types.AnchorRequest{}, errors.New("you are not friends with this user")
	}

	// Check if there's already a pending request for this roadmap and anchor
	var existingRequest types.AnchorRequest
	result := db.Where("roadmap_id = ? AND anchor_id = ? AND status = ?", roadmapID, anchorUserID, "pending").First(&existingRequest)
	if result.Error == nil {
		return types.AnchorRequest{}, errors.New("anchor request already exists for this roadmap")
	}

	// Check if the anchor is already assigned to this roadmap
	if roadmap.AnchorID != nil && *roadmap.AnchorID == anchorUserID {
		return types.AnchorRequest{}, errors.New("this user is already an anchor for this roadmap")
	}

	// Create the anchor request
	anchorRequest := types.AnchorRequest{
		RoadmapID:   roadmapID,
		RequesterID: userIDStr,
		AnchorID:    anchorUserID,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}

	if err := db.Create(&anchorRequest).Error; err != nil {
		return types.AnchorRequest{}, err
	}

	// Preload relationships
	db.Preload("Roadmap").Preload("Requester").Preload("Anchor").First(&anchorRequest, "request_id = ?", anchorRequest.RequestID)

	return anchorRequest, nil
}

// GetAnchorRequests gets all pending anchor requests for the current user (as anchor)
func GetAnchorRequests(c *gin.Context) []types.AnchorRequest {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return []types.AnchorRequest{}
	}

	var requests []types.AnchorRequest
	db.Where("anchor_id = ? AND status = ?", userID.(string), "pending").
		Preload("Roadmap").
		Preload("Requester").
		Order("created_at DESC").
		Find(&requests)

	return requests
}

// AcceptAnchorRequest accepts an anchor request and updates the roadmap
func AcceptAnchorRequest(c *gin.Context, requestID string) (types.AnchorRequest, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.AnchorRequest{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Fetch the anchor request
	var anchorRequest types.AnchorRequest
	if err := db.Where("request_id = ?", requestID).First(&anchorRequest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.AnchorRequest{}, errors.New("anchor request not found")
		}
		return types.AnchorRequest{}, err
	}

	// Verify the current user is the anchor
	if anchorRequest.AnchorID != userIDStr {
		return types.AnchorRequest{}, errors.New("only the anchor can accept this request")
	}

	// Verify the request is pending
	if anchorRequest.Status != "pending" {
		return types.AnchorRequest{}, errors.New("anchor request is not pending")
	}

	// Start a transaction
	tx := db.Begin()

	// Update the anchor request status
	now := time.Now()
	anchorRequest.Status = "accepted"
	anchorRequest.UpdatedAt = &now
	if err := tx.Save(&anchorRequest).Error; err != nil {
		tx.Rollback()
		return types.AnchorRequest{}, err
	}

	// Update the roadmap's anchor_id
	if err := tx.Model(&types.Roadmap{}).
		Where("roadmap_id = ?", anchorRequest.RoadmapID).
		Update("anchor_id", userIDStr).Error; err != nil {
		tx.Rollback()
		return types.AnchorRequest{}, err
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return types.AnchorRequest{}, err
	}

	// Reload with relationships
	db.Preload("Roadmap").Preload("Requester").Preload("Anchor").First(&anchorRequest, "request_id = ?", anchorRequest.RequestID)

	return anchorRequest, nil
}

// RejectAnchorRequest rejects an anchor request
func RejectAnchorRequest(c *gin.Context, requestID string) (types.AnchorRequest, error) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return types.AnchorRequest{}, errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Fetch the anchor request
	var anchorRequest types.AnchorRequest
	if err := db.Where("request_id = ?", requestID).First(&anchorRequest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.AnchorRequest{}, errors.New("anchor request not found")
		}
		return types.AnchorRequest{}, err
	}

	// Verify the current user is the anchor
	if anchorRequest.AnchorID != userIDStr {
		return types.AnchorRequest{}, errors.New("only the anchor can reject this request")
	}

	// Verify the request is pending
	if anchorRequest.Status != "pending" {
		return types.AnchorRequest{}, errors.New("anchor request is not pending")
	}

	// Update the status
	now := time.Now()
	anchorRequest.Status = "rejected"
	anchorRequest.UpdatedAt = &now
	if err := db.Save(&anchorRequest).Error; err != nil {
		return types.AnchorRequest{}, err
	}

	// Reload with relationships
	db.Preload("Roadmap").Preload("Requester").Preload("Anchor").First(&anchorRequest, "request_id = ?", anchorRequest.RequestID)

	return anchorRequest, nil
}

// RemoveAnchor removes an anchor from a roadmap
func RemoveAnchor(c *gin.Context, roadmapID string) error {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return errors.New("missing or invalid user_id")
	}

	userIDStr := userID.(string)

	// Verify the roadmap exists and belongs to the user
	var roadmap types.Roadmap
	if err := db.Where("roadmap_id = ? AND user_id = ?", roadmapID, userIDStr).First(&roadmap).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("roadmap not found or you don't have permission")
		}
		return err
	}

	// Check if there's an anchor
	if roadmap.AnchorID == nil {
		return errors.New("no anchor assigned to this roadmap")
	}

	// Remove the anchor
	if err := db.Model(&types.Roadmap{}).
		Where("roadmap_id = ?", roadmapID).
		Update("anchor_id", nil).Error; err != nil {
		return err
	}

	return nil
}