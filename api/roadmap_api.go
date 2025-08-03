package api

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)






func createRoadmap(c *gin.Context){

	var newRoadmap types.Roadmap


	if err := c.BindJSON(&newRoadmap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("Roadmap received: %+v\n", newRoadmap)

	tx := database.CreateRoadmap(c, newRoadmap)
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Roadmap created successfully"})
	log.Println("Roadmap created")

}


func getRoadmapsByUser(c *gin.Context){

	roadmaps := database.GetRoadmapsByUser(c)

	c.IndentedJSON(http.StatusOK, roadmaps)

}


func getRoadmap(c *gin.Context){
	roadmap_id := c.Param("roadmapID")


	roadmap := database.GetRoadmap(c, roadmap_id)

	c.IndentedJSON(http.StatusOK, roadmap)
}


func updateRoadmap(c *gin.Context){

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

func deleteRoadmap(c *gin.Context){
	roadmap_id := c.Param("roadmapID")

	err := database.DeleteRoadmap(c, roadmap_id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}

	c.JSON(http.StatusOK, gin.H{"message": "Roadmap deleted successfully"})
    log.Println("Delete")
}