package handlers

import (
	"context"
	"dr/server/agent"
	"dr/server/agent/anthropic"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"dr/server/websocket"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentHandler struct {
	db               *gorm.DB
	provider         agent.Provider
	anthropicService *anthropic.Service
}

func NewAgentHandler(db *gorm.DB) *AgentHandler {
	// Initialize Anthropic service if API key is available
	var anthropicSvc *anthropic.Service
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		anthropicSvc = anthropic.NewService(apiKey)
	}

	return &AgentHandler{
		db: db,
		// Selects the structured-generation backend from the AI_PROVIDER env var
		// (anthropic vs. openai). Resolved once at startup; env is fixed for the
		// process lifetime.
		provider:         agent.DefaultProvider(),
		anthropicService: anthropicSvc,
	}
}

func (h *AgentHandler) AgentGenerate(c *gin.Context) {
	ctx := c.Request.Context()
	var goalReq types.GoalRequest

	if !requireAIAck(c) {
		return
	}

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	log.Info(ctx, "Goal request received")

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate strategic roadmap with phases and milestones, tasks for first milestone
	generated, err := agent.GenerateGoalFromStructure(ctx, goalReq)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	// Bump the AI usage counter only on success - failed generations don't
	// burn a credit. Log-only on error so a counter blip doesn't fail the
	// user-visible request.
	if err := database.IncrementAIGenerations(userID); err != nil {
		log.Error(ctx, "AgentGenerate: failed to increment AI counter", err)
	}

	log.Info(ctx, "Generated roadmap with phases", fmt.Sprintf("phases=%d", len(generated.Roadmap.Phases)))

	// Set roadmap type to "ai_generated" since this was created by the AI
	generated.Roadmap.Type = "ai_generated"

	// Return the complete roadmap with phases and milestones
	utils.DataResponse(c, http.StatusOK, generated.Roadmap)
}

func (h *AgentHandler) AgentGenerateAsync(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	if !requireAIAck(c) {
		return
	}

	var req types.GoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	log.Info(ctx, "Roadmap generation request received")

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
	go processRoadmapGeneration(bgCtx, h.provider, job.JobID, draft.RoadmapID, userID, req)

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

func processRoadmapGeneration(ctx context.Context, provider agent.Provider, jobID, roadmapID, userID string, req types.GoalRequest) {
	// Time the whole async generation (job start → finish) and emit it as a
	// structured "roadmap generation finished" event with a numeric
	// generation_ms field, so Axiom can chart end-to-end generation latency.
	// The HTTP request only enqueues this work, so its access-log duration_ms
	// can't measure this. The defer covers every exit path; outcome stays
	// "failed" unless the success path sets it.
	start := time.Now()
	outcome := "failed"
	defer func() {
		log.FromContext(ctx).LogAttrs(log.LevelInfo, "roadmap generation finished",
			slog.String("job_id", jobID),
			slog.String("roadmap_id", roadmapID),
			slog.String("user_id", userID),
			slog.String("provider", provider.Name()),
			slog.String("outcome", outcome),
			slog.Int64("generation_ms", time.Since(start).Milliseconds()),
		)
	}()

	log.Info(ctx, "Starting roadmap generation process", fmt.Sprintf("JobID: %s, RoadmapID: %s, UserID: %s", jobID, roadmapID, userID))
	log.Info(ctx, "AI provider selected", fmt.Sprintf("provider=%s, JobID: %s", provider.Name(), jobID))

	updateJobProgress(jobID, 5, string(types.JobStatusProcessing), "Initializing AI generation...")

	updateJobProgress(jobID, 10, string(types.JobStatusProcessing), "Generating strategic roadmap (phases and milestones)...")
	log.Info(ctx, "Processing strategic roadmap generation", fmt.Sprintf("JobID: %s, RoadmapID: %s", jobID, roadmapID))

	// Step 1: Generate strategic roadmap (phases with milestones)
	roadmap, err := provider.GenerateStrategicRoadmap(ctx, req)
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
		tasks, err := provider.GenerateMilestoneTasks(ctx, req, firstMilestone)
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
	outcome = "completed"

	// Bump AI usage counter only on successful completion. Failed generations
	// (handled via handleGenerationError above) do not consume a credit.
	if err := database.IncrementAIGenerations(userID); err != nil {
		log.Error(ctx, "processRoadmapGeneration: failed to increment AI counter", err)
	}

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
	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "roadmap_complete",
		DedupKey:         jobID + ":complete",
		Title:            "Your roadmap is ready for review!",
		Body:             fmt.Sprintf("Your %s roadmap has been generated successfully.", req.GoalName),
		Data: map[string]any{
			"roadmap_id": roadmapID,
		},
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
	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "roadmap_failed",
		DedupKey:         jobID + ":failed",
		Title:            "Roadmap Generation Failed",
		Body:             "We encountered an issue. Please try again.",
		Data: map[string]any{
			"roadmap_id": roadmapID,
			"error":      err.Error(),
		},
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

	if !requireAIAck(c) {
		return
	}

	// Get the existing milestone to generate tasks for
	milestone := database.GetMilestone(c, milestoneID)
	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}
	log.Info(ctx, "Got milestone for task generation", milestone.MilestoneID)

	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	userID := c.GetString("user_id")
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	log.Info(ctx, "Goal request received")

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate tasks for this specific milestone
	tasks, err := h.provider.GenerateMilestoneTasks(ctx, goalReq, &milestone)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	// Bump AI usage counter only on success.
	if err := database.IncrementAIGenerations(userID); err != nil {
		log.Error(ctx, "AgentGenerateMilestone: failed to increment AI counter", err)
	}

	log.Info(ctx, "Generated tasks for milestone", len(tasks))

	// Attach tasks to milestone and update
	milestone.Tasks = tasks
	// TODO: Save tasks to database via UpdateMilestoneWithTasks

	utils.DataResponse(c, http.StatusOK, milestone)
}

