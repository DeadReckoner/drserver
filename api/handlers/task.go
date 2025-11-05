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

type DateRange struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date,omitempty"`
}

func GetTasks(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	roadmapID := c.Query("roadmap_id")

	dateRange := DateRange{
		StartDate: startDate,
		EndDate:   endDate,
	}
	fmt.Println("date range: ", dateRange)
	fmt.Println("roadmap_id: ", roadmapID)

	tasks := database.GetTasks(c, startDate, endDate, roadmapID)
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

func CreateStep(c *gin.Context) {
	taskID := c.Param("taskid")
	var newStep types.Step

	if !utils.BindJSONOrAbort(c, &newStep) {
		return
	}

	fmt.Printf("Step received: %+v\n", newStep)

	createdStep, err := database.CreateStep(c, taskID, newStep)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdStep)
	log.Println("Step created successfully")
}

func UpdateStep(c *gin.Context) {
	taskID := c.Param("taskid")
	stepID := c.Param("stepid")
	var updatedStep types.Step

	if !utils.BindJSONOrAbort(c, &updatedStep) {
		return
	}

	step := database.UpdateStep(c, taskID, stepID, updatedStep)
	if step.StepID == "" {
		utils.NotFoundResponse(c, "Step")
		return
	}

	utils.DataResponse(c, http.StatusOK, step)
	log.Println("Step updated successfully")
}

func DeleteStep(c *gin.Context) {
	taskID := c.Param("taskid")
	stepID := c.Param("stepid")

	err := database.DeleteStep(c, taskID, stepID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Step deleted successfully")
	log.Println("Step deleted successfully")
}
