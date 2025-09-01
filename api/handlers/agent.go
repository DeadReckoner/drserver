package handlers

import (
	"dr/server/agent"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func AgentGenerate(c *gin.Context) {
	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	fmt.Printf("Goal request received: %+v\n", goalReq)

	resp, err := agent.GenerateGoalFromStructure(goalReq)
	if err != nil {
		log.Printf("Failed to generate roadmap: %v", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	roadmap, err := agent.ParseRoadmap(resp)
	if err != nil {
		log.Printf("Failed to parse roadmap: %v", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	fmt.Printf("Generated roadmap: %+v\n", roadmap)
	utils.DataResponse(c, http.StatusOK, roadmap)
}

func StartAISession(c *gin.Context) {
	userID := c.GetString("user_id")
	session := agent.AISession{
		SessionID: uuid.New(),
		UserID:    userID,
		Status:    "collecting",
		Context:   []agent.Message{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	agent.SaveAISession(&session)

	firstQ := agent.Message{Role: "assistant", Content: "What kind of goal would you like to achieve?"}
	session.Context = append(session.Context, firstQ)

	agent.UpdateAISessionContext(session.SessionID, session.Context)

	utils.DataResponse(c, http.StatusOK, gin.H{
		"session_id": session.SessionID,
		"message":    firstQ.Content,
	})
}

func ReplyAISession(c *gin.Context) {
	sessionID := c.Param("id")
	var req struct {
		Message string `json:"message"`
	}

	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	session, err := agent.LoadAISession(sessionID)
	if err != nil {
		utils.NotFoundResponse(c, "AI session")
		return
	}

	session.Context = append(session.Context, agent.Message{Role: "user", Content: req.Message})

	reply, isComplete, roadmap, err := agent.ContinueAISession(session.Context)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	session.Context = append(session.Context, agent.Message{Role: "assistant", Content: reply})
	if isComplete {
		session.Status = "complete"
		session.Roadmap = roadmap
	}
	session.UpdatedAt = time.Now()

	agent.UpdateAISession(&session)

	resp := gin.H{"message": reply, "complete": isComplete}
	if isComplete {
		resp["roadmap"] = roadmap
	}
	utils.DataResponse(c, http.StatusOK, resp)
}

func GetAISession(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := agent.LoadAISession(sessionID)
	if err != nil {
		utils.NotFoundResponse(c, "AI session")
		return
	}
	utils.DataResponse(c, http.StatusOK, session)
}
