package handlers

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
)

func CreateUser(c *gin.Context) {
	var newUser types.User

	if !utils.BindJSONOrAbort(c, &newUser) {
		return
	}
	
	fmt.Printf("User received: %+v\n", newUser)

	tx := database.CreateUser(c, newUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Println("User upserted successfully")
}

func UpdateUser(c *gin.Context) {
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	var updatedUser types.User
	if !utils.BindJSONOrAbort(c, &updatedUser) {
		return
	}

	user := database.UpdateUser(userID)
	if user.UserID == "" {
		utils.NotFoundResponse(c, "User")
		return
	}

	log.Println("User updated successfully")
	utils.DataResponse(c, http.StatusOK, user)
}

func GetUser(c *gin.Context) {
	user := database.GetUser(c)
	if user.UserID == "" {
		utils.NotFoundResponse(c, "User")
		return
	}

	utils.DataResponse(c, http.StatusOK, user)
}

func DeleteUser(c *gin.Context) {
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
	log.Println("User deleted successfully")
}