package database

import (
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

	dsn := os.Getenv("DB_URL")
	log.Println("DB: ", dsn)
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	// Connect to the database using GORM
	for attempts := 1; attempts <= 5; attempts++ {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
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
	user_id, _ := c.Get("user_id")
	
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

func CreateTask(task types.Task) *gorm.DB {

	var task_id uint = 345

	newTask := types.Task{
		TaskID: task_id,
		UserID: 6,
		Title:       task.Title,
		Description: task.Description,
		Level:       task.Level,
		StartDate:   task.StartDate,
		EndDate:     task.EndDate,
		Completed:   task.Completed,
		ParentTaskID: &task_id,
	}

	tx := db.Create(&newTask)
	return tx
}
