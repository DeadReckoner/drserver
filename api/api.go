package api

import (
	"dr/server/api/handlers"
	"dr/server/api/routes"
	"dr/server/storage"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB) *gin.Engine {
	storageClient := storage.NewStorageClient(os.Getenv("API_URL"), os.Getenv("anon_key"))
	h := handlers.NewHandlers(db, storageClient)
	return routes.SetupRoutes(h)
}
