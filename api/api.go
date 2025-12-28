package api

import (
	"dr/server/api/routes"

	"github.com/gin-gonic/gin"
)

func NewRouter() *gin.Engine {
	return routes.SetupRoutes()
}
