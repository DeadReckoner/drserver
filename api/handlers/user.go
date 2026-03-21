package handlers

import (
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"fmt"
	"net/http"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UserHandler struct {
	db *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{db: db}
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

	err := database.DeleteUser(c)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "User deleted successfully")
	log.Info(ctx, "User deleted successfully")
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
