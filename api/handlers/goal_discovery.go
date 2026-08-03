package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dr/server/agent"
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"dr/server/websocket"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Form-based goal discovery (stage 1 → options → stage 2 → roadmap).
//
// Everything here rides the EXISTING async-job infrastructure: background_jobs
// rows, the /jobs/:jobid/status poll, the /ws/agent/:jobid socket, and
// notifications.Dispatch. Stage 2 does not generate anything itself — it adapts
// its payload into types.GoalRequest and hands off to processRoadmapGeneration,
// the same function the known-goal flow uses.

const (
	// goalOptionsCooldown bounds how fast one user can start DISTINCT
	// goal-option generations. Idempotent repeats bypass it (they return the
	// existing job); this only stops a client hammering "Show me different
	// options" into unbounded model spend. Concurrency is handled separately —
	// an in-flight job blocks a second one outright.
	goalOptionsCooldown = 10 * time.Second

	// goalOptionsStaleAfter bounds how long an in-flight job blocks a new one.
	// A worker that dies mid-generation leaves its job 'processing' forever;
	// past this age the job is treated as abandoned so one crash cannot lock a
	// user out of the feature. Well beyond a real generation, which is minutes.
	goalOptionsStaleAfter = 15 * time.Minute

	// maxGoalDiscoveryBodyBytes caps the request body. Stage 2 is the larger of
	// the two payloads and fits in a few KB; 64KB leaves generous headroom while
	// refusing anything designed to inflate the prompt.
	maxGoalDiscoveryBodyBytes = 64 << 10
)

// goalOptionsRunner and goalDiscoveryRoadmapRunner are the async hand-offs. They
// exist as variables so tests can exercise the 202 success path — every guard,
// the job insert, and the response — without a real generation goroutine racing
// the test's mock database. Production always uses the defaults below.
var (
	goalOptionsRunner = func(ctx context.Context, provider agent.Provider, jobID, userID string, in types.GoalOptionsInput) {
		go processGoalOptionsGeneration(ctx, provider, jobID, userID, in)
	}
	goalDiscoveryRoadmapRunner = func(ctx context.Context, provider agent.Provider, jobID, roadmapID, userID string, req types.GoalRequest) {
		go processRoadmapGeneration(ctx, provider, jobID, roadmapID, userID, req)
	}
)

// GoalDiscoveryRunnerCapture records what the stubbed async hand-offs were
// handed, so a test can assert on the generation input and the adapted
// GoalRequest instead of reverse-engineering them from SQL.
type GoalDiscoveryRunnerCapture struct {
	OptionsInputs   []types.GoalOptionsInput
	RoadmapRequests []types.GoalRequest
}

// SetGoalDiscoveryRunnersForTest replaces both async hand-offs with capturing
// no-ops and returns the capture plus a cleanup hook restoring the originals.
// ONLY for tests. The stubs run inline on the request goroutine, so the capture
// needs no synchronisation.
func SetGoalDiscoveryRunnersForTest() (*GoalDiscoveryRunnerCapture, func()) {
	capture := &GoalDiscoveryRunnerCapture{}
	origOptions, origRoadmap := goalOptionsRunner, goalDiscoveryRoadmapRunner

	goalOptionsRunner = func(_ context.Context, _ agent.Provider, _, _ string, in types.GoalOptionsInput) {
		capture.OptionsInputs = append(capture.OptionsInputs, in)
	}
	goalDiscoveryRoadmapRunner = func(_ context.Context, _ agent.Provider, _, _, _ string, req types.GoalRequest) {
		capture.RoadmapRequests = append(capture.RoadmapRequests, req)
	}

	return capture, func() {
		goalOptionsRunner, goalDiscoveryRoadmapRunner = origOptions, origRoadmap
	}
}

