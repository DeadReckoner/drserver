package handlers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type MetricsHandler struct {
	db *gorm.DB
}

func NewMetricsHandler(db *gorm.DB) *MetricsHandler {
	return &MetricsHandler{db: db}
}

func GetRoadmapMetrics(c *gin.Context) {

}
