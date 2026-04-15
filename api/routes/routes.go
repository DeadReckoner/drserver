package routes

import (
	"dr/server/api/handlers"
	"dr/server/api/utils"
	"dr/server/middleware"
	"net/http"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(h *handlers.Handlers) *gin.Engine {
	router := gin.Default()

	setupHealthRoutes(router)

	setupV1Routes(router, h)

	return router
}

func setupV1Routes(router *gin.Engine, h *handlers.Handlers) {
	v1 := router.Group("/api/v1")
	v1.Use(middleware.AuthMiddleware())
	v1.Use(middleware.AdminMiddleware())

	setupUserRoutes(v1, h.User)
	setupFriendRoutes(v1, h.Friend)
	setupRoadmapRoutes(v1, h.Roadmap, h.Nudge)
	setupHabitRoutes(v1, h.Habit, h.Nudge)
	setupAgentRoutes(v1, h.Agent)
	setupNotificationRoutes(v1, h.Notification)
	setupMessageThreadRoutes(v1, h.Message)
	setupFeedbackRoutes(v1, h.Feedback)
	setupNudgeRoutes(v1, h.Nudge)
	setupWebSocketRoutes(v1, h.Agent, h.Message)
	setupTaskRoutes(v1, h.Task)
}

func setupUserRoutes(rg *gin.RouterGroup, h *handlers.UserHandler) {
	users := rg.Group("/users")
	{
		users.POST("", h.CreateUser)
		users.POST("/with-image", h.CreateUserWithImage)
		users.GET("", h.GetUser)
		users.GET("/search", h.SearchUsers)
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
		friends.POST("/request/:id", utils.ValidateUUID("id"), h.CreateFriendRequest)
		friends.GET("/:id/profile", utils.ValidateUUID("id"), h.GetFriendProfile)
		friends.PATCH("/:id", utils.ValidateUUID("id"), h.UpdateFriendRequestStatus)
		friends.GET("", h.GetFriends)
		friends.DELETE("/:id", utils.ValidateUUID("id"), h.RemoveFriend)
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
		agent.POST("/generate", h.AgentGenerate)
		agent.POST("/generateMilestone/:milestoneid", h.AgentGenerateMilestone)

		agent.POST("/generate-async", h.AgentGenerateAsync)
		agent.POST("/generate-next-milestone-async/:milestoneid", utils.ValidateUUID("milestoneid"), h.AgentGenerateNextMilestoneAsync)
		sessions := agent.Group("/sessions")
		{
			sessions.POST("", h.StartAISession)
			sessions.GET("/:id", utils.ValidateUUID("id"), h.GetAISession)
			sessions.POST("/:id/reply", utils.ValidateUUID("id"), h.ReplyAISession)
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
