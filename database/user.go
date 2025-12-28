package database

import (
	"errors"
	"log"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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

// GetUserByID retrieves a user by their ID
func GetUserByID(userID string) (types.User, error) {
	var user types.User
	result := db.Where("user_id = ?", userID).First(&user)
	if result.Error != nil {
		return types.User{}, result.Error
	}
	return user, nil
}
