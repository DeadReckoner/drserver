package api

import (
	"dr/server/database"
	"net/http"

	"github.com/gin-gonic/gin"
)

// func createTask(c *gin.Context) {
// 	var newTask types.Task

// 	if err := c.BindJSON(&newTask); err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
// 		return
// 	}
// 	fmt.Printf("New task received: %+v\n", newTask)

// 	tx := database.CreateTask(newTask)
// 	if tx.Error != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
// 		return
// 	}

// 	c.JSON(http.StatusOK, gin.H{"message": "Task created successfully"})
// 	log.Println("Task created")
// }

func getTasks(c *gin.Context) {
	task_id := c.Param("userid")

	tasks := database.GetTasks(task_id)

	c.IndentedJSON(http.StatusOK, tasks)
}

