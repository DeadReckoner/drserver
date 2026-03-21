package api

import (
	"dr/server/api/handlers"
	"dr/server/api/routes"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB) *gin.Engine {
	h := handlers.NewHandlers(db)
	return routes.SetupRoutes(h)
}