// goalOptionsRequest is the body for creating (or regenerating) an option set.
type goalOptionsRequest struct {
	Answers types.GoalDiscoveryAnswers `json:"answers"`

	// PreviousJobID names the completed job whose options the user rejected.
	// The server reads the excluded titles from that job's stored result rather
	// than trusting a client-supplied list.
	PreviousJobID string `json:"previous_job_id,omitempty"`

	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// CreateGoalOptions handles POST /agent/goal-discovery/options.
//
// Used for both the first generation and every regeneration — regeneration is
// the same request with previous_job_id set. Responds 202 with the job id; the
// client polls /jobs/:jobid/status and/or opens /ws/agent/:jobid, exactly as it
// already does for roadmap generation.
func (h *AgentHandler) CreateGoalOptions(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	if !requireAIAck(c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGoalDiscoveryBodyBytes)
	var req goalOptionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestResponse(c, "Invalid request body")
		return
	}

	req.Answers.Normalize()
	if err := req.Answers.Validate(); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.PreviousJobID = strings.TrimSpace(req.PreviousJobID)

	// Idempotent repeat: the same key returns the job it already created rather
	// than starting a second generation. This is what makes a retried request
	// after a dropped response safe.
	if existing, err := database.FindJobByIdempotencyKey(userID, types.JobTypeGoalDiscovery, req.IdempotencyKey); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	} else if existing != nil {
		c.JSON(http.StatusAccepted, goalOptionsJobResponse(existing.JobID, existing.Status))
		return
	}

	// Concurrency: one in-flight option generation per user.
	active, err := database.CountActiveJobsOfType(userID, types.JobTypeGoalDiscovery, goalOptionsStaleAfter)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	if active > 0 {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"code":    "generation_in_progress",
			"message": "Your goal options are still being generated. Wait for that to finish before starting another.",
		})
		return
	}

	// Cooldown between distinct generations.
	lastAt, err := database.LatestJobCreatedAt(userID, types.JobTypeGoalDiscovery)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	if !lastAt.IsZero() && time.Since(lastAt) < goalOptionsCooldown {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"code":    "rate_limited",
			"message": "You're generating options too quickly. Give it a few seconds and try again.",
		})
		return
	}

	// Same entitlement rule as every other AI generation.
	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	// Exclusions come from the SERVER's copy of the previous result. A client
	// cannot inject arbitrary text into the prompt this way.
	excludeTitles, err := h.excludedTitlesFor(userID, req.PreviousJobID)
	if err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}

	job := types.BackgroundJob{
		JobID:    uuid.NewString(),
		UserID:   userID,
		Type:     string(types.JobTypeGoalDiscovery),
		Status:   string(types.JobStatusPending),
		Progress: 0,
		Payload: types.GoalDiscoveryJobPayload{
			Answers:        req.Answers,
			ExcludeTitles:  excludeTitles,
			PreviousJobID:  req.PreviousJobID,
			IdempotencyKey: req.IdempotencyKey,
			PromptVersion:  types.GoalOptionsPromptVersion,
		},
	}
	if err := database.CreateJob(&job); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	log.Info(ctx, "Goal options generation requested",
		fmt.Sprintf("job_id=%s regeneration=%t excluded=%d", job.JobID, req.PreviousJobID != "", len(excludeTitles)))

	bgCtx := log.WithLogger(context.Background(), log.NewLogger(job.JobID))
	goalOptionsRunner(bgCtx, h.provider, job.JobID, userID, types.GoalOptionsInput{
		Answers: req.Answers,
		Exclude: excludeTitles,
	})

	c.JSON(http.StatusAccepted, goalOptionsJobResponse(job.JobID, job.Status))
}

// excludedTitlesFor reads the goal titles the user already rejected out of the
// previous job's stored result. Returns nil for a first-time generation.
func (h *AgentHandler) excludedTitlesFor(userID, previousJobID string) ([]string, error) {
	if previousJobID == "" {
		return nil, nil
	}

	job, err := database.GetGoalDiscoveryJob(userID, previousJobID)
	if err != nil {
		// Deliberately the same message whether the job is missing or belongs to
		// somebody else: a 404-vs-403 distinction would confirm the existence of
		// another user's job.
		return nil, errors.New("previous_job_id does not refer to one of your goal discovery results")
	}
	result, err := decodeGoalOptionsResult(job.Result)
	if err != nil || result == nil {
		return nil, nil
	}

	titles := make([]string, 0, len(result.Options))
	for _, opt := range result.Options {
		titles = append(titles, opt.GoalTitle)
	}
	return titles, nil
}

