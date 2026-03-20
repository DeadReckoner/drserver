package database

import (
	"log"
	"os"
	"time"

	"dr/server/types"

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

// Helper functions for checking completion status

// shouldCompleteTask checks if all steps in a task are completed
// Returns false if task has no steps (empty task should not be auto-completed)
func shouldCompleteTask(taskID string) bool {
	var totalSteps int64
	db.Model(&types.Step{}).Where("task_id = ?", taskID).Count(&totalSteps)
	if totalSteps == 0 {
		return false // Don't auto-complete tasks with no steps
	}

	var incompleteSteps int64
	db.Model(&types.Step{}).Where("task_id = ? AND completed = ?", taskID, false).Count(&incompleteSteps)
	return incompleteSteps == 0
}

// shouldCompleteMilestone checks if all tasks in a milestone are completed
// Returns false if milestone has no tasks (empty milestone should not be auto-completed)
func shouldCompleteMilestone(milestoneID string) bool {
	var totalTasks int64
	db.Model(&types.Task{}).Where("milestone_id = ?", milestoneID).Count(&totalTasks)
	if totalTasks == 0 {
		return false // Don't auto-complete milestones with no tasks
	}

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
