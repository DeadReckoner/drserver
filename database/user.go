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
	tx, _ := UpdateUserReturningPrior(user_id, user)
	return tx
}

// UpdateUserReturningPrior updates the user and also returns the record as it
// was before the write.
//
// The prior state is what tells a state change apart from a repeated
// assertion of the same state — onboarding_completed going false→true is an
// activation event, whereas the client resending it on every profile edit is
// not. The update already has to read the row to build the GORM update, so
// returning it costs nothing; querying separately would add a round trip to
// the hot path purely to learn something already in hand.
func UpdateUserReturningPrior(user_id string, user types.User) (*gorm.DB, types.User) {
	var existingUser types.User
	result := db.Where("user_id = ?", user_id).First(&existingUser)

	if result.Error != nil {
		return &gorm.DB{Error: result.Error}, types.User{}
	}

	// Copied before Updates, which mutates existingUser in place with the
	// applied changes — reading it afterwards would report the new state.
	prior := existingUser

	tx := db.Model(&existingUser).Updates(user)
	return tx, prior
}

// DeleteUser removes the row identified by userID. The caller is responsible
// for ensuring userID matches the authenticated principal — this function
// trusts its argument.
func DeleteUser(userID string) error {
	if userID == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Where("user_id = ?", userID).Delete(&types.User{})
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