func goalOptionsJobResponse(jobID, status string) gin.H {
	return gin.H{
		"status":  status,
		"job_id":  jobID,
		"message": "Your goal options are being generated",
		"ws_url":  fmt.Sprintf("ws/agent/%s", jobID),
	}
}

// processGoalOptionsGeneration runs the generation off the request goroutine and
// reports progress over the same websocket/job-status machinery as roadmaps.
//
// The job row is the ONLY thing this mutates: a failure leaves the user's
// previously completed job (and therefore their current three options) exactly
// as it was, which is what lets the client keep showing them.
func processGoalOptionsGeneration(ctx context.Context, provider agent.Provider, jobID, userID string, in types.GoalOptionsInput) {
	// A panic in this goroutine would take the whole process down — it runs
	// outside the HTTP stack, so gin's Recovery middleware cannot see it. Turn
	// it into a failed job instead, the same outcome the user would get from any
	// other generation error.
	defer func() {
		if r := recover(); r != nil {
			log.Error(ctx, "Goal options generation panicked",
				fmt.Errorf("job_id=%s panic=%v", jobID, r))
			failGoalOptionsJob(jobID, userID, fmt.Errorf("panic: %v", r))
		}
	}()

	start := time.Now()
	log.Info(ctx, "Starting goal options generation",
		fmt.Sprintf("job_id=%s user_id=%s provider=%s prompt=%s", jobID, userID, provider.Name(), types.GoalOptionsPromptVersion))

	updateJobProgress(jobID, 5, string(types.JobStatusProcessing), "Reading your answers...")
	updateJobProgress(jobID, 25, string(types.JobStatusProcessing), "Looking for goals that fit...")

	out, err := agent.GenerateValidatedGoalOptions(ctx, provider, in)
	if err != nil {
		// The provider's own error text can carry model/provider detail, so it
		// is logged but never handed to the client.
		log.Error(ctx, "Goal options generation failed", err)
		failGoalOptionsJob(jobID, userID, err)
		return
	}

	updateJobProgress(jobID, 85, string(types.JobStatusProcessing), "Checking the options make sense...")

	result := types.GoalOptionsResult{
		Options:       out.Options,
		PromptVersion: types.GoalOptionsPromptVersion,
		GeneratedAt:   time.Now().UTC(),
		Safety:        out.Safety,
	}

	now := time.Now()
	if err := database.UpdateBackgroundJob(ctx, &types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusCompleted),
		Progress:    100,
		CompletedAt: &now,
		Result:      result,
	}); err != nil {
		log.Error(ctx, "Failed to persist goal options result", err)
		failGoalOptionsJob(jobID, userID, err)
		return
	}

	// Only a real generation burns a credit. A safety block does not produce
	// usable options, so it does not consume one.
	if out.Safety == nil {
		if err := database.IncrementAIGenerations(userID); err != nil {
			log.Error(ctx, "processGoalOptionsGeneration: failed to increment AI counter", err)
		}
	}

	log.Info(ctx, "Goal options generation finished",
		fmt.Sprintf("job_id=%s options=%d safety_blocked=%t generation_ms=%d",
			jobID, len(out.Options), out.Safety != nil, time.Since(start).Milliseconds()))

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:     "complete",
		JobID:    jobID,
		Progress: 100,
		Status:   string(types.JobStatusCompleted),
		Message:  "Your goal options are ready",
		Data:     map[string]any{"job_id": jobID},
	})

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "goal_options_complete",
		DedupKey:         jobID + ":complete",
		Title:            "Your goal options are ready",
		Body:             "Three goals picked for what you told us. Take a look and choose one.",
		Data:             map[string]any{"goal_discovery_job_id": jobID},
	})
}

