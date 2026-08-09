package api

import (
	"context"
	"dr/server/api/handlers"
	"dr/server/api/routes"
	"dr/server/auth/apple"
	"dr/server/log"
	"dr/server/middleware"
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
	// Storage runs server-side with the service-role key so it bypasses RLS;
	// the anon key would be rejected by the avatars bucket policies. Warn loudly
	// if it's unset rather than letting uploads fail later with an opaque
	// "Invalid Compact JWS" from the storage API.
	serviceKey := os.Getenv("service_role_key")
	if serviceKey == "" {
		log.Warn(context.Background(), "service_role_key is not set; storage uploads/deletes will fail")
	}
	storageClient := storage.NewStorageClient(os.Getenv("API_URL"), serviceKey)
	appleClient, err := apple.NewClientFromEnv()
	if err != nil {
		log.Warn(context.Background(), "Apple Sign-In revocation disabled", err)
	}
	h := handlers.NewHandlers(db, storageClient, appleClient)
	return routes.SetupRoutesWithOptions(h, opts)
}

// NewHealthOnlyRouter builds a router that serves nothing but the liveness and
// readiness probes. Used by the scheduler process group, which runs cron and
// must not expose the API — it isn't attached to the public http_service, but
// building the full route tree there would mean a second set of machines could
// serve user traffic if that ever changed by accident.
func NewHealthOnlyRouter(db *gorm.DB) *gin.Engine {
	router := gin.New()
	router.TrustedPlatform = gin.PlatformFlyIO
	router.Use(middleware.RequestLogger())
	router.Use(middleware.Recovery())

	h := handlers.NewHealthHandler(db)
	router.GET("/health", h.Live)
	router.GET("/healthz", h.Ready)
	return router
}

// NewRouterFromHandlers builds the router from a pre-built *handlers.Handlers
// struct. Intended for tests that want to inject fake services on individual
// handlers without going through the live database adapter.
func NewRouterFromHandlers(h *handlers.Handlers, opts routes.Options) *gin.Engine {
	return routes.SetupRoutesWithOptions(h, opts)
}
