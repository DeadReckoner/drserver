package routes

import (
	"dr/server/api/handlers"
	"dr/server/api/utils"
	"dr/server/middleware"
	"net/http"

	"github.com/gin-gonic/gin"
)

func SetupRoutes() *gin.Engine {
	router := gin.Default()

	setupHealthRoutes(router)

	setupV1Routes(router)

	return router
}

func setupV1Routes(router *gin.Engine) {
	v1 := router.Group("/api/v1")
	v1.Use(middleware.AuthMiddleware())
	v1.Use(middleware.AdminMiddleware())

	setupUserRoutes(v1)
	setupFriendRoutes(v1)
	setupRoadmapRoutes(v1)
	setupHabitRoutes(v1)
	setupAgentRoutes(v1)
	setupNotificationRoutes(v1)
	setupMessageThreadRoutes(v1)
	setupFeedbackRoutes(v1)
	setupNudgeRoutes(v1)
	setupWebSocketRoutes(v1)
}

func setupUserRoutes(rg *gin.RouterGroup) {
	users := rg.Group("/users")
	{
		users.POST("", handlers.CreateUser)
		users.GET("", handlers.GetUser)
		users.GET("/search", handlers.SearchUsers)
		users.PATCH("/:id", utils.ValidateUUID("id"), handlers.UpdateUser)
		users.DELETE("/:id", utils.ValidateUUID("id"), handlers.DeleteUser)
	}
}

func setupFriendRoutes(rg *gin.RouterGroup) {
	friends := rg.Group("/friends")
	{
		friends.GET("/search", handlers.SearchFriends)
		friends.GET("/requests", handlers.GetFriendRequests)
		friends.POST("/request/:id", utils.ValidateUUID("id"), handlers.CreateFriendRequest)
		friends.GET("/:id/profile", utils.ValidateUUID("id"), handlers.GetFriendProfile)
		friends.PATCH("/:id", utils.ValidateUUID("id"), handlers.UpdateFriendRequestStatus)
		friends.GET("", handlers.GetFriends)
		friends.DELETE("/:id", utils.ValidateUUID("id"), handlers.RemoveFriend)
	}
}

func setupRoadmapRoutes(rg *gin.RouterGroup) {
	roadmaps := rg.Group("/roadmaps")
	{
		roadmaps.POST("", handlers.CreateRoadmap)
		roadmaps.PUT("/:roadmapid/start", handlers.StartRoadmap)
		roadmaps.GET("", handlers.GetRoadmapsByUser)
		roadmaps.GET("/:roadmapid", utils.ValidateUUID("roadmapid"), handlers.GetRoadmap)
		roadmaps.PATCH("/:roadmapid", utils.ValidateUUID("roadmapid"), handlers.UpdateRoadmap)
		roadmaps.DELETE("/:roadmapid", utils.ValidateUUID("roadmapid"), handlers.DeleteRoadmap)

		// Milestone routes
		roadmaps.GET("/:roadmapid/milestones", utils.ValidateUUID("roadmapid"), handlers.GetMilestonesByRoadmap)
		roadmaps.POST("/:roadmapid/milestones", utils.ValidateUUID("roadmapid"), handlers.CreateMilestone)

		// Anchor routes
		roadmaps.POST("/:roadmapid/anchor-request", utils.ValidateUUID("roadmapid"), handlers.CreateAnchorRequest)
		roadmaps.DELETE("/:roadmapid/anchor", utils.ValidateUUID("roadmapid"), handlers.RemoveAnchor)

		// Nudge route (anchor nudges roadmap owner)
		roadmaps.POST("/:roadmapid/nudge", utils.ValidateUUID("roadmapid"), handlers.NudgeRoadmap)
	}

	// Anchor request routes
	anchorRequests := rg.Group("/anchor-requests")
	{
		anchorRequests.GET("", handlers.GetAnchorRequests)
		anchorRequests.POST("/:requestid/accept", utils.ValidateUUID("requestid"), handlers.AcceptAnchorRequest)
		anchorRequests.POST("/:requestid/reject", utils.ValidateUUID("requestid"), handlers.RejectAnchorRequest)
	}

	milestones := rg.Group("/milestones")
	{
		milestones.GET("/:milestoneid", utils.ValidateUUID("milestoneid"), handlers.GetMilestone)
		milestones.PATCH("/:milestoneid", utils.ValidateUUID("milestoneid"), handlers.UpdateMilestone)
		milestones.DELETE("/:milestoneid", utils.ValidateUUID("milestoneid"), handlers.DeleteMilestone)
		milestones.GET("/:milestoneid/tasks", utils.ValidateUUID("milestoneid"), handlers.GetTasksByMilestone)
	}

	tasks := rg.Group("/tasks")
	{
		tasks.POST("", handlers.CreateTask)
		tasks.GET("/user", handlers.GetTasks)
		tasks.GET("/:taskid", utils.ValidateUUID("taskid"), handlers.GetTask)
		tasks.PATCH("/:taskid", utils.ValidateUUID("taskid"), handlers.UpdateTask)
		tasks.DELETE("/:taskid", utils.ValidateUUID("taskid"), handlers.DeleteTask)

		tasks.POST("/:taskid/steps", utils.ValidateUUID("taskid"), handlers.CreateStep)
		tasks.PATCH("/:taskid/steps/:stepid", utils.ValidateUUID("taskid"), utils.ValidateUUID("stepid"), handlers.UpdateStep)
		tasks.DELETE("/:taskid/steps/:stepid", utils.ValidateUUID("taskid"), utils.ValidateUUID("stepid"), handlers.DeleteStep)
		tasks.POST("/:taskid/steps/complete-all", utils.ValidateUUID("taskid"), handlers.CompleteAllSteps)
	}
}