// GoalOptionsUserFacingError maps ANY generation failure to one safe message.
//
// The cause is deliberately discarded rather than wrapped: a provider error can
// carry the model name, request ids, quota detail, or a fragment of the prompt,
// and this string is persisted on the job and served straight back to the client
// by GET /jobs/:jobid/status.
//
// Exported so the guarantee is directly testable.
func GoalOptionsUserFacingError(cause error) string {
	return "We couldn't put your goal options together. Your answers were saved — please try again."
}

// failGoalOptionsJob marks the job failed with a message that is safe to show.
func failGoalOptionsJob(jobID, userID string, cause error) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(jobID))
	now := time.Now()

	userFacing := GoalOptionsUserFacingError(cause)

	_ = database.UpdateBackgroundJob(ctx, &types.BackgroundJob{
		JobID:       jobID,
		Status:      string(types.JobStatusFailed),
		Error:       userFacing,
		CompletedAt: &now,
	})

	websocket.Manager.BroadcastToJob(ctx, jobID, types.ProgressUpdate{
		Type:    "error",
		JobID:   jobID,
		Status:  string(types.JobStatusFailed),
		Message: userFacing,
	})

	notifications.Dispatch(notifications.DispatchOptions{
		UserID:           userID,
		Category:         notifications.CategoryMyGoals,
		NotificationType: "goal_options_failed",
		DedupKey:         jobID + ":failed",
		Title:            "Goal options didn't generate",
		Body:             "We hit a problem building your options. Your answers were saved — open the app to try again.",
		Data:             map[string]any{"goal_discovery_job_id": jobID},
	})

	log.Info(ctx, "Goal options job marked failed", fmt.Sprintf("job_id=%s cause=%v", jobID, cause))
}

// ─── Stage 2: hand off to the existing roadmap generator ─────────────────────

// CreateGoalDiscoveryRoadmap handles POST /agent/goal-discovery/roadmap.
//
// Validates the stage-2 payload, proves the selected option really came from one
// of THIS user's completed discovery results, then creates a normal
// roadmap_generation job and runs the existing processRoadmapGeneration. From
// this point the flow is indistinguishable from the known-goal path: same job
// type, same draft roadmap, same websocket, same completion notification, same
// destination screen.
func (h *AgentHandler) CreateGoalDiscoveryRoadmap(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	if !requireAIAck(c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGoalDiscoveryBodyBytes)
	var req types.GoalDiscoveryRoadmapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestResponse(c, "Invalid request body")
		return
	}

	req.Normalize()
	// The deadline check is a calendar comparison against the user's local
	// today, which the client sends. Falls back to the server's UTC date when
	// absent, which is the pre-existing behaviour elsewhere in the API.
	if err := req.Validate(clientLocalDate(c)); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}

	// Idempotent repeat returns the roadmap job already created for this key —
	// this is what stops a double tap producing two roadmaps.
	if existing, err := database.FindJobByIdempotencyKey(userID, types.JobTypeRoadmap, req.IdempotencyKey); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	} else if existing != nil {
		roadmapID := ""
		if result, ok := existing.Result.(map[string]any); ok {
			roadmapID, _ = result["roadmap_id"].(string)
		}
		c.JSON(http.StatusAccepted, gin.H{
			"status":     existing.Status,
			"job_id":     existing.JobID,
			"roadmap_id": roadmapID,
			"message":    "Your roadmap is being processed",
			"ws_url":     fmt.Sprintf("ws/agent/%s", existing.JobID),
		})
		return
	}

	// Provenance: the option must exist in a COMPLETED discovery job owned by
	// this user. A client-supplied option is never trusted as though the server
	// generated it.
	discoveryJob, err := database.GetGoalDiscoveryJob(userID, req.DiscoveryJobID)
	if err != nil {
		utils.BadRequestResponse(c, "discovery_job_id does not refer to one of your goal discovery results")
		return
	}
	if discoveryJob.Status != string(types.JobStatusCompleted) {
		utils.BadRequestResponse(c, "That goal discovery result is not ready yet")
		return
	}
	result, err := decodeGoalOptionsResult(discoveryJob.Result)
	if err != nil || result == nil {
		utils.BadRequestResponse(c, "That goal discovery result is no longer available")
		return
	}
	selected, found := findOptionByID(result.Options, req.SelectedOptionID)
	if !found {
		utils.BadRequestResponse(c, "selected_option_id is not one of the options generated for you")
		return
	}

	if !utils.EnforceLimit(c, func() (*types.PlanLimitError, error) {
		return database.CheckAIGeneration(userID)
	}) {
		return
	}

	goalReq := BuildGoalRequestFromDiscovery(req, selected)

	job := types.BackgroundJob{
		JobID:    uuid.NewString(),
		UserID:   userID,
		Type:     string(types.JobTypeRoadmap),
		Status:   string(types.JobStatusPending),
		Progress: 0,
		Payload: types.GoalDiscoveryRoadmapJobPayload{
			GoalRequest:      goalReq,
			IdempotencyKey:   req.IdempotencyKey,
			DiscoveryJobID:   req.DiscoveryJobID,
			SelectedOptionID: req.SelectedOptionID,
		},
	}
	if err := database.CreateJob(&job); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	draft := types.Roadmap{
		RoadmapID:        uuid.NewString(),
		UserID:           userID,
		Goal:             goalReq.GoalName,
		Description:      goalReq.GoalDescription,
		Type:             string(types.RoadmapTypeAIGenerated),
		JobID:            &job.JobID,
		GenerationStatus: string(types.GenerationStatusProcessing),
	}
	if err := database.CreateDraftRoadmap(c, &draft); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	log.Info(ctx, "Goal discovery roadmap generation requested",
		fmt.Sprintf("job_id=%s roadmap_id=%s discovery_job_id=%s", job.JobID, draft.RoadmapID, req.DiscoveryJobID))

	bgCtx := log.WithLogger(context.Background(), log.NewLogger(job.JobID))
	goalDiscoveryRoadmapRunner(bgCtx, h.provider, job.JobID, draft.RoadmapID, userID, goalReq)

	c.JSON(http.StatusAccepted, gin.H{
		"status":     job.Status,
		"job_id":     job.JobID,
		"roadmap_id": draft.RoadmapID,
		"message":    "Your roadmap is being processed",
		"ws_url":     fmt.Sprintf("ws/agent/%s", job.JobID),
	})
}

