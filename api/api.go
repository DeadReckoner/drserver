package api

import (
	"dr/server/agent"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"

	"dr/server/middleware"

	"github.com/gin-gonic/gin"
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


	router.POST("/agent/generate", agentGen)

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
	if err != nil{
		log.Fatal("Failed to parse roadmap", err)
	}

	fmt.Println("Roadmap:", roadmap)
	
}


func wsHandler(w http.ResponseWriter, r *http.Request) {
    // Upgrade the HTTP connection to a WebSocket connection
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
       fmt.Println("Error upgrading:", err)
       return
    }
    defer conn.Close()
    // Listen for incoming messages
    for {
       // Read message from the client
       _, message, err := conn.ReadMessage()
       if err != nil {
          fmt.Println("Error reading message:", err)
          break
       }
       fmt.Printf("Received: %s\\n", message)
       // Echo the message back to the client
       if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
          fmt.Println("Error writing message:", err)
          break
       }
    }
}