// AgentGenerateNextMilestoneAsync kicks off an async job to generate tasks
// (and, if warranted by the user's review, adjusted strategic fields) for the
// given milestone. The milestone must already exist in the DB — this endpoint
// does NOT create a roadmap. Progress is broadcast over the existing
// /ws/agent/:jobid websocket.
//
// TODO: prevent concurrent next-milestone jobs for the same milestone_id
// (unique active-job check).
func (h *AgentHandler) AgentGenerateNextMilestoneAsync(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")
	milestoneID := c.Param("milestoneid")

	if !requireAIAck(c) {
		return
	}

	var req types.GenerateNextMilestoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	milestone := database.GetMilestone(c, milestoneID)
	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	log.Info(ctx, "Next milestone generation request received", milestoneID)

	job := types.BackgroundJob{
		JobID:    uuid.NewString(),
		UserID:   userID,
		Type:     string(types.JobTypeMilestone),
		Status:   string(types.JobStatusPending),
		Progress: 0,
		Payload:  req,
	}

	if err := database.CreateBackgroundJob(c, &job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	bgCtx := log.WithLogger(context.Background(), log.NewLogger(job.JobID))
	go processNextMilestoneGeneration(bgCtx, h.provider, job.JobID, milestoneID, userID, req)

	c.JSON(http.StatusAccepted, gin.H{
		"status":       job.Status,
		"job_id":       job.JobID,
		"milestone_id": milestoneID,
		"roadmap_id":   milestone.RoadmapID,
		"message":      "Your next milestone is being generated",
		"ws_url":       fmt.Sprintf("ws/agent/%s", job.JobID),
	})
}

// processNextMilestoneGeneration runs the two-step AI flow (adjustments +
// tasks) for the "generate next milestone" endpoint, broadcasting progress
// over the existing websocket infrastructure.
func processNextMilestoneGeneration(ctx context.Context, provider agent.Provider, jobID, milestoneID, userID string, req types.GenerateNextMilestoneRequest) {
	log.Info(ctx, "Starting next milestone generation process",
		fmt.Sprintf("JobID: %s, MilestoneID: %s, UserID: %s", jobID, milestoneID, userID))
	log.Info(ctx, "AI provider selected", fmt.Sprintf("provider=%s, JobID: %s", provider.Name(), jobID))

	updateJobProgress(jobID, 5, string(types.JobStatusProcessing), "Reviewing your milestone feedback...")

	// Reload milestone with Spikes/Risks preloaded so buildMilestoneContext
	// has full strategic context for the adjustments prompt.
	milestone := database.GetMilestoneForGeneration(userID, milestoneID)
	if milestone.MilestoneID == "" {
		handleNextMilestoneError(jobID, milestoneID, userID, fmt.Errorf("milestone not found"))
		return
	}

	updateJobProgress(jobID, 15, string(types.JobStatusProcessing), "Analyzing what to adjust...")

	adjustments, tasks, err := provider.GenerateNextMilestoneFromReview(ctx, req.GoalRequest, &milestone, req.Review)
	if err != nil {
		log.Info(ctx, "Next milestone generation failed", fmt.Sprintf("JobID: %s, Error: %v", jobID, err))
		handleNextMilestoneError(jobID, milestoneID, userID, err)
		return
	}

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:     "progress",
		JobID:    jobID,
		Progress: 45,
		Status:   string(types.JobStatusProcessing),
		Message:  "Milestone reviewed — generating tasks...",
		Data: map[string]any{
			"adjusted":  adjustments.Adjusted,
			"rationale": adjustments.Rationale,
		},
	})

	updateJobProgress(jobID, 80, string(types.JobStatusProcessing), "Saving tasks...")

	if err := database.SaveNextMilestone(ctx, milestoneID, adjustments, tasks); err != nil {
		log.Info(ctx, "Next milestone save failed", fmt.Sprintf("JobID: %s, Error: %v", jobID, err))
		handleNextMilestoneError(jobID, milestoneID, userID, err)
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
			"milestone_id": milestoneID,
			"roadmap_id":   milestone.RoadmapID,
			"adjusted":     adjustments.Adjusted,
		},
	}
	_ = database.UpdateBackgroundJob(ctx, &job)

	// Bump AI usage counter only on successful completion.
	if err := database.IncrementAIGenerations(userID); err != nil {
		log.Error(ctx, "processNextMilestoneGeneration: failed to increment AI counter", err)
	}

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:     "complete",
		JobID:    jobID,
		Progress: 100,
		Status:   string(types.JobStatusCompleted),
		Message:  "Your next milestone is ready!",
		Data: map[string]any{
			"milestone_id": milestoneID,
			"roadmap_id":   milestone.RoadmapID,
			"adjusted":     adjustments.Adjusted,
		},
	})

	log.Info(ctx, "Sending next milestone completion push notification",
		fmt.Sprintf("UserID: %s, MilestoneID: %s", userID, milestoneID))
	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "next_milestone_complete",
		DedupKey:         jobID + ":complete",
		Title:            "Your next milestone is ready!",
		Body:             fmt.Sprintf("%s is ready to start.", milestone.SubGoal),
		Data: map[string]any{
			"milestone_id": milestoneID,
			"roadmap_id":   milestone.RoadmapID,
		},
	})
}

