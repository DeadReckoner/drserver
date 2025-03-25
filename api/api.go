package api

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"dr/server/middleware"

	"github.com/gin-gonic/gin"
)

func Server() {
	router := gin.Default()

	router.Use(middleware.AuthMiddleware())

	router.POST("/user", createUser)
	router.GET("/user", getUser)
	router.PATCH("/user/:id", updateUser)
	router.DELETE("/user/:id", deleteUser)

	router.POST("/task", createTask)
	router.GET("/tasks/:userid", getTasks)
	router.GET("/task/:taskid", todo)
	router.PATCH("/task/:taskid", todo)
	router.DELETE("/task/:taskid", todo)

	router.Run("localhost:8080")
}

func createUser(c *gin.Context) {

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

func updateUser(c *gin.Context) {
	user_id := c.Param("id")

	user := database.UpdateUser(user_id)

	log.Println("Update")
	c.IndentedJSON((http.StatusAccepted), user)
}

func getUser(c *gin.Context) {

	user := database.GetUser(c)

	c.IndentedJSON(http.StatusOK, user)
}

func deleteUser(c *gin.Context) {
	log.Println("Delete")
}

func createTask(c *gin.Context) {
	var newTask types.Task

	if err := c.BindJSON(&newTask); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("Task received: %+v\n", newTask)

	tx := database.CreateTask(newTask)
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task created successfully"})
	log.Println("Task created")
}

func getTasks(c *gin.Context) {
	task_id := c.Param("userid")

	tasks := database.GetTasks(task_id)

	c.IndentedJSON(http.StatusOK, tasks)
}

func todo(c *gin.Context) {
	log.Println("ToDo")
}
