package api

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)


func Server(){
	router := gin.Default()

	router.POST("/user", createUser)
	router.GET("/user/:id", getUser)
	router.PATCH("/user/:id", updateUser)
	router.DELETE("/user/:id", deleteUser)
  

    
	router.POST("/task", createTask)
	router.GET("/tasks/:userid", getTasks)
 	router.GET("/task/:taskid", todo)
  	router.PATCH("/task/:taskid", todo)
  	router.DELETE("/task/:taskid", todo)



	router.Run("localhost:8080")
}

func createUser(c *gin.Context){

	var newUser types.User

    if err := c.BindJSON(&newUser); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
	fmt.Printf("User received: %+v\n", newUser)

	tx := database.CreateUser(newUser)
	if tx.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
        return
    }

	c.JSON(http.StatusOK, gin.H{"message": "User created successfully"})
    log.Println("User created")
}
func updateUser(c *gin.Context){

	log.Println("Update")
}

func getUser(c *gin.Context){

	database.GetUser("Michael Bollinger")
	log.Println("Get")
}

func deleteUser(c *gin.Context){
	log.Println("Delete")
}


func createTask(c *gin.Context){
  log.Println("Created Task")
}

func getTasks(c *gin.Context){
  userid := c.Param("id")

  log.Println("Get tasks for user", userid)
}


func todo(c *gin.Context){
  log.Println("ToDo")
}


