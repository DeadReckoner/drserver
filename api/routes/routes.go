package routes

import (
	"dr/server/api/handlers"
	"dr/server/api/utils"
	"dr/server/middleware"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Options allows callers (notably tests) to override the middleware stack
// applied to authenticated /api/v1 routes. Nil fields fall back to the
// production middleware so the production call site can pass an empty Options.
type Options struct {
	AuthMiddleware  gin.HandlerFunc
	AdminMiddleware gin.HandlerFunc
}

func SetupRoutes(h *handlers.Handlers) *gin.Engine {
	return SetupRoutesWithOptions(h, Options{})
}

func SetupRoutesWithOptions(h *handlers.Handlers, opts Options) *gin.Engine {
	// gin.New() instead of gin.Default() so we control the global middleware:
	// our structured JSON access logger and JSON panic recovery in place of
	// gin's text logger + text recovery.
	router := gin.New()
	// Fly terminates TLS at its edge and forwards the real client IP in the
	// Fly-Client-IP header; without this c.ClientIP() returns the edge proxy
	// address instead.
	router.TrustedPlatform = gin.PlatformFlyIO
	// RequestLogger OUTER, Recovery INNER: Recovery turns a handler panic into a
	// normal 500 before control returns to RequestLogger's post-Next code, so
	// panic-induced 500s still get an access-log line.
	router.Use(middleware.RequestLogger())
	router.Use(middleware.Recovery())

	setupHealthRoutes(router)

	setupV1Routes(router, h, opts)

	return router
}

func setupV1Routes(router *gin.Engine, h *handlers.Handlers, opts Options) {
	// Public endpoints (no auth required)
	router.GET("/api/v1/roadmaps/shared/:token", h.Roadmap.GetSharedRoadmap)

	// Admin endpoints: shared-secret auth via X-Admin-Key header, no Supabase
	// JWT required. Registered before the /api/v1 group below so the
	// AuthMiddleware doesn't apply.
	setupAdminRoutes(router, h.Admin)

	// External webhooks: auth is verified inside each handler (HMAC or
	// shared secret depending on provider). No Supabase JWT.
	setupWebhookRoutes(router, h.Webhook)

	auth := opts.AuthMiddleware
	if auth == nil {
		auth = middleware.AuthMiddleware()
	}
	admin := opts.AdminMiddleware
	if admin == nil {
		admin = middleware.AdminMiddleware()
	}

	// Authenticated endpoints
	v1 := router.Group("/api/v1")
	v1.Use(auth)
	v1.Use(admin)

	// Copy shared roadmap requires auth
	v1.POST("/roadmaps/shared/:token/copy", h.Roadmap.CopySharedRoadmap)

	setupUserRoutes(v1, h.User)
	setupFriendRoutes(v1, h.Friend)
	setupRoadmapRoutes(v1, h.Roadmap, h.Nudge)
	setupHabitRoutes(v1, h.Habit, h.Nudge)
	setupAgentRoutes(v1, h.Agent)
	setupNotificationRoutes(v1, h.Notification)
	setupNotificationSettingsRoutes(v1, h.NotificationSettings)
	setupMessageThreadRoutes(v1, h.Message)
	setupFeedbackRoutes(v1, h.Feedback)
	setupNudgeRoutes(v1, h.Nudge)
	setupWebSocketRoutes(v1, h.Agent, h.Message)
	setupTaskRoutes(v1, h.Task)
	setupBillingRoutes(v1, h.Billing)
	setupReportRoutes(v1, h.Report)
}

func setupBillingRoutes(rg *gin.RouterGroup, h *handlers.BillingHandler) {
	billing := rg.Group("/billing")
	{
		billing.GET("/status", h.GetBillingStatus)
	}
}

func setupAdminRoutes(router *gin.Engine, h *handlers.AdminHandler) {
	admin := router.Group("/admin")
	admin.Use(handlers.AdminAuthMiddleware())
	{
		admin.POST("/users/:userid/plan", utils.ValidateUUID("userid"), h.SetUserPlan)
	}
}

func setupWebhookRoutes(router *gin.Engine, h *handlers.WebhookHandler) {
	router.POST("/webhooks/revenuecat", h.RevenueCatWebhook)
}

func setupUserRoutes(rg *gin.RouterGroup, h *handlers.UserHandler) {
	users := rg.Group("/users")
	{
		users.POST("", h.CreateUser)
		users.POST("/with-image", h.CreateUserWithImage)
		users.GET("", h.GetUser)
		users.GET("/search", h.SearchUsers)
		// Account-page streak + rolling 30-day compliance (drmobile#151)
		users.GET("/stats", h.GetUserStats)
		users.PATCH("/:id", utils.ValidateUUID("id"), h.UpdateUser)
		users.PATCH("/:id/with-image", utils.ValidateUUID("id"), h.UpdateUserWithImage)
		users.DELETE("/:id", utils.ValidateUUID("id"), h.DeleteUser)
		users.DELETE("/:id/image", utils.ValidateUUID("id"), h.DeleteUserImage)
	}
}

func setupFriendRoutes(rg *gin.RouterGroup, h *handlers.FriendHandler) {
	friends := rg.Group("/friends")
	{
		friends.GET("/search", h.SearchFriends)
		friends.GET("/requests", h.GetFriendRequests)
		friends.GET("/blocked", h.GetBlockedUsers)
		friends.GET("/anchoring", h.GetAnchoring)
		friends.POST("/request/:id", utils.ValidateUUID("id"), h.CreateFriendRequest)
		friends.GET("/:id/profile", utils.ValidateUUID("id"), h.GetFriendProfile)
		friends.PATCH("/:id", utils.ValidateUUID("id"), h.UpdateFriendRequestStatus)
		friends.GET("", h.GetFriends)
		friends.DELETE("/:id", utils.ValidateUUID("id"), h.RemoveFriend)
		// Remove & Block (and unblock to re-invite). Independent of plain Remove.
		friends.POST("/:id/block", utils.ValidateUUID("id"), h.BlockFriend)
		friends.DELETE("/:id/block", utils.ValidateUUID("id"), h.UnblockFriend)
	}
}

func setupRoadmapRoutes(rg *gin.RouterGroup, h *handlers.RoadmapHandler, nudge *handlers.NudgeHandler) {
	roadmaps := rg.Group("/roadmaps")
	{
		roadmaps.POST("", h.CreateRoadmap)
		roadmaps.PUT("/:roadmapid/start", h.StartRoadmap)
		roadmaps.GET("", h.GetRoadmapsByUser)
		roadmaps.GET("/:roadmapid", utils.ValidateUUID("roadmapid"), h.GetRoadmap)
		roadmaps.PATCH("/:roadmapid", utils.ValidateUUID("roadmapid"), h.UpdateRoadmap)
		roadmaps.DELETE("/:roadmapid", utils.ValidateUUID("roadmapid"), h.DeleteRoadmap)

		// Milestone routes
		roadmaps.GET("/:roadmapid/milestones", utils.ValidateUUID("roadmapid"), h.GetMilestonesByRoadmap)
		roadmaps.POST("/:roadmapid/milestones", utils.ValidateUUID("roadmapid"), h.CreateMilestone)

		// Anchor routes
		roadmaps.POST("/:roadmapid/anchor-request", utils.ValidateUUID("roadmapid"), h.CreateAnchorRequest)
		roadmaps.DELETE("/:roadmapid/anchor", utils.ValidateUUID("roadmapid"), h.RemoveAnchor)

		// Nudge route (anchor nudges roadmap owner)
		roadmaps.POST("/:roadmapid/nudge", utils.ValidateUUID("roadmapid"), nudge.NudgeRoadmap)

		// Lifecycle controls
		roadmaps.PUT("/:roadmapid/restart", utils.ValidateUUID("roadmapid"), h.RestartRoadmap)
		roadmaps.POST("/:roadmapid/shift", utils.ValidateUUID("roadmapid"), h.ShiftDates)
		roadmaps.POST("/:roadmapid/realign", utils.ValidateUUID("roadmapid"), h.RealignDates)
		roadmaps.POST("/:roadmapid/pause", utils.ValidateUUID("roadmapid"), h.PauseRoadmap)
		roadmaps.POST("/:roadmapid/resume", utils.ValidateUUID("roadmapid"), h.ResumeRoadmap)
		roadmaps.POST("/:roadmapid/archive", utils.ValidateUUID("roadmapid"), h.ArchiveRoadmap)
		roadmaps.POST("/:roadmapid/restore", utils.ValidateUUID("roadmapid"), h.RestoreRoadmap)
		roadmaps.POST("/:roadmapid/share", utils.ValidateUUID("roadmapid"), h.ShareRoadmap)
		roadmaps.DELETE("/:roadmapid/share", utils.ValidateUUID("roadmapid"), h.RevokeShare)
	}

	// Anchor request routes
	anchorRequests := rg.Group("/anchor-requests")
	{
		anchorRequests.GET("", h.GetAnchorRequests)
		anchorRequests.POST("/:requestid/accept", utils.ValidateUUID("requestid"), h.AcceptAnchorRequest)
		anchorRequests.POST("/:requestid/reject", utils.ValidateUUID("requestid"), h.RejectAnchorRequest)
	}

	milestones := rg.Group("/milestones")
	{
		milestones.GET("/:milestoneid", utils.ValidateUUID("milestoneid"), h.GetMilestone)
		milestones.PATCH("/:milestoneid", utils.ValidateUUID("milestoneid"), h.UpdateMilestone)
		milestones.DELETE("/:milestoneid", utils.ValidateUUID("milestoneid"), h.DeleteMilestone)
	}
}

func setupTaskRoutes(rg *gin.RouterGroup, h *handlers.TaskHandler) {
	// Task routes under milestones
	milestones := rg.Group("/milestones")
	{
		milestones.GET("/:milestoneid/tasks", utils.ValidateUUID("milestoneid"), h.GetTasksByMilestone)
	}

	tasks := rg.Group("/tasks")
	{
		tasks.POST("", h.CreateTask)
		tasks.GET("/user", h.GetTasks)
		tasks.GET("/overdue", h.GetOverdueTasks)
		tasks.GET("/:taskid", utils.ValidateUUID("taskid"), h.GetTask)
		tasks.PATCH("/:taskid", utils.ValidateUUID("taskid"), h.UpdateTask)
		tasks.DELETE("/:taskid", utils.ValidateUUID("taskid"), h.DeleteTask)

		tasks.POST("/:taskid/steps", utils.ValidateUUID("taskid"), h.CreateStep)
		tasks.PATCH("/:taskid/steps/:stepid", utils.ValidateUUID("taskid"), utils.ValidateUUID("stepid"), h.UpdateStep)
		tasks.DELETE("/:taskid/steps/:stepid", utils.ValidateUUID("taskid"), utils.ValidateUUID("stepid"), h.DeleteStep)
		tasks.POST("/:taskid/steps/complete-all", utils.ValidateUUID("taskid"), h.CompleteAllSteps)
	}
}

func setupHabitRoutes(rg *gin.RouterGroup, h *handlers.HabitHandler, nudge *handlers.NudgeHandler) {
	habits := rg.Group("/habits")
	{
		// Habit CRUD operations
		habits.POST("", h.CreateHabit)
		habits.GET("", h.GetHabits)
		habits.GET("/today", h.GetTodaysHabits)
		habits.GET("/date/:date", h.GetHabitsForDate)
		habits.GET("/range", h.GetHabitsForRange)
		habits.GET("/:habitid", utils.ValidateUUID("habitid"), h.GetHabit)
		habits.PATCH("/:habitid", utils.ValidateUUID("habitid"), h.UpdateHabit)
		habits.DELETE("/:habitid", utils.ValidateUUID("habitid"), h.DeleteHabit)
		habits.POST("/:habitid/archive", utils.ValidateUUID("habitid"), h.ArchiveHabit)

		// Completion operations
		habits.POST("/:habitid/complete", utils.ValidateUUID("habitid"), h.CompleteHabit)
		habits.DELETE("/:habitid/complete", utils.ValidateUUID("habitid"), h.UncompleteHabit)
		habits.GET("/:habitid/completions", utils.ValidateUUID("habitid"), h.GetCompletions)

		// Statistics
		habits.GET("/:habitid/stats", utils.ValidateUUID("habitid"), h.GetHabitStats)

		// Anchor operations
		habits.POST("/:habitid/anchor-request", utils.ValidateUUID("habitid"), h.CreateHabitAnchorRequest)
		habits.DELETE("/:habitid/anchor", utils.ValidateUUID("habitid"), h.RemoveHabitAnchor)

		// Nudge route (anchor nudges habit owner)
		habits.POST("/:habitid/nudge", utils.ValidateUUID("habitid"), nudge.NudgeHabit)
	}
}

func setupAgentRoutes(rg *gin.RouterGroup, h *handlers.AgentHandler) {
	agent := rg.Group("/agent")
	{
		// Structured roadmap generation endpoints. Backend provider is selected
		// by the AI_PROVIDER env var (anthropic or openai); see agent.DefaultProvider.
		// Note: /generate (synchronous) is slated for deprecation and remains OpenAI-only.
		agent.POST("/generate", h.AgentGenerate)
		agent.POST("/generateMilestone/:milestoneid", h.AgentGenerateMilestone)
		agent.POST("/generate-async", h.AgentGenerateAsync)
		agent.POST("/generate-next-milestone-async/:milestoneid", utils.ValidateUUID("milestoneid"), h.AgentGenerateNextMilestoneAsync)

		// Anthropic-based conversational chat endpoint
		// Multi-turn conversation with tool use for roadmap generation, editing, and coaching
		agent.POST("/chat", h.Chat)
		agent.DELETE("/chat/session/:session_id", h.EndChatSession)

		// DEPRECATED: these never had a working implementation (no persistence, no
		// model call). Superseded by /agent/chat. They now return 410 Gone.
		// TODO: remove once no clients call them.
		sessions := agent.Group("/sessions")
		{
			sessions.POST("", h.StartAISession)
			sessions.GET("/:id", h.GetAISession)
			sessions.POST("/:id/reply", h.ReplyAISession)
		}
	}

	jobs := rg.Group("/jobs")
	{
		jobs.GET("/:jobid/status", utils.ValidateUUID("jobid"), h.GetJobStatus)
	}
}

func setupNotificationRoutes(rg *gin.RouterGroup, h *handlers.NotificationHandler) {
	notifications := rg.Group("/notifications")
	{
		notifications.GET("", h.GetNotifications)
		notifications.POST("/clear", h.ClearNotifications)
		notifications.PUT("/:alertid/read", utils.ValidateUUID("alertid"), h.MarkNotificationRead)
		notifications.DELETE("/:alertid", utils.ValidateUUID("alertid"), h.DismissNotification)
	}

	push := rg.Group("/push")
	{
		push.POST("/tokens", h.RegisterPushToken)
		push.DELETE("/tokens/:token", h.DeactivatePushToken)
	}

	badge := rg.Group("/badge")
	{
		badge.GET("", h.GetBadgeCount)
		badge.POST("/refresh", h.RefreshBadge)
	}
}

func setupNotificationSettingsRoutes(rg *gin.RouterGroup, h *handlers.NotificationSettingsHandler) {
	settings := rg.Group("/notification-settings")
	{
		// Global settings (channels, quiet hours, digest, MyGoals + F&A defaults)
		settings.GET("", h.GetSettings)
		settings.PUT("", h.UpdateSettings)

		// Per-friend overrides
		settings.GET("/friends", h.ListFriendOverrides)
		settings.GET("/friends/:friendid", utils.ValidateUUID("friendid"), h.GetFriendOverride)
		settings.PUT("/friends/:friendid", utils.ValidateUUID("friendid"), h.UpsertFriendOverride)
		settings.DELETE("/friends/:friendid", utils.ValidateUUID("friendid"), h.DeleteFriendOverride)

		// Per-anchored-goal overrides
		settings.GET("/anchored-goals", h.ListAnchoredGoalOverrides)
		settings.GET("/anchored-goals/:roadmapid", utils.ValidateUUID("roadmapid"), h.GetAnchoredGoalOverride)
		settings.PUT("/anchored-goals/:roadmapid", utils.ValidateUUID("roadmapid"), h.UpsertAnchoredGoalOverride)
		settings.DELETE("/anchored-goals/:roadmapid", utils.ValidateUUID("roadmapid"), h.DeleteAnchoredGoalOverride)
	}
}

func setupMessageThreadRoutes(rg *gin.RouterGroup, h *handlers.MessageHandler) {
	threads := rg.Group("/threads")
	{
		threads.GET("", h.GetThreads)
		threads.GET("/:threadid/messages", utils.ValidateUUID("threadid"), h.GetMessages)
		threads.GET("/unread", h.GetTotalUnreadCount)
		threads.GET("/message/:messageid", utils.ValidateUUID("messageid"), h.GetMessageById)
	}
}

func setupFeedbackRoutes(rg *gin.RouterGroup, h *handlers.FeedbackHandler) {
	feedback := rg.Group("/feedback")
	{
		feedback.POST("", h.CreateFeedback)
	}
}

func setupReportRoutes(rg *gin.RouterGroup, h *handlers.ReportHandler) {
	reports := rg.Group("/reports")
	{
		reports.POST("", h.CreateReport)
	}
}

func setupNudgeRoutes(rg *gin.RouterGroup, h *handlers.NudgeHandler) {
	nudges := rg.Group("/nudges")
	{
		nudges.GET("", h.GetNudgesReceived)
		nudges.GET("/sent", h.GetNudgesSent)
	}
}

func setupWebSocketRoutes(rg *gin.RouterGroup, agent *handlers.AgentHandler, message *handlers.MessageHandler) {
	ws := rg.Group("/ws")
	{
		// Thread-specific WebSocket connection
		ws.GET("/messages/:threadid", utils.ValidateUUID("threadid"), message.WSHandler)

		ws.GET("/agent/:jobid", utils.ValidateUUID("jobid"), agent.WSAgentProgress)
	}
}

func setupHealthRoutes(router *gin.Engine) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "dr-server"})
	})

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
