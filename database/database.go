package database

import (
	"fmt"
	"log"
	"os"

	"dr/server/types"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

type User struct{
	gorm.Model
	first_name string
	last_name string
	email string
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
	// conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	// if err != nil {
	// 	log.Fatalf("Failed to connect to the database: %v", err)
	// }
	// defer conn.Close(context.Background())

	// // Example query to test connection
	// var version string
	// if err := conn.QueryRow(context.Background(), "SELECT version()").Scan(&version); err != nil {
	// 	log.Fatalf("Query failed: %v", err)
	// }

	// log.Println("Connected to:", version

}

func CreateUser(user types.User) *gorm.DB{
	tx := db.Create(&User{first_name: user.First, last_name: user.Last, email: user.Email})

	return tx
}

func GetUser(userid string){

	fmt.Println("get user {}", userid)
} 

