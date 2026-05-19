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
	return NewRouterWithOptions(db, routes.Options{})
}

// NewRouterWithOptions builds the router with overridable wiring (notably the
// auth middleware). Tests use this to inject a fake auth middleware.
func NewRouterWithOptions(db *gorm.DB, opts routes.Options) *gin.Engine {
	storageClient := storage.NewStorageClient(os.Getenv("API_URL"), os.Getenv("anon_key"))
	h := handlers.NewHandlers(db, storageClient)
	return routes.SetupRoutesWithOptions(h, opts)
}