// BuildGoalRequestFromDiscovery adapts a stage-2 payload into the GoalRequest
// the existing roadmap generator already consumes. No new generator, no second
// prompt: the goal-discovery context rides in on AdditionalContext, which the
// shared prompt builder already passes through to the model.
//
// Exported for tests, which assert the mapping directly rather than through HTTP.
func BuildGoalRequestFromDiscovery(req types.GoalDiscoveryRoadmapRequest, selected types.GoalOption) types.GoalRequest {
	return types.GoalRequest{
		GoalName:        req.SelectedGoal,
		GoalDescription: req.CompletionDefinition,
		GoalImportance:  req.WhyNow,
		// Only ideal-deadline mode fixes a date. In the other two modes the
		// generator derives the timeline itself, so this stays empty rather than
		// being guessed at from a pace estimate.
		TargetCompletionDate: deadlineFor(req.Pace),
		WeeklyTimeCommitment: weeklyCommitmentFor(req.Pace, selected),
		WorkFrequency:        workFrequencyFor(req.Pace),
		Constraints:          constraintsText(req),
		CurrentSkillLevel:    req.Baseline,
		AdditionalContext:    buildDiscoveryContext(req, selected),
		Source:               types.GoalDiscoverySource,
	}
}

func deadlineFor(pace types.GoalDiscoveryPaceSelection) string {
	if pace.Mode == types.PaceModeIdealDeadline {
		return pace.TargetDate
	}
	return ""
}