func setupHabitRoutes(rg *gin.RouterGroup) {
	habits := rg.Group("/habits")
	{
		// Habit CRUD operations
		habits.POST("", handlers.CreateHabit)
		habits.GET("", handlers.GetHabits)
		habits.GET("/today", handlers.GetTodaysHabits)
		habits.GET("/date/:date", handlers.GetHabitsForDate)
		habits.GET("/range", handlers.GetHabitsForRange)
		habits.GET("/:habitid", utils.ValidateUUID("habitid"), handlers.GetHabit)
		habits.PATCH("/:habitid", utils.ValidateUUID("habitid"), handlers.UpdateHabit)
		habits.DELETE("/:habitid", utils.ValidateUUID("habitid"), handlers.DeleteHabit)
		habits.POST("/:habitid/archive", utils.ValidateUUID("habitid"), handlers.ArchiveHabit)

		// Completion operations
		habits.POST("/:habitid/complete", utils.ValidateUUID("habitid"), handlers.CompleteHabit)
		habits.DELETE("/:habitid/complete", utils.ValidateUUID("habitid"), handlers.UncompleteHabit)
		habits.GET("/:habitid/completions", utils.ValidateUUID("habitid"), handlers.GetCompletions)

		// Statistics
		habits.GET("/:habitid/stats", utils.ValidateUUID("habitid"), handlers.GetHabitStats)

		// Anchor operations
		habits.POST("/:habitid/anchor-request", utils.ValidateUUID("habitid"), handlers.CreateHabitAnchorRequest)
		habits.DELETE("/:habitid/anchor", utils.ValidateUUID("habitid"), handlers.RemoveHabitAnchor)

		// Nudge route (anchor nudges habit owner)
		habits.POST("/:habitid/nudge", utils.ValidateUUID("habitid"), handlers.NudgeHabit)
	}
}

func setupAgentRoutes(rg *gin.RouterGroup) {
	agent := rg.Group("/agent")
	{
		agent.POST("/generate", handlers.AgentGenerate)
		agent.POST("/generateMilestone/:milestoneid", handlers.AgentGenerateMilestone)

		agent.POST("/generate-async", handlers.AgentGenerateAsync)
		sessions := agent.Group("/sessions")
		{
			sessions.POST("", handlers.StartAISession)
			sessions.GET("/:id", utils.ValidateUUID("id"), handlers.GetAISession)
			sessions.POST("/:id/reply", utils.ValidateUUID("id"), handlers.ReplyAISession)
		}
	}

	jobs := rg.Group("/jobs")
	{
		jobs.GET("/:jobid/status", utils.ValidateUUID("jobid"), handlers.GetJobStatus)
	}
}

func setupNotificationRoutes(rg *gin.RouterGroup) {
	notifications := rg.Group("/notifications")
	{
		notifications.GET("", handlers.GetNotifications)
		notifications.POST("/clear", handlers.ClearNotifications)
		notifications.PUT("/:alertid/read", utils.ValidateUUID("alertid"), handlers.MarkNotificationRead)
		notifications.DELETE("/:alertid", utils.ValidateUUID("alertid"), handlers.DismissNotification)
	}

	push := rg.Group("/push")
	{
		push.POST("/tokens", handlers.RegisterPushToken)
		push.DELETE("/tokens/:token", handlers.DeactivatePushToken)
	}

	badge := rg.Group("/badge")
	{
		badge.GET("", handlers.GetBadgeCount)
		badge.POST("/refresh", handlers.RefreshBadge)
	}
}

func setupMessageThreadRoutes(rg *gin.RouterGroup) {
	threads := rg.Group("/threads")
	{
		threads.GET("", handlers.GetThreads)
		threads.GET("/:threadid/messages", utils.ValidateUUID("threadid"), handlers.GetMessages)
		threads.GET("/unread", handlers.GetTotalUnreadCount)
		threads.GET("/message/:messageid", utils.ValidateUUID("messageid"), handlers.GetMessageById)
	}
}

func setupFeedbackRoutes(rg *gin.RouterGroup) {
	feedback := rg.Group("/feedback")
	{
		feedback.POST("", handlers.CreateFeedback)
	}
}

func setupNudgeRoutes(rg *gin.RouterGroup) {
	nudges := rg.Group("/nudges")
	{
		nudges.GET("", handlers.GetNudgesReceived)
		nudges.GET("/sent", handlers.GetNudgesSent)
	}
}

func setupWebSocketRoutes(rg *gin.RouterGroup) {
	ws := rg.Group("/ws")
	{
		// Thread-specific WebSocket connection
		ws.GET("/messages/:threadid", utils.ValidateUUID("threadid"), handlers.WSHandler)

		ws.GET("/agent/:jobid", utils.ValidateUUID("jobid"), handlers.WSAgentProgress)
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
