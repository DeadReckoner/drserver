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
	
	setupUserRoutes(v1)
	setupRoadmapRoutes(v1)
	setupTaskRoutes(v1)
	setupAgentRoutes(v1)
	setupWebSocketRoutes(v1)
}

func setupUserRoutes(rg *gin.RouterGroup) {
	users := rg.Group("/users")
	{
		users.POST("", handlers.CreateUser)
		users.GET("", handlers.GetUser)
		users.PATCH("/:id", utils.ValidateUUID("id"), handlers.UpdateUser)
		users.DELETE("/:id", utils.ValidateUUID("id"), handlers.DeleteUser)
	}
}

func setupRoadmapRoutes(rg *gin.RouterGroup) {
	roadmaps := rg.Group("/roadmaps")
	{
		roadmaps.POST("", handlers.CreateRoadmap)
		roadmaps.GET("", handlers.GetRoadmapsByUser)
		roadmaps.GET("/:roadmapid", utils.ValidateUUID("roadmapid"), handlers.GetRoadmap)
		roadmaps.PATCH("/:roadmapid", utils.ValidateUUID("roadmapid"), handlers.UpdateRoadmap)
		roadmaps.DELETE("/:roadmapid", utils.ValidateUUID("roadmapid"), handlers.DeleteRoadmap)

		roadmaps.GET("/:roadmapid/milestones", 
			utils.ValidateUUID("roadmapid"), 
			handlers.GetMilestonesByRoadmap)
	}

	milestones := rg.Group("/milestones")
	{
		milestones.GET("/:milestoneid", utils.ValidateUUID("milestoneid"), handlers.GetMilestone)
		milestones.PATCH("/:milestoneid", utils.ValidateUUID("milestoneid"), handlers.UpdateMilestone)
		milestones.DELETE("/:milestoneid", utils.ValidateUUID("milestoneid"), handlers.DeleteMilestone)

		milestones.GET("/:milestoneid/tasks", 
			utils.ValidateUUID("milestoneid"), 
			handlers.GetTasksByMilestone)
	}
}

func setupTaskRoutes(rg *gin.RouterGroup) {
	tasks := rg.Group("/tasks")
	{
		tasks.POST("", handlers.CreateTask)
		tasks.GET("/user/:userid", utils.ValidateUUID("userid"), handlers.GetTasks)
		tasks.GET("/:taskid", utils.ValidateUUID("taskid"), handlers.GetTask)
		tasks.PATCH("/:taskid", utils.ValidateUUID("taskid"), handlers.UpdateTask)
		tasks.DELETE("/:taskid", utils.ValidateUUID("taskid"), handlers.DeleteTask)
	}
}

func setupAgentRoutes(rg *gin.RouterGroup) {
	agent := rg.Group("/agent")
	{
		agent.POST("/generate", handlers.AgentGenerate)
		
		sessions := agent.Group("/sessions")
		{
			sessions.POST("", handlers.StartAISession)
			sessions.GET("/:id", utils.ValidateUUID("id"), handlers.GetAISession)
			sessions.POST("/:id/reply", utils.ValidateUUID("id"), handlers.ReplyAISession)
		}
	}
}

func setupWebSocketRoutes(rg *gin.RouterGroup) {
	ws := rg.Group("/ws")
	{
		ws.GET("/messages/:threadid", utils.ValidateUUID("threadid"), handlers.WSHandler)
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