package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/storage"
	"dr/server/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	avatarsBucket = "avatars"
)

type UserHandler struct {
	db      *gorm.DB
	storage *storage.StorageClient
}

func NewUserHandler(db *gorm.DB, storageClient *storage.StorageClient) *UserHandler {
	return &UserHandler{db: db, storage: storageClient}
}

func (h *UserHandler) CreateUser(c *gin.Context) {
	ctx := c.Request.Context()
	var newUser types.User

	if !utils.BindJSONOrAbort(c, &newUser) {
		return
	}

	// Parse full_name into first_name/last_name if provided (for Apple/Google sign-in)
	newUser.ParseFullName()

	fmt.Printf("User received: %+v\n", newUser)

	tx := database.CreateUser(c, newUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User upserted successfully")
}

// CreateUserWithImage creates a user with an optional profile image.
// Accepts multipart/form-data with:
//   - "user": JSON string of user data
//   - "image": optional image file
func (h *UserHandler) CreateUserWithImage(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse user JSON from form field
	userJSON := c.PostForm("user")
	if userJSON == "" {
		utils.BadRequestResponse(c, "user field is required")
		return
	}

	var newUser types.User
	if err := json.Unmarshal([]byte(userJSON), &newUser); err != nil {
		utils.BadRequestResponse(c, "invalid user JSON: "+err.Error())
		return
	}

	newUser.ParseFullName()

	// Handle optional image file
	file, header, err := c.Request.FormFile("image")
	if err == nil {
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		if err != nil {
			utils.InternalErrorResponse(c, err)
			return
		}

		path := fmt.Sprintf("%s/profile", newUser.UserID)
		contentType := header.Header.Get("Content-Type")

		if err := h.storage.Upload(ctx, avatarsBucket, path, contentType, data); err != nil {
			log.Error(ctx, "Failed to upload profile image", err)
			utils.InternalErrorResponse(c, err)
			return
		}

		newUser.ProfileImageURL = h.storage.PublicURL(avatarsBucket, path)
	}

	tx := database.CreateUser(c, newUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User created with image successfully")
}

func (h *UserHandler) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	var updatedUser types.User
	if !utils.BindJSONOrAbort(c, &updatedUser) {
		return
	}

	// Parse full_name into first_name/last_name if provided
	updatedUser.ParseFullName()

	tx := database.UpdateUser(userID, updatedUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User updated successfully")
}

// UpdateUserWithImage updates a user with an optional profile image.
// Accepts multipart/form-data with:
//   - "user": JSON string of user data
//   - "image": optional image file
func (h *UserHandler) UpdateUserWithImage(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	// Parse user JSON from form field
	userJSON := c.PostForm("user")
	if userJSON == "" {
		utils.BadRequestResponse(c, "user field is required")
		return
	}

	var updatedUser types.User
	if err := json.Unmarshal([]byte(userJSON), &updatedUser); err != nil {
		utils.BadRequestResponse(c, "invalid user JSON: "+err.Error())
		return
	}

	updatedUser.ParseFullName()

	// Handle optional image file
	file, header, err := c.Request.FormFile("image")
	if err == nil {
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		if err != nil {
			utils.InternalErrorResponse(c, err)
			return
		}

		path := fmt.Sprintf("%s/profile", userID)
		contentType := header.Header.Get("Content-Type")

		if err := h.storage.Upload(ctx, avatarsBucket, path, contentType, data); err != nil {
			log.Error(ctx, "Failed to upload profile image", err)
			utils.InternalErrorResponse(c, err)
			return
		}

		updatedUser.ProfileImageURL = h.storage.PublicURL(avatarsBucket, path)
	}

	tx := database.UpdateUser(userID, updatedUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User updated with image successfully")
}

func (h *UserHandler) GetUser(c *gin.Context) {
	ctx := c.Request.Context()
	user := database.GetUser(c)

	log.Info(ctx, "User: ", user)
	if user.UserID == "" {
		utils.NotFoundResponse(c, "User")
		return
	}

	utils.DataResponse(c, http.StatusOK, user)
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	fmt.Printf("Deleting User: %s\n", userID)

	// Delete profile image from storage (ignore errors - image may not exist)
	path := fmt.Sprintf("%s/profile", userID)
	if err := h.storage.Delete(ctx, avatarsBucket, path); err != nil {
		log.Warn(ctx, "Failed to delete profile image (may not exist)", err)
	}

	err := database.DeleteUser(c)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "User deleted successfully")
	log.Info(ctx, "User deleted successfully")
}

// DeleteUserImage removes a user's profile image without deleting the user.
func (h *UserHandler) DeleteUserImage(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	// Delete from storage
	path := fmt.Sprintf("%s/profile", userID)
	if err := h.storage.Delete(ctx, avatarsBucket, path); err != nil {
		log.Error(ctx, "Failed to delete profile image", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	// Clear the URL in the database
	tx := database.UpdateUser(userID, types.User{ProfileImageURL: ""})
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User profile image deleted successfully")
}

func (h *UserHandler) SearchUsers(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		utils.BadRequestResponse(c, "email query parameter is required")
		return
	}

	users := database.SearchUsersByEmail(email)
	utils.DataResponse(c, http.StatusOK, users)
}
