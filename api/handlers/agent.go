package handlers

import (
	"context"
	"dr/server/agent"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"dr/server/websocket"
	"fmt"
	"net/http"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentHandler struct {
	db *gorm.DB
}

func NewAgentHandler(db *gorm.DB) *AgentHandler {
	return &AgentHandler{db: db}
}

func (h *AgentHandler) AgentGenerate(c *gin.Context) {
	ctx := c.Request.Context()
	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	fmt.Printf("Goal request received: %+v\n", goalReq)

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate strategic roadmap with phases and milestones, tasks for first milestone
	generated, err := agent.GenerateGoalFromStructure(ctx, goalReq)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	fmt.Printf("Generated roadmap with phases: %+v\n", generated)

	// Set roadmap type to "ai_generated" since this was created by the AI
	generated.Roadmap.Type = "ai_generated"

	// Return the complete roadmap with phases and milestones
	utils.DataResponse(c, http.StatusOK, generated.Roadmap)
}

func (h *AgentHandler) AgentGenerateAsync(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	var req types.GoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Info(ctx, "Roadmap generation request received", fmt.Sprintf("UserID: %s, Goal: %s", userID, req.GoalName))

	job := types.BackgroundJob{
		JobID:    uuid.NewString(),
		UserID:   userID,
		Type:     string(types.JobTypeRoadmap),
		Status:   string(types.JobStatusPending),
		Progress: 0,
		Payload:  req,
	}

	if err := database.CreateBackgroundJob(c, &job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	draft := types.Roadmap{
		RoadmapID:        uuid.NewString(),
		UserID:           userID,
		Goal:             req.GoalName,
		Description:      req.GoalDescription,
		Type:             string(types.RoadmapTypeAIGenerated),
		JobID:            &job.JobID,
		GenerationStatus: string(types.GenerationStatusProcessing),
	}

	if err := database.CreateDraftRoadmap(c, &draft); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create draft"})
		return
	}

	// Use background context for the goroutine so it doesn't get cancelled when the HTTP request completes
	// Create logger with JobID as correlation ID for tracking
	bgCtx := log.WithLogger(context.Background(), log.NewLogger(job.JobID))
	go processRoadmapGeneration(bgCtx, job.JobID, draft.RoadmapID, userID, req)

	c.JSON(http.StatusAccepted, gin.H{
		"status":     job.Status,
		"job_id":     job.JobID,
		"roadmap_id": draft.RoadmapID,
		"message":    "Your roadmap is being processed",
		"ws_url":     fmt.Sprintf("ws/agent/%s", job.JobID),
	})

}

func (h *AgentHandler) WSAgentProgress(c *gin.Context) {
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
	defer func() { _ = conn.Close() }()

	websocket.Manager.AddConnection(ctx, jobID, conn)
	defer websocket.Manager.RemoveConnection(ctx, jobID, conn)

	_ = conn.WriteJSON(types.ProgressUpdate{
		Type:     "status",
		JobID:    job.JobID,
		Progress: job.Progress,
		Status:   job.Status,
		Message:  getProgressMessage(job.Status, job.Progress),
	})

	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			log.Info(ctx, "Error reading message; breaking connection", err)
			break
		}

		if msg["type"] == "ping" {
			_ = conn.WriteJSON(map[string]string{"type": "pong"})
		}
	}

}

