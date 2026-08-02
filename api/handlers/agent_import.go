package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"dr/server/agent/anthropic"
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"dr/server/websocket"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// planUploadEnvVar overrides the upload cap per environment, in bytes.
	planUploadEnvVar = "PLAN_IMPORT_MAX_BYTES"

	// defaultPlanUploadBytes is the cap when PLAN_IMPORT_MAX_BYTES is unset:
	// enough for any realistic plan document while keeping request size and
	// per-import token spend modest.
	defaultPlanUploadBytes = 10 << 20

	// planUploadCeilingBytes is the largest cap we accept, whatever the env
	// says. The Messages API rejects a request over 32MB and base64 inflates
	// the PDF by 4/3, putting the theoretical maximum near 24MB; 20MB leaves
	// room for the prompt. A larger configured value is clamped to this rather
	// than honored, so a misconfiguration surfaces as a 413 we control instead
	// of a 400 from the API mid-job.
	planUploadCeilingBytes = 20 << 20

	// planUploadSlackBytes is headroom over the file cap for multipart framing
	// and the context field, so the body limit rejects only genuinely oversized
	// uploads and the file-size check owns the precise error message.
	planUploadSlackBytes = 1 << 20

	// planFileField and planContextField are the multipart field names.
	planFileField    = "file"
	planContextField = "context"
)

// pdfMagic is the header every PDF starts with. Checked in addition to the
// declared content type, which the client controls and can get wrong.
var pdfMagic = []byte("%PDF-")

// planUploadLimit resolves the upload cap from PLAN_IMPORT_MAX_BYTES, falling
// back to the default when unset and clamping to what the Messages API can
// actually accept. Resolved per request rather than at startup: env is fixed
// for the process lifetime either way, and one Getenv is nothing next to a
// multi-megabyte upload followed by a model call.
//
// A junk or out-of-range value warns and falls back rather than failing the
// request — a typo in one environment's config shouldn't take plan import down.
func planUploadLimit(ctx context.Context) int64 {
	raw := strings.TrimSpace(os.Getenv(planUploadEnvVar))
	if raw == "" {
		return defaultPlanUploadBytes
	}

	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit <= 0 {
		log.Warn(ctx, "invalid "+planUploadEnvVar+"; using default",
			fmt.Sprintf("value=%q, default=%d", raw, int64(defaultPlanUploadBytes)))
		return defaultPlanUploadBytes
	}
	if limit > planUploadCeilingBytes {
		log.Warn(ctx, planUploadEnvVar+" exceeds what the Messages API accepts; clamping",
			fmt.Sprintf("value=%d, ceiling=%d", limit, int64(planUploadCeilingBytes)))
		return planUploadCeilingBytes
	}
	return limit
}

// ImportPlan handles POST /api/v1/agent/import-plan: the user uploads a plan or
// roadmap they already have as a PDF, and Claude transposes it onto the
// Roadmap → Phase → Milestone → Task → Step model.
//
// Request is multipart/form-data:
//   - "file"    (required) the PDF
//   - "context" (optional) a JSON object matching types.PlanImportContext,
//     used only to fill gaps the document leaves open
//
// Like /agent/generate-async this returns 202 immediately with a job_id and a
// draft roadmap_id, and reports progress over /ws/agent/:jobid.
func (h *AgentHandler) ImportPlan(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	// PDF input is Anthropic-specific, so this endpoint does not follow the
	// AI_PROVIDER switch the generation endpoints use.
	if h.importer == nil {
		utils.ErrorResponse(c, http.StatusServiceUnavailable,
			"Plan import is not configured. Set ANTHROPIC_API_KEY environment variable.")
		return
	}

	if !requireAIAck(c) {
		return
	}

	// Read the file before the context field: reading the file is what parses
	// the multipart body, and it's the path that reports an oversized upload
	// properly. Touching c.PostForm first would parse the body behind our back
	// and swallow that error.
	pdf, filename, ok := readPlanUpload(c)
	if !ok {
		return
	}

	hints, ok := parsePlanImportContext(c)
	if !ok {
		return
	}

	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	log.Info(ctx, "Plan import request received", fmt.Sprintf("filename=%s, bytes=%d", filename, len(pdf)))

	job := types.BackgroundJob{
		JobID:    uuid.NewString(),
		UserID:   userID,
		Type:     string(types.JobTypeImport),
		Status:   string(types.JobStatusPending),
		Progress: 0,
		// The PDF itself is deliberately not persisted — the payload records
		// what the import was asked to do, not the document.
		Payload: map[string]any{
			"filename":   filename,
			"size_bytes": len(pdf),
			"context":    hints,
		},
	}

	if err := database.CreateBackgroundJob(c, &job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	// Goal is a placeholder until the model reads the document; the draft exists
	// so the client has a roadmap_id to navigate to while the import runs.
	goal := hints.GoalName
	if goal == "" {
		goal = "Imported plan"
	}
	draft := types.Roadmap{
		RoadmapID:        uuid.NewString(),
		UserID:           userID,
		Goal:             goal,
		Type:             string(types.RoadmapTypeAIGenerated),
		JobID:            &job.JobID,
		GenerationStatus: string(types.GenerationStatusProcessing),
	}

	if err := database.CreateDraftRoadmap(c, &draft); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create draft"})
		return
	}

	// Background context so the work survives the HTTP request completing,
	// with the job ID as the correlation ID.
	bgCtx := log.WithLogger(context.Background(), log.NewLogger(job.JobID))
	go processPlanImport(bgCtx, h.importer, job.JobID, draft.RoadmapID, userID, pdf, hints)

	c.JSON(http.StatusAccepted, gin.H{
		"status":     job.Status,
		"job_id":     job.JobID,
		"roadmap_id": draft.RoadmapID,
		"message":    "Your plan is being imported",
		"ws_url":     fmt.Sprintf("ws/agent/%s", job.JobID),
	})
}