// handleNextMilestoneError marks the job failed and notifies the user, WITHOUT
// touching the roadmap's generation_status (the roadmap itself is already
// generated and should not be marked failed).
func handleNextMilestoneError(jobID, milestoneID, userID string, err error) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(jobID))
	log.Info(ctx, "Handling next milestone generation error",
		fmt.Sprintf("JobID: %s, MilestoneID: %s, Error: %v", jobID, milestoneID, err))
	now := time.Now()

	job := types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusFailed),
		Error:       err.Error(),
		CompletedAt: &now,
	}
	_ = database.UpdateBackgroundJob(ctx, &job)

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:    "error",
		JobID:   jobID,
		Status:  string(types.JobStatusFailed),
		Message: "Failed to generate next milestone",
		Data:    map[string]string{"error": err.Error()},
	})

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "next_milestone_failed",
		DedupKey:         jobID + ":failed",
		Title:            "Next Milestone Generation Failed",
		Body:             "We encountered an issue. Please try again.",
		Data: map[string]any{
			"milestone_id": milestoneID,
			"error":        err.Error(),
		},
	})
}

// sessionsDeprecationMessage points callers at the supported conversational
// endpoint. The /agent/sessions handlers never had a working implementation
// (no persistence, no model call), so they return 410 Gone rather than silently
// succeeding with empty data.
const sessionsDeprecationMessage = "The /agent/sessions endpoints are deprecated and were never functional. " +
	"Use POST /api/v1/agent/chat for conversational roadmap generation."

