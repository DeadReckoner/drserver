package database

import (
	"log"
	"os"
	"strconv"
	"time"

	"dr/server/types"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

type User struct {
	UserID    uint   `gorm:"primaryKey;column:user_id" json:"user_id"`
	FirstName string `gorm:"column:first_name" json:"first_name"`
	LastName  string `gorm:"column:last_name" json:"last_name"`
	Email     string `gorm:"column:email" json:"email"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type Task struct {
	TaskID       uint   `gorm:"primaryKey;column:task_id" json:"task_id"`
	UserID       uint   `gorm:"index" json:"user_id"`
	Title        string `gorm:"column:title" json:"title"`
	Description  string `gorm:"column:description" json:"description"`
	Level        string `gorm:"column:level" json:"level"`
	Completed    bool   `gorm:"column:completed" json:"complete"`
	StartDate    string `gorm:"column:start_date" json:"start_date"`
	EndDate      string `gorm:"column:end_date" json:"end_date"`
	CreatedAt    time.Time
	ParentTaskID *uint `gorm:"index" json:"parent_task_id"`
}

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
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}

	log.Println("Successfully connected to Supabase PostgreSQL")
}

// Create user in Database
func CreateUser(user types.User) *gorm.DB {

	newUser := User{
		FirstName: user.First,
		LastName:  user.Last,
		Email:     user.Email,
	}

	tx := db.Create(&newUser)
	return tx
}

// GetUser from Database
func GetUser(user_id string) User {
	var user User
	id, _ := strconv.Atoi(user_id)
	log.Printf("Fetching User: %s\n", user_id)
	db.First(&user, id)

	log.Println(user)

	return user
}

func UpdateUser(user_id string) User {
	var user User
	id, _ := strconv.Atoi(user_id)

	log.Printf("Fetching User: %s\n", user_id)
	db.First(&user, id)

	log.Println(user)

	return user
}

// GetTasks by id
func GetTasks(id string) []Task {
	var tasks []Task
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

	newTask := Task{
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
