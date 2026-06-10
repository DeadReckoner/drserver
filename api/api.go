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

// NewRouterFromHandlers builds the router from a pre-built *handlers.Handlers
// struct. Intended for tests that want to inject fake services on individual
// handlers without going through the live database adapter.
func NewRouterFromHandlers(h *handlers.Handlers, opts routes.Options) *gin.Engine {
	return routes.SetupRoutesWithOptions(h, opts)
}
