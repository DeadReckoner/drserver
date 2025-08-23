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

	err := database.DeleteRoadmap(c, roadmap_id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
