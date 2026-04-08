package database

import (
	"log"

	"github.com/gin-gonic/gin"
)

func GetMetricsByRoadmap(c *gin.Context, roadmapID string) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
	}
}