// weeklyCommitmentFor describes the weekly time the generator should plan
// around. A generated pace is stated as a PREFERENCE, not a fixed input: the
// user may have edited the goal after picking it, in which case the original
// estimate no longer describes the work and the generator must recalculate.
func weeklyCommitmentFor(pace types.GoalDiscoveryPaceSelection, selected types.GoalOption) string {
	switch pace.Mode {
	case types.PaceModeGenerated:
		effort, ok := selected.EffortFor(pace.SelectedPace)
		if !ok {
			return fmt.Sprintf("The user prefers a %s pace.", pace.SelectedPace)
		}
		return fmt.Sprintf(
			"The user chose the %s pace, originally estimated at %.0f-%.0f hours per week over roughly %.0f-%.0f months. "+
				"Treat that as a PREFERENCE, not a fixed input: if the goal or completion definition below differs from the "+
				"option this estimate was made for, recalculate the schedule that the stated goal actually requires and keep "+
				"the weekly load as close to the preference as the goal allows.",
			pace.SelectedPace,
			effort.HoursPerWeekMin, effort.HoursPerWeekMax,
			effort.EstimatedDurationMonthsMin, effort.EstimatedDurationMonthsMax,
		)

	case types.PaceModeIdealDeadline:
		return fmt.Sprintf(
			"The user gave a target date of %s instead of a weekly commitment. Work out the weekly pace that date requires "+
				"and state it in the plan. If the date is not realistic for this goal, say so and schedule the most it can "+
				"realistically cover by then.", pace.TargetDate)

	case types.PaceModeWeeklyLimit:
		if pace.Preset == types.WeeklyCustom {
			return fmt.Sprintf(
				"The user described their weekly availability in their own words: %q. Treat it as a hard ceiling and derive "+
					"the timeline from it.", pace.CustomDescription)
		}
		return fmt.Sprintf(
			"The user's weekly availability is fixed at: %s. Treat it as a hard ceiling and derive the timeline from it — "+
				"do not assume more time than this.", weeklyPresetText(pace.Preset))
	}
	return ""
}

// weeklyPresetText renders a preset for the prompt. Deliberately NOT converted
// into an hours number: "2 to 3 sessions a week" has no defensible hour value,
// and inventing one here would silently mislead the generator.
func weeklyPresetText(preset types.GoalDiscoveryWeeklyPreset) string {
	switch preset {
	case types.WeeklyTenMinutesDay:
		return "about 10 minutes a day"
	case types.WeeklyThirtyMinutesDay:
		return "about 30 minutes a day"
	case types.WeeklyTwoToThreeSessions:
		return "2 to 3 sessions a week"
	case types.WeeklyFourToSixHours:
		return "4 to 6 hours a week"
	case types.WeeklySevenToTenHours:
		return "7 to 10 hours a week"
	case types.WeeklyTenPlusHours:
		return "10 or more hours a week"
	}
	return string(preset)
}

func workFrequencyFor(pace types.GoalDiscoveryPaceSelection) string {
	switch pace.Mode {
	case types.PaceModeGenerated:
		return fmt.Sprintf("%s pace", pace.SelectedPace)
	case types.PaceModeIdealDeadline:
		return "whatever cadence the target date requires"
	case types.PaceModeWeeklyLimit:
		if pace.Preset == types.WeeklyCustom {
			return pace.CustomDescription
		}
		return weeklyPresetText(pace.Preset)
	}
	return ""
}

// constraintsText renders the selected constraints plus the user's own detail.
func constraintsText(req types.GoalDiscoveryRoadmapRequest) string {
	var parts []string
	for _, constraint := range req.Constraints {
		if constraint == types.ConstraintNone {
			parts = append(parts, "no major constraints right now")
			continue
		}
		parts = append(parts, strings.ReplaceAll(string(constraint), "_", " "))
	}
	out := strings.Join(parts, ", ")
	if req.ConstraintDetails != "" {
		out += ". Details: " + req.ConstraintDetails
	}
	return out
}

