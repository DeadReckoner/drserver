package handlers

import (
	"bytes"
	"context"
	"dr/server/agent"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"dr/server/websocket"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func AgentGenerate(c *gin.Context) {
	ctx := c.Request.Context()
	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	fmt.Printf("Goal request received: %+v\n", goalReq)

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate both macro map and milestone map
	generated, err := agent.GenerateGoalFromStructure(ctx, goalReq)
	if err != nil {
		//log.Printf("Failed to generate roadmap: %v", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	//duration := time.Since(start)
	//log.Printf("Request took: %v", duration)

	fmt.Printf("Generated roadmap with macro map: %+v\n", generated)

	// Set roadmap type to "ai_generated" since this was created by the AI
	generated.Roadmap.Type = "ai_generated"

	// Attach macro map to roadmap (nest it inside)
	generated.Roadmap.MacroMap = generated.MacroMap

	// Return the complete roadmap with nested macro map
	utils.DataResponse(c, http.StatusOK, generated.Roadmap)
}

func AgentGenerateAsync(c *gin.Context) {
	userID := c.GetString("user_id")

	var req types.GoalRequest
	if err := c.ShouldBindJSON(&req); err != nil{
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	job := types.BackgroundJob{
		JobID: uuid.NewString(),
		UserID: userID,
		Type: string(types.JobTypeRoadmap),
		Status: string(types.JobStatusPending),
		Progress: 0,
		Payload: req,
	}

	if err := database.CreateBackgroundJob(c, &job); err != nil{
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	draft := types.Roadmap {
		RoadmapID: uuid.NewString(),
		UserID: userID,
		Goal: req.GoalName,
		Description: req.GoalDescription,
		Type: string(types.RoadmapTypeAIGenerated),
		JobID: &job.JobID,
		GenerationStatus: string(types.GenerationStatusProcessing),
	}

	if err := database.CreateDraftRoadmap(c, &draft); err != nil{
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create draft"})
		return
	}

	// Use background context for the goroutine so it doesn't get cancelled when the HTTP request completes
	go processRoadmapGeneration(context.Background(), job.JobID, draft.RoadmapID, userID, req)

	c.JSON(http.StatusAccepted, gin.H{
		"status": job.Status,
		"job_id": job.JobID,
		"roadmap_id": draft.RoadmapID,
		"message": "Your roadmap is being processed",
		"ws_url": fmt.Sprintf("ws/agent/%s", job.JobID),
	})

}


func WSAgentProgress(c *gin.Context){
	ctx := c.Request.Context()
	jobID := c.Param("jobid")
	userID := c.GetString("user_id")


	// Verify job exists and user is authorized
	job, err := database.GetBackgroundJob(c, jobID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	// upgrade to Websocket
	conn, err := websocket.UpgradeConnection(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Websocket upgrade failed"})
		return
	}
	defer conn.Close()

	websocket.Manager.AddConnection(ctx, jobID, conn)
	defer websocket.Manager.RemoveConnection(ctx, jobID, conn)

	conn.WriteJSON(types.ProgressUpdate{
		Type: "status",
		JobID: job.JobID,
		Progress: job.Progress,
		Status: job.Status,
		Message: getProgressMessage(job.Status, job.Progress),
	})

	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			log.Info(ctx, "Error reading message; breaking connection", err)
			break
		}

		if msg["type"] == "ping" {
			conn.WriteJSON(map[string]string{"type": "pong"})
		}
	}

}

func GetJobStatus(c *gin.Context) {
	jobID := c.Param("jobid")
	userID := c.GetString("user_id")

	job, err := database.GetBackgroundJob(c, jobID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	response := gin.H{
		"job_id": job.JobID, 
		"status": job.Status,
		"progress": job.Progress,
		"created_at": job.CreatedAt,
	}

	if job.Status == string(types.JobStatusCompleted) && job.Result != nil {
		response["result"] = job.Result
		response["completed_at"] = job.CompletedAt
	} else if job.Status == string(types.JobStatusFailed){
		response["error"] = job.Error
		response["failed_at"] = job.CompletedAt
	}
	c.JSON(http.StatusOK, response)
}

func processRoadmapGeneration(ctx context.Context, jobID, roadmapID, userID string, req types.GoalRequest) {
	updateJobProgress(jobID, 5, string(types.JobStatusProcessing), "Initializing AI generation...")

	updateJobProgress(jobID, 10, string(types.JobStatusProcessing), "Generating macro map")

	generated, err := agent.GenerateGoalFromStructure(ctx, req)
	if err != nil {
		handleGenerationError(jobID, roadmapID, userID, err)
		return
	}
	updateJobProgress(jobID, 80, string(types.JobStatusProcessing), "Saving roadmap data...")

	generated.Roadmap.RoadmapID = roadmapID
	generated.Roadmap.UserID = userID
	generated.Roadmap.Type = string(types.RoadmapTypeAIGenerated)

	if err := database.UpdateRoadmapWithGeneration(ctx, generated.Roadmap); err != nil {
		handleGenerationError(jobID, roadmapID, userID, err)
		return
	}

	updateJobProgress(jobID, 95, string(types.JobStatusProcessing), "Finalizing...")

	now := time.Now()
	job := types.BackgroundJob{
		JobID: jobID,
		Status: string(types.JobStatusCompleted),
		Progress: 100,
		CompletedAt: &now,
		Result: map[string]any{
			"roadmap_id": roadmapID,
			"macromap_id": generated.MacroMap.MacroMapID,
		},
	}
	database.UpdateBackgroundJob(ctx, &job)

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type: "complete",
		JobID: jobID,
		Progress: 100,
		Status: string(types.JobStatusCompleted),
		Message: "Your roadmap is ready for review...",
		Data: map[string]any{
			"roadmap_id": roadmapID,
		},
	})

	sendPushNotification(userID, "roadmap_complete",
		"Your roadmap is ready for review!", fmt.Sprintf("Your %s roadmap has been generated successfully.", req.GoalName), 
		map[string]any{
		"roadmap_id": roadmapID,
	})
}

func updateJobProgress(jobID string, progress int, status, message string){
	ctx := context.Background()

	// Update db
	job := types.BackgroundJob{
		JobID: jobID,
		Progress: progress,
		Status: status,
	}
	if status == string(types.JobStatusProcessing) && progress == 5{
		now := time.Now()
		job.StartedAt = &now
	}

	database.UpdateBackgroundJob(ctx, &job)

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type: "progress",
		JobID: jobID,
		Progress: progress,
		Status: status,
		Message: message,
	})
}

