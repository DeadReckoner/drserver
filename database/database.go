package database

import (
	"log"
	"os"
	"strconv"

	"dr/server/types"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

type User struct{
	gorm.Model
    FirstName string `gorm:"column:first_name" json:"first_name"`
    LastName  string `gorm:"column:last_name" json:"last_name"`
    Email     string `gorm:"column:email" json:"email"`
}

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

func CreateUser(user types.User) *gorm.DB{

	newUser := User{
		FirstName: user.First,
		LastName: user.Last,
		Email: user.Email,
	}

	tx := db.Create(&newUser)
	return tx
}

func GetUser(user_id string) User{
	var user User
	id, _ := strconv.Atoi(user_id)
	log.Printf("Fetching User: %s\n", user_id)
	db.First(&user, id)

	log.Println(user)

	return user
} 

