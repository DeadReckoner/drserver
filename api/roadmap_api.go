package api

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func createRoadmap(c *gin.Context) {

	var newRoadmap types.Roadmap

	if err := c.BindJSON(&newRoadmap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("Roadmap received: %+v\n", newRoadmap)

	createdRoadmap, err := database.CreateRoadmap(c, newRoadmap)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.IndentedJSON(http.StatusCreated, createdRoadmap)
	log.Println("Roadmap created")

}

func getRoadmapsByUser(c *gin.Context) {

	roadmaps := database.GetRoadmapsByUser(c)

	c.IndentedJSON(http.StatusOK, roadmaps)

}

func getRoadmap(c *gin.Context) {
	roadmap_id := c.Param("roadmapID")

	roadmap := database.GetRoadmap(c, roadmap_id)

	c.IndentedJSON(http.StatusOK, roadmap)
}

func updateRoadmap(c *gin.Context) {

	roadmap_id := c.Param("roadmapID")

	var updatedRoadmap types.Roadmap

	if err := c.BindJSON(&updatedRoadmap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("Roadmap received: %+v\n", updatedRoadmap)

	roadmap := database.UpdateRoadmap(c, roadmap_id, updatedRoadmap)

	c.IndentedJSON(http.StatusOK, roadmap)
}

func deleteRoadmap(c *gin.Context) {
	roadmap_id := c.Param("roadmapID")

	if _, err := uuid.Parse(roadmap_id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid roadmap ID"})
		return
	}

	err := database.DeleteRoadmap(c, roadmap_id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Roadmap deleted successfully"})
	log.Println("Delete")
}

func getMilestonesByRoadmap(c *gin.Context) {
	roadmap_id := c.Param("roadmapid")

	if _, err := uuid.Parse(roadmap_id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid roadmapid"})
		return
	}

	milestones := database.GetMilestonesByRoadmap(c, roadmap_id)

	c.IndentedJSON(http.StatusOK, milestones)
}

func getMilestone(c *gin.Context) {
	milestone_id := c.Param("milestoneid")
	if _, err := uuid.Parse(milestone_id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid milestoneid"})
		return
	}

	milestone := database.GetMilestone(c, milestone_id)

	c.IndentedJSON(http.StatusOK, milestone)
}

func updateMilestone(c *gin.Context) {
	milestone_id := c.Param("milestoneid")

	var updatedMilestone types.Milestone

	if err := c.BindJSON(&updatedMilestone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	milestone := database.UpdateMilestone(c, milestone_id, updatedMilestone)

	c.IndentedJSON(http.StatusOK, milestone)
}

func deleteMilestone(c *gin.Context) {
	milestone_id := c.Param("milestoneid")
	
	if _, err := uuid.Parse(milestone_id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid milestoneid"})
		return
	}

	err := database.DeleteMilestone(c, milestone_id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Milestone deleted successfully"})
}

func createTask(c *gin.Context) {
	var newTask types.Task

	if err := c.BindJSON(&newTask); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("Task received: %+v\n", newTask)

	createdTask, err := database.CreateTask(c, newTask)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.IndentedJSON(http.StatusCreated, createdTask)
	log.Println("Task created")
}

func getTasksByMilestone(c *gin.Context) {
	milestone_id := c.Param("milestoneid")

	if _, err := uuid.Parse(milestone_id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid milestoneid"})
		return
	}

	tasks := database.GetTasksByMilestone(c, milestone_id)

	c.IndentedJSON(http.StatusOK, tasks)
}

func getTask(c *gin.Context) {
	task_id := c.Param("taskid")
	if _, err := uuid.Parse(task_id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid taskid"})
		return
	}

	task := database.GetTask(c, task_id)

	c.IndentedJSON(http.StatusOK, task)
}

func updateTask(c *gin.Context) {
	task_id := c.Param("taskid")

	var updatedTask types.Task

	if err := c.BindJSON(&updatedTask); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task := database.UpdateTask(c, task_id, updatedTask)

	c.IndentedJSON(http.StatusOK, task)
}

func deleteTask(c *gin.Context) {
	task_id := c.Param("taskid")

	err := database.DeleteTask(c, task_id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
	log.Println("Task deleted")
}