// buildDiscoveryContext assembles the stage-1 answers and the provenance of the
// selected option into the free-text context field the shared roadmap prompt
// already forwards. This is how the generator receives the full picture without
// a second prompt or a change to the known-goal path.
func buildDiscoveryContext(req types.GoalDiscoveryRoadmapRequest, selected types.GoalOption) string {
	var sb strings.Builder

	sb.WriteString("This goal came from Dead Reckoner's guided goal discovery, not from a user who already knew what to pursue.\n\n")
	sb.WriteString("What they said when they started:\n")
	sb.WriteString(fmt.Sprintf("- What prompted them: %s\n", req.Answers.Trigger))
	if req.Answers.TriggerNote != "" {
		sb.WriteString(fmt.Sprintf("- In their words: %s\n", req.Answers.TriggerNote))
	}
	sb.WriteString(fmt.Sprintf("- Areas below standard: %s\n", joinConstraintish(req.Answers.Areas, req.Answers.CustomArea)))
	sb.WriteString(fmt.Sprintf("- How they describe themselves: %s\n", req.Answers.SelfDescription))
	sb.WriteString(fmt.Sprintf("- What they keep thinking about but not acting on: %s\n", req.Answers.RecurringThought))
	sb.WriteString(fmt.Sprintf("- What would make the next few months feel successful: %s\n", req.Answers.SuccessDefinition))
	sb.WriteString(fmt.Sprintf("- What they want out of it: %s\n", joinOutcomeish(req.Answers.Outcomes, req.Answers.CustomOutcome)))
	sb.WriteString(fmt.Sprintf("- Kind of target they wanted: %s\n", req.Answers.TargetType))

	sb.WriteString("\nThe goal option they chose (before their own edits):\n")
	sb.WriteString(fmt.Sprintf("- Title: %s\n", selected.GoalTitle))
	sb.WriteString(fmt.Sprintf("- Target outcome: %s\n", selected.TargetOutcome))
	sb.WriteString(fmt.Sprintf("- Difficulty: %s\n", selected.Difficulty.Level))

	if selected.GoalTitle != req.SelectedGoal || selected.TargetOutcome != req.CompletionDefinition {
		sb.WriteString("\nThe user EDITED the goal and/or the definition of done after choosing it. " +
			"The edited wording above is authoritative — plan for what they actually wrote, and recalculate " +
			"the schedule if the edit changed the size of the work.\n")
	}

	if req.AdditionalContext != "" {
		sb.WriteString(fmt.Sprintf("\nAnything else they wanted considered: %s\n", req.AdditionalContext))
	}
	return sb.String()
}

func joinConstraintish(areas []types.GoalDiscoveryArea, custom string) string {
	parts := make([]string, 0, len(areas)+1)
	for _, a := range areas {
		parts = append(parts, strings.ReplaceAll(string(a), "_", " "))
	}
	if custom != "" {
		parts = append(parts, custom)
	}
	return strings.Join(parts, ", ")
}

func joinOutcomeish(outcomes []types.GoalDiscoveryOutcome, custom string) string {
	parts := make([]string, 0, len(outcomes)+1)
	for _, o := range outcomes {
		parts = append(parts, strings.ReplaceAll(string(o), "_", " "))
	}
	if custom != "" {
		parts = append(parts, custom)
	}
	return strings.Join(parts, ", ")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// decodeGoalOptionsResult reads the JSONB result column back into the typed
// result. GORM hands it back as map[string]any or json.RawMessage depending on
// the driver path, so this round-trips through JSON rather than type-asserting.
func decodeGoalOptionsResult(raw any) (*types.GoalOptionsResult, error) {
	if raw == nil {
		return nil, nil
	}

	var encoded []byte
	switch v := raw.(type) {
	case []byte:
		encoded = v
	case string:
		encoded = []byte(v)
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		encoded = buf
	}

	var result types.GoalOptionsResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func findOptionByID(options []types.GoalOption, id string) (types.GoalOption, bool) {
	for _, opt := range options {
		if opt.ID == id {
			return opt, true
		}
	}
	return types.GoalOption{}, false
}

// clientLocalDateHeader carries the caller's local calendar date so a
// future-deadline check is made in the user's own timezone. A user in UTC-7
// picking "tomorrow" late in the evening would otherwise be rejected for
// choosing a date the server already considers past.
const clientLocalDateHeader = "X-Client-Date"

func clientLocalDate(c *gin.Context) time.Time {
	if raw := strings.TrimSpace(c.GetHeader(clientLocalDateHeader)); raw != "" {
		if parsed, err := time.Parse(types.DateOnlyLayout, raw); err == nil {
			return parsed
		}
	}
	return time.Now().UTC()
}
