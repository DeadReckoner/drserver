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

func CreateTask(c *gin.Context) {
	var newTask types.Task

	if !utils.BindJSONOrAbort(c, &newTask) {
		return
	}
	
	fmt.Printf("Task received: %+v\n", newTask)

	createdTask, err := database.CreateTask(c, newTask)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdTask)
	log.Println("Task created successfully")
}

func GetTasks(c *gin.Context) {
	userID := c.Param("userid")
	tasks := database.GetTasks(userID)
	utils.DataResponse(c, http.StatusOK, tasks)
}

func GetTasksByMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	tasks := database.GetTasksByMilestone(c, milestoneID)
	utils.DataResponse(c, http.StatusOK, tasks)
}

func GetTask(c *gin.Context) {
	taskID := c.Param("taskid")
	task := database.GetTask(c, taskID)
	
	if task.TaskID == "" {
		utils.NotFoundResponse(c, "Task")
		return
	}

	utils.DataResponse(c, http.StatusOK, task)
}

func UpdateTask(c *gin.Context) {
	taskID := c.Param("taskid")
	var updatedTask types.Task

	if !utils.BindJSONOrAbort(c, &updatedTask) {
		return
	}

	task := database.UpdateTask(c, taskID, updatedTask)
	if task.TaskID == "" {
		utils.NotFoundResponse(c, "Task")
		return
	}

	utils.DataResponse(c, http.StatusOK, task)
	log.Println("Task updated successfully")
}

func DeleteTask(c *gin.Context) {
	taskID := c.Param("taskid")

	err := database.DeleteTask(c, taskID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Task deleted successfully")
	log.Println("Task deleted successfully")
}