// StartAISession is deprecated.
//
// Deprecated: superseded by POST /agent/chat. Returns 410 Gone.
func (h *AgentHandler) StartAISession(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusGone, sessionsDeprecationMessage)
}

// ReplyAISession is deprecated.
//
// Deprecated: superseded by POST /agent/chat. Returns 410 Gone.
func (h *AgentHandler) ReplyAISession(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusGone, sessionsDeprecationMessage)
}

// GetAISession is deprecated.
//
// Deprecated: superseded by POST /agent/chat. Returns 410 Gone.
func (h *AgentHandler) GetAISession(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusGone, sessionsDeprecationMessage)
}

// ─── Anthropic Chat Handlers ─────────────────────────────────────────────────

// AgentChatRequest is the request body for the Anthropic chat endpoint
type AgentChatRequest struct {
	// SessionID is optional on first message; a new one is created and returned if absent
	SessionID string              `json:"session_id"`
	Message   string              `json:"message" binding:"required"`
	Mode      anthropic.AgentMode `json:"mode" binding:"required"`
	// Context is flexible — the caller populates it with whatever the agent needs:
	// For roadmap generation: { "goal_draft": "...", "user_level": "..." }
	// For performance coach: { "habits": [...], "milestone": {...}, "completion_rates": {...} }
	// For roadmap editor: { "current_roadmap": {...} }
	Context map[string]any `json:"context,omitempty"`
}

// AgentChatResponse is the response body for the Anthropic chat endpoint
type AgentChatResponse struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	// StructuredData is populated only when the agent completes its task and calls a tool.
	// The shape depends on the mode:
	//   roadmap_generation  → anthropic.RoadmapOutput
	//   performance_coach   → anthropic.PerformanceReportOutput
	//   roadmap_editor      → anthropic.RoadmapEditOutput
	StructuredData any    `json:"structured_data,omitempty"`
	ToolUsed       string `json:"tool_used,omitempty"`
	IsComplete     bool   `json:"is_complete"`
}

// Chat handles a single turn of conversation with the Dead Reckoner agent using Anthropic.
//
// POST /api/v1/agent/chat
//
// The client sends a message and receives:
//   - A human-readable reply (always present)
//   - Structured data (only when the agent completes its task)
//   - is_complete flag so the client knows when to save the result to the DB
func (h *AgentHandler) Chat(c *gin.Context) {
	if h.anthropicService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Anthropic service not configured. Set ANTHROPIC_API_KEY environment variable."})
		return
	}

	if !requireAIAck(c) {
		return
	}

	var req AgentChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate mode
	switch req.Mode {
	case anthropic.ModeRoadmapGeneration, anthropic.ModePerformanceCoach, anthropic.ModeRoadmapEditor:
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode, must be one of: roadmap_generation, performance_coach, roadmap_editor"})
		return
	}

	// Generate session ID if not provided (first message in conversation)
	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}

	agentReq := anthropic.AgentRequest{
		SessionID:   req.SessionID,
		UserMessage: req.Message,
		Mode:        req.Mode,
		Context:     req.Context,
	}

	resp, err := h.anthropicService.Run(c.Request.Context(), agentReq)
	if err != nil {
		log.Info(c.Request.Context(), "Anthropic agent error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "agent error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, AgentChatResponse{
		SessionID:      resp.SessionID,
		Message:        resp.Message,
		StructuredData: resp.StructuredData,
		ToolUsed:       resp.ToolUsed,
		IsComplete:     resp.IsComplete,
	})
}

// EndChatSession explicitly clears a session from memory once the client has saved
// the structured output to the database.
//
// DELETE /api/v1/agent/chat/session/:session_id
func (h *AgentHandler) EndChatSession(c *gin.Context) {
	if h.anthropicService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Anthropic service not configured"})
		return
	}

	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id required"})
		return
	}

	h.anthropicService.DeleteSession(sessionID)
	c.JSON(http.StatusOK, gin.H{"deleted": sessionID})
}