func handleGenerationError(jobID, roadmapID, userID string, err error) {
	ctx := context.Background()
	now := time.Now()

	job := types.BackgroundJob{
		JobID: jobID,
		Status: string(types.JobStatusFailed),
		Error: err.Error(),
		CompletedAt: &now,
	}

	database.UpdateBackgroundJob(ctx, &job)

	database.UpdateRoadmapGenerationStatus(ctx, roadmapID, string(types.GenerationStatusFailed))

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type: "error",
		JobID: jobID,
		Status: string(types.JobStatusFailed), 
		Message: "Failed to generate roadmap",
		Data: map[string]string{"error": err.Error()},
	})

	sendPushNotification(userID, "roadmap_failed",
		"Roadmap Generation Failed", 
		"We encountered an issue. Please try again.", 
		map[string]any{
			"roadmap_id": roadmapID,
			"error": err.Error(),
		})

}

func getProgressMessage(status string, progress int) string {
	if status == "failed" {
		return "Generation failed"
	}
	if status == "completed" {
		return "Roadmap ready!"
	}
	if progress < 10 {
		return "Initializing..."
	} else if progress < 40 {
		return "Generating strategic roadmap..."
	} else if progress < 80 {
		return "Creating detailed tasks..."
	} else if progress < 95 {
		return "Saving roadmap..."
	}
	return "Finalizing..."
}

