package service

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ServiceAPI struct {
}

/******************************************************************
 *
 * User API
 *
 ******************************************************************/

func (api *ServiceAPI) CreateUser(c *gin.Context) {

	var newUser types.User

	if err := c.BindJSON(&newUser); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("User received: %+v\n", newUser)

	tx := database.CreateUser(c, newUser)
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User created successfully"})
	log.Println("User created")
}

func (api *ServiceAPI) updateUser(c *gin.Context) {
	user_id := c.Param("id")

	user := database.UpdateUser(user_id)

	log.Println("Update")
	c.IndentedJSON((http.StatusAccepted), user)
}

func (api *ServiceAPI) getUser(c *gin.Context) {

	user := database.GetUser(c)

	c.IndentedJSON(http.StatusOK, user)
}

func (api *ServiceAPI) deleteUser(c *gin.Context) {
	user_id := c.Param("id")
	fmt.Printf("Deleting User: %s\n", user_id)

	err := database.DeleteUser(c)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
	log.Println("Delete")
}