// parsePlanImportContext reads the optional "context" form field. An absent
// field is fine; a malformed one is a 400 rather than a silent ignore, so a
// client bug doesn't quietly cost the user an AI generation.
func parsePlanImportContext(c *gin.Context) (types.PlanImportContext, bool) {
	var hints types.PlanImportContext
	raw := c.PostForm(planContextField)
	if raw == "" {
		return hints, true
	}
	if err := json.Unmarshal([]byte(raw), &hints); err != nil {
		utils.BadRequestResponse(c, "invalid context JSON: "+err.Error())
		return hints, false
	}
	return hints, true
}

// readPlanUpload pulls the PDF out of the multipart body.
//
// The body is capped before parsing: net/http buffers up to 32MB of a multipart
// form in memory before any handler code runs, so without MaxBytesReader an
// oversized upload would be fully buffered only to be rejected afterwards.
func readPlanUpload(c *gin.Context) ([]byte, string, bool) {
	limit := planUploadLimit(c.Request.Context())
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+planUploadSlackBytes)

	file, header, err := c.Request.FormFile(planFileField)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			utils.ErrorResponse(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("PDF is too large; the limit is %d bytes", limit))
			return nil, "", false
		}
		utils.BadRequestResponse(c, "a PDF file is required in the \"file\" field")
		return nil, "", false
	}
	defer func() { _ = file.Close() }()

	if header.Size > limit {
		utils.ErrorResponse(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("PDF is too large (%d bytes); the limit is %d bytes", header.Size, limit))
		return nil, "", false
	}

	data, err := readCappedUpload(file, limit)
	if err != nil {
		if errors.Is(err, errUploadTooLarge) {
			utils.ErrorResponse(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("PDF is too large; the limit is %d bytes", limit))
			return nil, "", false
		}
		utils.InternalErrorResponse(c, err)
		return nil, "", false
	}

	if !isPDF(data) {
		utils.BadRequestResponse(c, "the uploaded file is not a PDF")
		return nil, "", false
	}

	return data, sanitizeUploadName(header), true
}

var errUploadTooLarge = errors.New("upload exceeds size limit")

// readCappedUpload reads at most limit bytes, returning errUploadTooLarge if
// the stream had more. Guards against a header.Size that understates the body.
func readCappedUpload(file multipart.File, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errUploadTooLarge
	}
	return data, nil
}

func isPDF(data []byte) bool {
	return bytes.HasPrefix(data, pdfMagic)
}

// sanitizeUploadName returns a filename safe to log and store in the job
// payload: the client-supplied name is untrusted, so it is truncated and used
// for display only — never as a storage path.
func sanitizeUploadName(header *multipart.FileHeader) string {
	name := header.Filename
	if name == "" {
		return "plan.pdf"
	}
	const maxNameLen = 120
	if len(name) > maxNameLen {
		return name[:maxNameLen]
	}
	return name
}