func sendPushNotification(userID, notificationType, title, body string, data map[string]any) {

	tokens := database.GetActivePushTokens(context.Background(), userID)
	if len(tokens) == 0 {
		log.Info(context.Background(), "No active push tokens", userID)
		return
	}

	alert := types.Notification{
		NotificationID: uuid.NewString(),
		UserID: userID,
		Type: notificationType,
		Title: title,
		Body: body,
		Data: data,
		Status: string(types.NotificationStatusPending),
	}

	database.CreateNotification(context.Background(), &alert)

	for _, token := range tokens {
		go sendExpoNotification(token.ExpoToken, title, body, data, alert.NotificationID)
	}
}

func sendExpoNotification(expoToken, title, body string, data map[string]any, notificationID string) {
	ctx := context.Background()

	// Prepare Expo Push notification payload
	pushMessage := map[string]any{
		"to":       expoToken,
		"title":    title,
		"body":     body,
		"data":     data,
		"sound":    "default",
		"priority": "high",
	}

	payload, err := json.Marshal(pushMessage)
	if err != nil {
		log.Info(ctx, "Failed to marshal push notification payload", err)
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		return
	}

	// Send to Expo Push API
	resp, err := http.Post(
		"https://exp.host/--/api/v2/push/send",
		"application/json",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		log.Info(ctx, "Failed to send push notification", err)
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		return
	}
	defer resp.Body.Close()

	// Read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Info(ctx, "Failed to read push notification response", err)
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), err.Error())
		return
	}

	// Check if successful
	if resp.StatusCode != http.StatusOK {
		log.Info(ctx, "Expo push notification failed", string(responseBody))
		updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), string(responseBody))
		return
	}

	// Parse response to check for errors
	var expoResponse struct {
		Data []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"data"`
	}

	if err := json.Unmarshal(responseBody, &expoResponse); err == nil {
		if len(expoResponse.Data) > 0 && expoResponse.Data[0].Status == "error" {
			log.Info(ctx, "Expo returned error", expoResponse.Data[0].Message)
			updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusFailed), expoResponse.Data[0].Message)
			return
		}
	}

	// Success!
	updateNotificationStatus(ctx, notificationID, string(types.NotificationStatusSent), "")
	log.Info(ctx, "Push notification sent successfully", notificationID)
}

func updateNotificationStatus(ctx context.Context, notificationID, status, errorMsg string) {
	updates := map[string]any{
		"status": status,
	}

	if status == string(types.NotificationStatusSent) {
		now := time.Now()
		updates["sent_at"] = now
	}

	if errorMsg != "" {
		updates["error"] = errorMsg
	}

	// Update notification status in database
	notification := types.Notification{
		NotificationID: notificationID,
	}

	if err := database.UpdateNotificationStatus(ctx, &notification, updates); err != nil {
		log.Info(ctx, "Failed to update notification status", err)
	}
}

func AgentGenerateMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	milestoneID := c.Param("milestoneid")

	nextMilestone := database.GetMilestone(c, milestoneID)
	log.Info(ctx, "got next milestone:", nextMilestone)

	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	fmt.Printf("Goal request received: %+v\n", goalReq)

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate both macro map and milestone map
	generated, err := agent.GenerateGoalFromStructure(ctx, goalReq)
	if err != nil {
		//log.Printf("Failed to generate roadmap: %v", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	//duration := time.Since(start)
	//log.Printf("Request took: %v", duration)

	fmt.Printf("Generated roadmap with macro map: %+v\n", generated)

	// Set roadmap type to "ai_generated" since this was created by the AI
	generated.Roadmap.Type = "ai_generated"

	// Attach macro map to roadmap (nest it inside)
	generated.Roadmap.MacroMap = generated.MacroMap

	// Return the complete roadmap with nested macro map
	utils.DataResponse(c, http.StatusOK, generated.Roadmap)
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
