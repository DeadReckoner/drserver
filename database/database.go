package database

import (
	"context"
	"os"
	"time"

	"dr/server/log"
	"dr/server/types"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

func Connect(ctx context.Context) *gorm.DB {
	err := godotenv.Load()
	if err != nil {
		log.Info(ctx, "No .env file found, using env vars.")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Error(ctx, "DATABASE_URL not set")
	}

	var connDB *gorm.DB
	var connErr error

	for attempts := 1; attempts <= 5; attempts++ {
		connDB, connErr = gorm.Open(postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true,
		}), &gorm.Config{})
		if connErr == nil {
			break
		}
		log.Error(ctx, "DB connection failed (attempt %d): %v", attempts, connErr)
		time.Sleep(time.Duration(attempts) * time.Second)
	}

	if connErr != nil {
		log.Error(ctx, "Failed to connect to the database after retries: %v", connErr)
	}

	sqlDB, err := connDB.DB()
	if err != nil {
		log.Error(ctx, "Failed to get underlying SQL DB: %v", sqlDB)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	// Set the global db for backward compatibility with existing database functions
	db = connDB

	log.Info(ctx, "Successfully connected to Supabase PostgreSQL")

	return connDB
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
