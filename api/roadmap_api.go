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