func (h *AgentHandler) GetJobStatus(c *gin.Context) {
	jobID := c.Param("jobid")
	userID := c.GetString("user_id")

	job, err := database.GetBackgroundJob(c, jobID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	response := gin.H{
		"job_id":     job.JobID,
		"status":     job.Status,
		"progress":   job.Progress,
		"created_at": job.CreatedAt,
	}

	if job.Status == string(types.JobStatusCompleted) && job.Result != nil {
		response["result"] = job.Result
		response["completed_at"] = job.CompletedAt
	} else if job.Status == string(types.JobStatusFailed) {
		response["error"] = job.Error
		response["failed_at"] = job.CompletedAt
	}
	c.JSON(http.StatusOK, response)
}

func processRoadmapGeneration(ctx context.Context, jobID, roadmapID, userID string, req types.GoalRequest) {
	log.Info(ctx, "Starting roadmap generation process", fmt.Sprintf("JobID: %s, RoadmapID: %s, UserID: %s", jobID, roadmapID, userID))

	updateJobProgress(jobID, 5, string(types.JobStatusProcessing), "Initializing AI generation...")

	updateJobProgress(jobID, 10, string(types.JobStatusProcessing), "Generating strategic roadmap (phases and milestones)...")
	log.Info(ctx, "Processing strategic roadmap generation", fmt.Sprintf("JobID: %s, RoadmapID: %s", jobID, roadmapID))

	// Step 1: Generate strategic roadmap (phases with milestones)
	roadmap, err := agent.GenerateStrategicRoadmapFromGoalRequest(ctx, req)
	if err != nil {
		log.Info(ctx, "Strategic roadmap generation failed", fmt.Sprintf("JobID: %s, Error: %v", jobID, err))
		handleGenerationError(jobID, roadmapID, userID, err)
		return
	}
	log.Info(ctx, "Strategic roadmap generation completed", fmt.Sprintf("JobID: %s, Phases: %d", jobID, len(roadmap.Phases)))

	updateJobProgress(jobID, 50, string(types.JobStatusProcessing), "Strategic roadmap complete! Generating tasks for first milestone...")
	log.Info(ctx, "Processing first milestone tasks", fmt.Sprintf("JobID: %s, RoadmapID: %s", jobID, roadmapID))

	// Step 2: Generate tasks for the first milestone only
	var firstMilestone *types.Milestone
	for i := range roadmap.Phases {
		if len(roadmap.Phases[i].Milestones) > 0 {
			firstMilestone = &roadmap.Phases[i].Milestones[0]
			break
		}
	}

	if firstMilestone != nil {
		tasks, err := agent.GenerateMilestoneTasksFromGoalRequest(ctx, req, firstMilestone)
		if err != nil {
			log.Info(ctx, "Milestone tasks generation failed", fmt.Sprintf("JobID: %s, Error: %v", jobID, err))
			handleGenerationError(jobID, roadmapID, userID, err)
			return
		}
		firstMilestone.Tasks = tasks
		log.Info(ctx, "Milestone tasks generation completed", fmt.Sprintf("JobID: %s, Tasks: %d", jobID, len(tasks)))
	}

	updateJobProgress(jobID, 80, string(types.JobStatusProcessing), "Saving roadmap data...")

	roadmap.RoadmapID = roadmapID
	roadmap.UserID = userID
	roadmap.Type = string(types.RoadmapTypeAIGenerated)

	if err := database.UpdateRoadmapWithGeneration(ctx, roadmap); err != nil {
		handleGenerationError(jobID, roadmapID, userID, err)
		return
	}

	updateJobProgress(jobID, 95, string(types.JobStatusProcessing), "Finalizing...")

	now := time.Now()
	job := types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusCompleted),
		Progress:    100,
		CompletedAt: &now,
		Result: map[string]any{
			"roadmap_id": roadmapID,
		},
	}
	_ = database.UpdateBackgroundJob(ctx, &job)
	_ = database.UpdateRoadmapGenerationStatus(ctx, roadmapID, string(types.GenerationStatusCompleted))

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:     "complete",
		JobID:    jobID,
		Progress: 100,
		Status:   string(types.JobStatusCompleted),
		Message:  "Your roadmap is ready for review...",
		Data: map[string]any{
			"roadmap_id": roadmapID,
		},
	})

	log.Info(ctx, "Sending roadmap completion push notification", fmt.Sprintf("UserID: %s, RoadmapID: %s", userID, roadmapID))
	utils.SendPushNotification(userID, "roadmap_complete",
		"Your roadmap is ready for review!", fmt.Sprintf("Your %s roadmap has been generated successfully.", req.GoalName),
		map[string]any{
			"roadmap_id": roadmapID,
		})
	log.Info(ctx, "Push notification sent successfully", fmt.Sprintf("UserID: %s, Type: roadmap_complete", userID))
}

func updateJobProgress(jobID string, progress int, status, message string) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(jobID))

	// Update db
	job := types.BackgroundJob{
		JobID:    jobID,
		Progress: progress,
		Status:   status,
	}
	if status == string(types.JobStatusProcessing) && progress == 5 {
		now := time.Now()
		job.StartedAt = &now
	}

	_ = database.UpdateBackgroundJob(ctx, &job)

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:     "progress",
		JobID:    jobID,
		Progress: progress,
		Status:   status,
		Message:  message,
	})
}

func handleGenerationError(jobID, roadmapID, userID string, err error) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(jobID))
	log.Info(ctx, "Handling roadmap generation error", fmt.Sprintf("JobID: %s, RoadmapID: %s, Error: %v", jobID, roadmapID, err))
	now := time.Now()

	job := types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusFailed),
		Error:       err.Error(),
		CompletedAt: &now,
	}

	_ = database.UpdateBackgroundJob(ctx, &job)

	_ = database.UpdateRoadmapGenerationStatus(ctx, roadmapID, string(types.GenerationStatusFailed))

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:    "error",
		JobID:   jobID,
		Status:  string(types.JobStatusFailed),
		Message: "Failed to generate roadmap",
		Data:    map[string]string{"error": err.Error()},
	})

	log.Info(ctx, "Sending roadmap failure push notification", fmt.Sprintf("UserID: %s, RoadmapID: %s", userID, roadmapID))
	utils.SendPushNotification(userID, "roadmap_failed",
		"Roadmap Generation Failed",
		"We encountered an issue. Please try again.",
		map[string]any{
			"roadmap_id": roadmapID,
			"error":      err.Error(),
		})
	log.Info(ctx, "Push notification sent successfully", fmt.Sprintf("UserID: %s, Type: roadmap_failed", userID))

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

func (h *AgentHandler) AgentGenerateMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	milestoneID := c.Param("milestoneid")

	// Get the existing milestone to generate tasks for
	milestone := database.GetMilestone(c, milestoneID)
	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}
	log.Info(ctx, "Got milestone for task generation:", milestone.SubGoal)

	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	fmt.Printf("Goal request received: %+v\n", goalReq)

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate tasks for this specific milestone
	tasks, err := agent.GenerateMilestoneTasksFromGoalRequest(ctx, goalReq, &milestone)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	fmt.Printf("Generated tasks for milestone: %d tasks\n", len(tasks))

	// Attach tasks to milestone and update
	milestone.Tasks = tasks
	// TODO: Save tasks to database via UpdateMilestoneWithTasks

	utils.DataResponse(c, http.StatusOK, milestone)
}

func (h *AgentHandler) StartAISession(c *gin.Context) {
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

func (h *AgentHandler) ReplyAISession(c *gin.Context) {
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

func (h *AgentHandler) GetAISession(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := agent.LoadAISession(sessionID)
	if err != nil {
		utils.NotFoundResponse(c, "AI session")
		return
	}
	utils.DataResponse(c, http.StatusOK, session)
}
