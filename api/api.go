package api

import (
	"dr/server/agent"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"dr/server/middleware"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewRouter() *gin.Engine {
	router := gin.Default()

	router.Use(middleware.AuthMiddleware())

	router.POST("/user", createUser)
	router.GET("/user", getUser)
	router.PATCH("/user/:id", updateUser)
	router.DELETE("/user/:id", deleteUser)

	router.POST("/task", todo)
	router.GET("/tasks/:userid", getTasks)
	router.GET("/task/:taskid", todo)
	router.PATCH("/task/:taskid", updateTask)
	router.DELETE("/task/:taskid", todo)

	router.POST("/roadmap", createRoadmap)
	router.GET("/roadmap/:roadmapID", getRoadmap)
	router.PATCH("/roadmap/:roadmapID", updateRoadmap)
	router.DELETE("/roadmap/:roadmapID", deleteRoadmap)
	router.GET("/roadmaps", getRoadmapsByUser)

	router.GET("/roadmap/milestones/:roadmapid", getMilestonesByRoadmap)
	router.PATCH("/roadmap/milestone/:milestoneid", updateMilestone)
	router.DELETE("/roadmap/milestone/:milestoneid", todo)
	router.GET("/roadmap/milestone/:milestoneid", getMilestone)

	router.POST("/milestone/task", createTask)
	router.GET("/milestone/tasks/:milestoneid", getTasksByMilestone)
	router.GET("/milestone/task/:taskid", getTask)
	router.PATCH("/milestone/task/:taskid", updateTask)
	router.DELETE("/milestone/task/:taskid", deleteTask)

	router.POST("/agent/generate", agentGen)
	// ...existing code...
	router.POST("/agent/session/start", startAISession)
	router.POST("/agent/session/:id/reply", replyAISession)
	router.GET("/agent/session/:id", getAISession)
	// ...existing code...

	router.GET("/message/:threadid", wsHandler)

	// Health check endpoint
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return router
}

func todo(c *gin.Context) {
	log.Println("ToDo")
}

func agentGen(c *gin.Context) {

	var prompt types.Prompt
	if err := c.BindJSON(&prompt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("Prompt received: %+v\n", prompt)

	resp, err := agent.GenerateGoalBlueprint(prompt.UserPrompt)
	if err != nil {
		log.Println("Failed to generate roadmap", err)
	}

	roadmap, err := agent.ParseRoadmap(resp)
	if err != nil {
		log.Fatal("Failed to parse roadmap", err)
	}

	fmt.Println("Roadmap:", roadmap)

}

func wsHandler(c *gin.Context) {
	threadID := c.Param("threadid")
	userID := c.GetString("user_id") // optional: auth middleware should set this
	log.Printf("ws connect thread=%s user=%s\n", threadID, userID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v\n", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// set some sane limits
	conn.SetReadLimit(1024 * 64)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	var writeMu sync.Mutex

	for {
		mt, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read error: %v\n", err)
			}
			break
		}
		log.Printf("Received (thread=%s user=%s): %s\n", threadID, userID, message)

		// echo back (synchronized)
		writeMu.Lock()
		err = conn.WriteMessage(mt, message)
		writeMu.Unlock()
		if err != nil {
			log.Printf("ws write error: %v\n", err)
			break
		}
	}
}

func startAISession(c *gin.Context) {
	userID := c.GetString("user_id") // adjust as needed for your auth
	session := agent.AISession{
		SessionID: uuid.New(),
		UserID:    userID,
		Status:    "collecting",
		Context:   []agent.Message{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// Save session to DB (implement this)
	agent.SaveAISession(&session)

	// First AI question
	firstQ := agent.Message{Role: "assistant", Content: "What kind of goal would you like to achieve?"}
	session.Context = append(session.Context, firstQ)
	agent.UpdateAISessionContext(session.SessionID, session.Context)

	c.JSON(http.StatusOK, gin.H{
		"session_id": session.SessionID,
		"message":    firstQ.Content,
	})
}

func replyAISession(c *gin.Context) {
	sessionID := c.Param("id")
	var req struct {
		Message string `json:"message"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Load session from DB (implement this)
	session, err := agent.LoadAISession(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	// Add user message
	session.Context = append(session.Context, agent.Message{Role: "user", Content: req.Message})

	// Continue conversation with AI
	reply, isComplete, roadmap, err := agent.ContinueAISession(session.Context)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	session.Context = append(session.Context, agent.Message{Role: "assistant", Content: reply})
	if isComplete {
		session.Status = "complete"
		session.Roadmap = roadmap
	}
	session.UpdatedAt = time.Now()
	// Save session (implement this)
	agent.UpdateAISession(&session)

	resp := gin.H{"message": reply, "complete": isComplete}
	if isComplete {
		resp["roadmap"] = roadmap
	}
	c.JSON(http.StatusOK, resp)
}

func getAISession(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := agent.LoadAISession(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, session)
}
