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

// Create user in Database
func CreateUser(c *gin.Context, user types.User) *gorm.DB {
	// Handle error later
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return &gorm.DB{Error: errors.New("missing or invalid user_id")}
	}
	
	user.UserID = user_id.(string)
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
	
	rm.UserID = user_id.(string)
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

