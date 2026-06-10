package handlers

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskHandler struct {
	service TaskService
}

// NewTaskHandler builds a handler backed by the live database adapter.
// Tests use NewTaskHandlerWithService to inject a fake.
func NewTaskHandler(db *gorm.DB) *TaskHandler {
	return &TaskHandler{service: DBTaskService{}}
}

// NewTaskHandlerWithService is the test-friendly constructor; production
// code should use NewTaskHandler.
func NewTaskHandlerWithService(service TaskService) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) CreateTask(c *gin.Context) {
	var newTask types.Task

	if !utils.BindJSONOrAbort(c, &newTask) {
		return
	}

	fmt.Printf("Task received: %+v\n", newTask)

	createdTask, err := h.service.Create(c, newTask)
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

func (h *TaskHandler) GetTasks(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	roadmapID := c.Query("roadmap_id")

	dateRange := DateRange{
		StartDate: startDate,
		EndDate:   endDate,
	}
	fmt.Println("date range: ", dateRange)
	fmt.Println("roadmap_id: ", roadmapID)

	tasks := h.service.List(c, startDate, endDate, roadmapID)
	utils.DataResponse(c, http.StatusOK, tasks)
}

// GetOverdueTasks returns all incomplete tasks with dates before today.
// Query params:
//   - roadmap_id (optional): filter to specific roadmap
func (h *TaskHandler) GetOverdueTasks(c *gin.Context) {
	roadmapID := c.Query("roadmap_id")
	tasks := database.GetOverdueTasks(c, roadmapID)
	utils.DataResponse(c, http.StatusOK, tasks)
}

func (h *TaskHandler) GetTasksByMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	tasks := database.GetTasksByMilestone(c, milestoneID)
	utils.DataResponse(c, http.StatusOK, tasks)
}

func (h *TaskHandler) GetTask(c *gin.Context) {
	taskID := c.Param("taskid")
	task := h.service.Get(c, taskID)

	if task.TaskID == "" {
		utils.NotFoundResponse(c, "Task")
		return
	}

	utils.DataResponse(c, http.StatusOK, task)
}

func (h *TaskHandler) UpdateTask(c *gin.Context) {
	taskID := c.Param("taskid")
	var updatedTask types.Task

	if !utils.BindJSONOrAbort(c, &updatedTask) {
		return
	}

	task := h.service.Update(c, taskID, updatedTask)
	if task.TaskID == "" {
		utils.NotFoundResponse(c, "Task")
		return
	}

	utils.DataResponse(c, http.StatusOK, task)
	log.Println("Task updated successfully")
}

func (h *TaskHandler) DeleteTask(c *gin.Context) {
	taskID := c.Param("taskid")

	err := h.service.Delete(c, taskID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Task deleted successfully")
	log.Println("Task deleted successfully")
}

func (h *TaskHandler) CreateStep(c *gin.Context) {
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

func (h *TaskHandler) UpdateStep(c *gin.Context) {
	taskID := c.Param("taskid")
	stepID := c.Param("stepid")
	var updatedStep types.Step

	if !utils.BindJSONOrAbort(c, &updatedStep) {
		return
	}

	step := h.service.UpdateStep(c, taskID, stepID, updatedStep)
	if step.StepID == "" {
		utils.NotFoundResponse(c, "Step")
		return
	}

	utils.DataResponse(c, http.StatusOK, step)
	log.Println("Step updated successfully")
}

func (h *TaskHandler) DeleteStep(c *gin.Context) {
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

// CompleteAllSteps marks all steps of a task as complete in a single operation.
// This also triggers cascade completion for the task, milestone, and roadmap.
func (h *TaskHandler) CompleteAllSteps(c *gin.Context) {
	taskID := c.Param("taskid")

	stepsCompleted, err := h.service.CompleteAllSteps(c, taskID)
	if err != nil {
		if err.Error() == "task not found" {
			utils.NotFoundResponse(c, "Task")
			return
		}
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "All steps marked as complete",
		"steps_completed": stepsCompleted,
		"task_id":         taskID,
	})
	log.Printf("All steps completed for task %s: %d steps", taskID, stepsCompleted)
}