// processPlanImport runs the transposition and persists the result, mirroring
// processRoadmapGeneration's job/websocket/notification lifecycle.
func processPlanImport(ctx context.Context, importer *anthropic.Provider, jobID, roadmapID, userID string, pdf []byte, hints types.PlanImportContext) {
	// Same end-to-end timing event as roadmap generation, so both AI paths
	// chart the same way in Axiom.
	start := time.Now()
	outcome := "failed"
	defer func() {
		log.FromContext(ctx).LogAttrs(log.LevelInfo, "plan import finished",
			slog.String("job_id", jobID),
			slog.String("roadmap_id", roadmapID),
			slog.String("user_id", userID),
			slog.String("outcome", outcome),
			slog.Int64("generation_ms", time.Since(start).Milliseconds()),
		)
	}()

	log.Info(ctx, "Starting plan import process", fmt.Sprintf("JobID: %s, RoadmapID: %s, UserID: %s", jobID, roadmapID, userID))

	updateJobProgress(jobID, 5, string(types.JobStatusProcessing), "Reading your plan...")
	updateJobProgress(jobID, 15, string(types.JobStatusProcessing), "Transposing your plan onto a roadmap...")

	imported, err := importer.ImportPlanFromPDF(ctx, pdf, hints, time.Now())
	if err != nil {
		log.Info(ctx, "Plan import failed", fmt.Sprintf("JobID: %s, Error: %v", jobID, err))
		handleImportError(jobID, roadmapID, userID, err)
		return
	}

	updateJobProgress(jobID, 80, string(types.JobStatusProcessing), "Saving your roadmap...")

	roadmap := imported.Roadmap
	roadmap.RoadmapID = roadmapID
	roadmap.UserID = userID
	roadmap.Type = string(types.RoadmapTypeAIGenerated)

	if err := database.UpdateRoadmapWithGeneration(ctx, &roadmap); err != nil {
		handleImportError(jobID, roadmapID, userID, err)
		return
	}

	updateJobProgress(jobID, 95, string(types.JobStatusProcessing), "Finalizing...")

	result := map[string]any{
		"roadmap_id":   roadmapID,
		"import_notes": imported.Notes,
	}

	now := time.Now()
	job := types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusCompleted),
		Progress:    100,
		CompletedAt: &now,
		Result:      result,
	}
	_ = database.UpdateBackgroundJob(ctx, &job)
	_ = database.UpdateRoadmapGenerationStatus(ctx, roadmapID, string(types.GenerationStatusCompleted))
	outcome = "completed"

	// AI usage counts only on success, matching the generation endpoints.
	if err := database.IncrementAIGenerations(userID); err != nil {
		log.Error(ctx, "processPlanImport: failed to increment AI counter", err)
	}

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:     "complete",
		JobID:    jobID,
		Progress: 100,
		Status:   string(types.JobStatusCompleted),
		Message:  "Your plan is ready for review...",
		Data:     result,
	})

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "roadmap_complete",
		DedupKey:         jobID + ":complete",
		Title:            "Your imported plan is ready for review!",
		Body:             fmt.Sprintf("%s has been imported as a roadmap.", roadmap.Goal),
		Data: map[string]any{
			"roadmap_id": roadmapID,
		},
	})
	log.Info(ctx, "Plan import completed", fmt.Sprintf("JobID: %s, RoadmapID: %s", jobID, roadmapID))
}

// handleImportError marks the job failed and tells the user why in terms they
// can act on — a declined or oversized document is the user's to fix, whereas
// anything else is ours.
func handleImportError(jobID, roadmapID, userID string, err error) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(jobID))
	log.Info(ctx, "Handling plan import error", fmt.Sprintf("JobID: %s, RoadmapID: %s, Error: %v", jobID, roadmapID, err))
	now := time.Now()

	job := types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusFailed),
		Error:       err.Error(),
		CompletedAt: &now,
	}
	_ = database.UpdateBackgroundJob(ctx, &job)
	_ = database.UpdateRoadmapGenerationStatus(ctx, roadmapID, string(types.GenerationStatusFailed))

	message := importFailureMessage(err)

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:    "error",
		JobID:   jobID,
		Status:  string(types.JobStatusFailed),
		Message: message,
		Data:    map[string]string{"error": err.Error()},
	})

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "roadmap_failed",
		DedupKey:         jobID + ":failed",
		Title:            "Plan Import Failed",
		Body:             message,
		Data: map[string]any{
			"roadmap_id": roadmapID,
			"error":      err.Error(),
		},
	})
}

// importFailureMessage maps an import error to user-facing copy.
func importFailureMessage(err error) string {
	switch {
	case errors.Is(err, anthropic.ErrPlanRefused):
		return "We couldn't import that document. Try uploading the plan itself, without unrelated material."
	case errors.Is(err, anthropic.ErrPlanTooLarge):
		return "That plan was too long to import in one go. Try splitting it into smaller documents."
	default:
		return "We encountered an issue importing your plan. Please try again."
	}
}
