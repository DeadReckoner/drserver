package agent

import (
	"bytes"
	"context"
	"dr/server/log"
	"dr/server/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

const (
	// OpenAI API endpoint for structured output responses
	openAIURL             = "https://api.openai.com/v1/responses"
	GenerationTemperature = 0.7
	RequestTimeout        = 60 * time.Second
)

// Message represents a single message in a conversation with the AI
// Used to build up context and chat history
type Message struct {
	Role    string `json:"role"`    // "system", "user", or "assistant"
	Content string `json:"content"` // The actual message content
}

// AISession represents an interactive session where the AI asks clarifying questions
// before generating a roadmap (not currently fully implemented)
type AISession struct {
	SessionID uuid.UUID      `json:"session_id"`
	UserID    string         `json:"user_id"`
	Status    string         `json:"status"`  // "collecting" or "complete"
	Context   []Message      `json:"context"` // chat history
	Roadmap   *types.Roadmap `json:"roadmap,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// AIRequest represents the full request payload sent to OpenAI's API
// Includes the model name, conversation messages, and strict JSON schema formatting
type AIRequest struct {
	Model     string    `json:"model"`                       // e.g., "gpt-5-mini"
	Input     []Message `json:"input"`                       // Conversation messages (system + user prompts)
	MaxTokens int       `json:"max_output_tokens,omitempty"` // Maximum tokens to generatexs
	Stream    bool      `json:"stream,omitempty"`            // Enable streaming responses
	Text      struct {
		Format struct {
			Type   string                 `json:"type"`   // "json_schema"
			Name   string                 `json:"name"`   // Schema name (e.g., "macro_map")
			Schema map[string]interface{} `json:"schema"` // JSON Schema definition
			Strict bool                   `json:"strict"` // Enforce strict validation (all properties must be required)
		} `json:"format"`
	} `json:"text"`
}

// GeneratedRoadmap contains the output from the two-step generation process:
// 1. Strategic: Roadmap with Phases → Milestones (with acceptance tests, durations, spikes, risks)
// 2. Tactical: Tasks and Steps for the first milestone only
type GeneratedRoadmap struct {
	Roadmap *types.Roadmap `json:"roadmap"` // Contains Phases with Milestones, first milestone has Tasks
}

// buildGoalPrompt converts structured goal request into natural language prompt
func buildGoalPrompt(goalReq types.GoalRequest) string {
	prompt := fmt.Sprintf(`
Based on the following goal information, create a detailed roadmap:

Goal: %s
Description: %s
Importance: %s
Target Completion: %s
Weekly Time Commitment: %s
Work Frequency: %s
Constraints: %s
Current Skill Level: %s
`, goalReq.GoalName, goalReq.GoalDescription, goalReq.GoalImportance,
		goalReq.TargetCompletionDate, goalReq.WeeklyTimeCommitment,
		goalReq.WorkFrequency, goalReq.Constraints, goalReq.CurrentSkillLevel)

	if goalReq.AdditionalContext != "" {
		prompt += fmt.Sprintf("Additional Context: %s\n", goalReq.AdditionalContext)
	}

	prompt += "\nPlease create a comprehensive roadmap with milestones, tasks, and actionable steps that takes into account the user's skill level, time commitment, and constraints.\n"

	return prompt
}

// GenerateGoalFromStructure is the main entry point when the user provides structured goal data
// (via a form with fields like goal name, time commitment, skill level, etc.)
// It converts the structured data into a natural language prompt and calls the generation pipeline
func GenerateGoalFromStructure(ctx context.Context, goalReq types.GoalRequest) (*GeneratedRoadmap, error) {
	prompt := buildGoalPrompt(goalReq)
	return generateRoadmapFromPrompt(ctx, prompt)
}

// GenerateStrategicRoadmapFromGoalRequest generates the strategic roadmap (phases with milestones) from a goal request
// This allows for progress tracking between strategic and tactical generation
func GenerateStrategicRoadmapFromGoalRequest(ctx context.Context, goalReq types.GoalRequest) (*types.Roadmap, error) {
	log.Info(ctx, "Starting strategic roadmap generation", fmt.Sprintf("Goal: %s", goalReq.GoalName))
	prompt := buildGoalPrompt(goalReq)
	log.Info(ctx, "User Prompt", prompt)

	// Generate strategic roadmap
	log.Info(ctx, "Calling OpenAI API for strategic roadmap generation", "")
	strategicResp, err := generateStrategicRoadmap(ctx, prompt)
	if err != nil {
		log.Info(ctx, "Strategic roadmap API call failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to generate strategic roadmap: %w", err)
	}
	log.Info(ctx, "Strategic roadmap API call successful", fmt.Sprintf("Response size: %d bytes", len(strategicResp)))

	// Parse the raw JSON response
	roadmap, err := parseStrategicRoadmapResponse(strategicResp)
	if err != nil {
		log.Info(ctx, "Strategic roadmap parsing failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to parse strategic roadmap: %w", err)
	}

	log.Info(ctx, "Strategic roadmap parsed successfully", fmt.Sprintf("Phases: %d", len(roadmap.Phases)))
	return roadmap, nil
}

// GenerateMilestoneTasksFromGoalRequest generates tasks for a specific milestone
// This allows for progress tracking between strategic and tactical generation
func GenerateMilestoneTasksFromGoalRequest(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone) ([]types.Task, error) {
	log.Info(ctx, "Starting milestone tasks generation", fmt.Sprintf("Goal: %s, Milestone: %s", goalReq.GoalName, milestone.SubGoal))
	prompt := buildGoalPrompt(goalReq)

	// Generate tasks using the milestone as context
	log.Info(ctx, "Calling OpenAI API for milestone tasks generation", "")
	tasksResp, err := generateMilestoneTasks(ctx, prompt, milestone)
	if err != nil {
		log.Info(ctx, "Milestone tasks API call failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to generate milestone tasks: %w", err)
	}
	log.Info(ctx, "Milestone tasks API call successful", fmt.Sprintf("Response size: %d bytes", len(tasksResp)))

	// Parse the tasks response
	tasks, err := parseMilestoneTasksResponse(tasksResp)
	if err != nil {
		log.Info(ctx, "Milestone tasks parsing failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to parse tasks: %w", err)
	}

	log.Info(ctx, "Milestone tasks parsed successfully", fmt.Sprintf("Tasks: %d", len(tasks)))
	return tasks, nil
}

// generateRoadmapFromPrompt is the core orchestrator for the two-step generation process
// It coordinates both strategic (phases/milestones) and tactical (tasks/steps) planning
//
// FLOW:
// 1. Generate strategic roadmap (phases with milestones including acceptance tests, durations, spikes, risks)
// 2. Parse the strategic roadmap response
// 3. Generate tasks for the first milestone only
// 4. Attach tasks to the first milestone
// 5. Return the complete roadmap
func generateRoadmapFromPrompt(ctx context.Context, userPrompt string) (*GeneratedRoadmap, error) {

	log.Info(ctx, "User Prompt", userPrompt)
	// STEP 1: Generate Strategic Roadmap
	// This creates the high-level plan with 2-4 phases and milestones with strategic metadata
	strategicResp, err := generateStrategicRoadmap(ctx, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate strategic roadmap: %w", err)
	}

	// Parse the raw JSON response from OpenAI into our Roadmap structure with Phases
	roadmap, err := parseStrategicRoadmapResponse(strategicResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse strategic roadmap: %w", err)
	}

	// STEP 2: Generate Tasks for the first milestone only
	// Find the first milestone across all phases
	var firstMilestone *types.Milestone
	for i := range roadmap.Phases {
		if len(roadmap.Phases[i].Milestones) > 0 {
			firstMilestone = &roadmap.Phases[i].Milestones[0]
			break
		}
	}

	if firstMilestone != nil {
		tasksResp, err := generateMilestoneTasks(ctx, userPrompt, firstMilestone)
		if err != nil {
			return nil, fmt.Errorf("failed to generate milestone tasks: %w", err)
		}

		tasks, err := parseMilestoneTasksResponse(tasksResp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tasks: %w", err)
		}

		firstMilestone.Tasks = tasks
	}

	// Also populate the top-level Milestones array for backward compatibility
	// Flatten all milestones from phases into the Milestones array
	for _, phase := range roadmap.Phases {
		roadmap.Milestones = append(roadmap.Milestones, phase.Milestones...)
	}

	return &GeneratedRoadmap{
		Roadmap: roadmap,
	}, nil
}

// generateStrategicRoadmap creates the strategic high-level plan
// Returns raw JSON response from OpenAI containing phases and milestones
//
// OUTPUT STRUCTURE:
// - Roadmap with goal, description, days
// - 2-4 Phases (major stages like "Foundation", "Intermediate", "Advanced")
// - Each phase has 2-5 Milestones with:
//   - sub_goal (milestone name)
//   - Acceptance tests (how to know it's done)
//   - Duration estimates (min/max days)
//   - Resources forecast (tools, materials needed)
//   - Spikes (time-boxed investigations)
//   - Risks (potential blockers with severity)
//   - NO tasks/steps yet (those are generated in stage 2)
func generateStrategicRoadmap(ctx context.Context, userPrompt string) ([]byte, error) {
	// Define the strict JSON schema that OpenAI must follow
	// NOTE: With strict=true, ALL properties MUST be in the required array
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"goal": map[string]interface{}{
				"type": "string", // Overall goal title
			},
			"description": map[string]interface{}{
				"type": "string", // Description of the roadmap
			},
			"days": map[string]interface{}{
				"type": "integer", // Total estimated days
			},
			"phases": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						// Phase-level properties
						"name": map[string]interface{}{
							"type": "string", // e.g., "Foundation Phase"
						},
						"description": map[string]interface{}{
							"type": "string", // What this phase accomplishes
						},
						"order_index": map[string]interface{}{
							"type": "integer", // Sequential ordering (1, 2, 3...)
						},
						"duration_weeks": map[string]interface{}{
							"type": "string", // e.g., "2-3 weeks"
						},
						// Nested milestones within this phase
						"milestones": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									// Milestone properties
									"sub_goal": map[string]interface{}{
										"type": "string", // e.g., "Environment Setup Complete"
									},
									"description": map[string]interface{}{
										"type": "string", // What this milestone accomplishes
									},
									"acceptance_test": map[string]interface{}{
										"type": "string", // How to verify completion
									},
									"duration_days_min": map[string]interface{}{
										"type": "integer", // Optimistic estimate
									},
									"duration_days_max": map[string]interface{}{
										"type": "integer", // Pessimistic estimate
									},
									"order_index": map[string]interface{}{
										"type": "integer", // Order within the phase
									},
									"day": map[string]interface{}{
										"type": "integer", // Day this milestone starts
									},
									"resources_forecast": map[string]interface{}{
										"type": "string", // JSON string of needed resources
									},
									// Spikes: time-boxed investigations for unknowns
									"spikes": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"description": map[string]interface{}{
													"type": "string", // What to investigate
												},
												"timebox_days": map[string]interface{}{
													"type": "integer", // Max time to spend
												},
											},
											"required":             []string{"description", "timebox_days"},
											"additionalProperties": false,
										},
									},
									// Risks: potential blockers or challenges
									"risks": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"description": map[string]interface{}{
													"type": "string", // What could go wrong
												},
												"severity": map[string]interface{}{
													"type": "string",
													"enum": []string{"low", "medium", "high"},
												},
											},
											"required":             []string{"description", "severity"},
											"additionalProperties": false,
										},
									},
								},
								// All milestone properties are required (OpenAI strict mode)
								"required":             []string{"sub_goal", "description", "acceptance_test", "duration_days_min", "duration_days_max", "order_index", "day", "resources_forecast", "spikes", "risks"},
								"additionalProperties": false,
							},
						},
					},
					// All phase properties are required
					"required":             []string{"name", "description", "order_index", "duration_weeks", "milestones"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"goal", "description", "days", "phases"},
		"additionalProperties": false,
	}

	systemPrompt := `You are a Goal Achievement Coach Agent focused on creating strategic roadmaps.

				CORE DIRECTIVES:
				- Be autonomous: Make informed assumptions rather than asking for clarification
				- Be thorough: Create comprehensive, actionable plans
				- Be realistic: Provide achievable timelines with buffer for learning curves
				- Be empowering: Frame guidance as enabling the user, not prescribing

				YOUR TASK - STRATEGIC PLANNING:
				Generate a high-level strategic roadmap with 2-4 major phases representing the journey from current state to goal achievement.

				For each phase, create 2-5 milestones that represent significant achievements. Each milestone must include:
				- sub_goal: Short, concise milestone name (no more than 5 words)
				- description: Informative but not overly descriptive
				- acceptance_test: Clear acceptance test (how to know it's complete)
				- duration_days_min/max: Duration estimates (min/max days, accounting for learning curves)
				- day: The day number when this milestone should start (sequential across the whole roadmap)
				- resources_forecast: Resources forecast (tools, materials, courses needed as JSON string)
				- spikes: Time-boxed investigations for unknowns (use empty array if none needed)
				- risks: Potential obstacles with severity level (use empty array if no significant risks)

				STRATEGIC PRINCIPLES:
				1. Start with fundamentals before advanced concepts
				2. Build milestones that validate progress before moving forward
				3. Include learning time - people need to absorb and practice
				4. Anticipate common obstacles and plan mitigations
				5. Balance ambition with achievability

				ASSUMPTIONS POLICY:
				If the goal is vague, assume the most common interpretation and proceed. Document your assumptions in the phase descriptions.

				Resources forecast should be a JSON string like: {"books": ["Book Title"], "tools": ["Tool Name"], "courses": ["Course Name"], "budget": "$X"}

				This is the STRATEGIC VIEW - keep it high-level. Do NOT include tasks or steps. Tasks will be generated separately for the first milestone.`

	// Build the OpenAI request with system prompt and user prompt
	reqBody := AIRequest{
		Model: "gpt-5-nano",
		Input: []Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
	}
	// Configure structured output with strict schema validation
	reqBody.Text.Format.Type = "json_schema"
	reqBody.Text.Format.Name = "strategic_roadmap"
	reqBody.Text.Format.Schema = schema
	reqBody.Text.Format.Strict = true

	// Make the HTTP request to OpenAI and return raw JSON response
	return makeOpenAIRequest(ctx, reqBody)
}

// generateMilestoneTasks creates detailed tasks for a specific milestone
// Takes a milestone as input and generates 10-14 detailed tasks
//
// INPUT: milestone - The milestone to generate tasks for
// OUTPUT: Raw JSON response with tasks and steps
func generateMilestoneTasks(ctx context.Context, userPrompt string, milestone *types.Milestone) ([]byte, error) {
	// Convert the milestone into a text summary to inject into the AI prompt
	milestoneContext := buildMilestoneContext(milestone)

	// Schema for Tasks - defines structure for detailed tasks
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tasks": map[string]interface{}{
				"type":     "array",
				"minItems": 10,
				"maxItems": 14,
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type": "string",
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"rationale": map[string]interface{}{
							"type": "string",
						},
						"est_minutes": map[string]interface{}{
							"type": "integer",
						},
						"dependencies": map[string]interface{}{
							"type": "string",
						},
						"confidence": map[string]interface{}{
							"type": "string",
							"enum": []string{"low", "medium", "high"},
						},
						"day": map[string]interface{}{
							"type": "integer",
						},
						"completed": map[string]interface{}{
							"type": "boolean",
						},
						"steps": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"description": map[string]interface{}{
										"type": "string",
									},
									"completed": map[string]interface{}{
										"type": "boolean",
									},
									"number": map[string]interface{}{
										"type": "integer",
									},
								},
								"required":             []string{"description", "completed", "number"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"title", "description", "rationale", "est_minutes", "dependencies", "confidence", "day", "completed", "steps"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"tasks"},
		"additionalProperties": false,
	}

	systemPrompt := fmt.Sprintf(`You are a Goal Achievement Coach Agent focused on creating detailed, actionable tactical plans.

									CORE DIRECTIVES:
									- Be thorough: Break down complex actions into clear, achievable steps
									- Be specific: Every task should have concrete actions, not vague suggestions
									- Be realistic: Account for learning time, practice, and potential setbacks
									- Be empowering: Include rationales that help users understand WHY each task matters

									MILESTONE CONTEXT:
									%s

									YOUR TASK - GENERATE TASKS:
									Generate 10-14 specific, actionable tasks for this milestone.

									EACH TASK MUST INCLUDE:
									- Title: Clear, action-oriented (no more than 5 words)
									- Description: What this task involves and how to approach it
									- Rationale: Why this task matters for the overall goal
									- Estimated minutes: Realistic time estimate (account for learning curve)
									- Dependencies: JSON array of prerequisite task titles (e.g., ["Task 1 Title"] or [] for none)
									- Confidence: Your confidence in the estimate (low/medium/high)
									- Day: Which day in the milestone this task should be done (spread across 7-14 days)
									- 2-4 detailed steps: Granular instructions that remove ambiguity

									TASK DESIGN PRINCIPLES:
									1. Start with foundational/easier tasks, progress to more complex
									2. Include both "learning" and "doing" tasks
									3. Build in practice and repetition for skill development
									4. Add checkpoint/validation tasks every 3-4 tasks
									5. Make dependencies realistic - don't over-constrain the sequence
									6. Spread tasks across days to allow for learning and practice time

									FORMATTING:
									- Set all 'completed' fields to false
									- Day numbers should be sequential starting from day 1
									- Dependencies should be exact task titles from earlier tasks in the list`, milestoneContext)

	reqBody := AIRequest{
		Model: "gpt-5-mini",
		Input: []Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
	}
	reqBody.Text.Format.Type = "json_schema"
	reqBody.Text.Format.Name = "milestone_tasks"
	reqBody.Text.Format.Schema = schema
	reqBody.Text.Format.Strict = true

	return makeOpenAIRequest(ctx, reqBody)
}

// buildMilestoneContext converts a Milestone into a human-readable text summary
// This summary gets injected into the task generation prompt to provide context
func buildMilestoneContext(milestone *types.Milestone) string {
	if milestone == nil {
		return "No milestone context available"
	}

	context := fmt.Sprintf("Milestone: %s\n", milestone.SubGoal)
	context += fmt.Sprintf("Description: %s\n", milestone.Description)

	if milestone.AcceptanceTest != "" {
		context += fmt.Sprintf("Acceptance Test: %s\n", milestone.AcceptanceTest)
	}

	if milestone.DurationDaysMin > 0 || milestone.DurationDaysMax > 0 {
		context += fmt.Sprintf("Duration: %d-%d days\n", milestone.DurationDaysMin, milestone.DurationDaysMax)
	}

	if milestone.ResourcesForecast != nil && *milestone.ResourcesForecast != "" {
		context += fmt.Sprintf("Resources: %s\n", *milestone.ResourcesForecast)
	}

	// Include spikes if any
	if len(milestone.Spikes) > 0 {
		context += "Spikes to investigate: "
		for _, spike := range milestone.Spikes {
			context += fmt.Sprintf("%s (%d days); ", spike.Description, spike.TimeboxDays)
		}
		context += "\n"
	}

	// Include risks if any
	if len(milestone.Risks) > 0 {
		context += "Risks to consider: "
		for _, risk := range milestone.Risks {
			context += fmt.Sprintf("%s [%s]; ", risk.Description, risk.Severity)
		}
		context += "\n"
	}

	return context
}

// parseStrategicRoadmapResponse extracts the Roadmap with Phases from OpenAI's response
func parseStrategicRoadmapResponse(rawResp []byte) (*types.Roadmap, error) {
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(rawResp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(apiResp.Output) == 0 {
		return nil, fmt.Errorf("no output found in OpenAI response")
	}

	var messageContent []types.OpenAIContent
	for _, output := range apiResp.Output {
		if output.Type == "message" && len(output.Content) > 0 {
			messageContent = output.Content
			break
		}
	}

	if len(messageContent) == 0 {
		return nil, fmt.Errorf("no content found in OpenAI output")
	}

	rawJSON := messageContent[0].Text

	var roadmap types.Roadmap
	if err := json.Unmarshal([]byte(rawJSON), &roadmap); err != nil {
		return nil, fmt.Errorf("failed to parse strategic roadmap JSON: %w", err)
	}

	return &roadmap, nil
}

// parseMilestoneTasksResponse extracts tasks from OpenAI's response
func parseMilestoneTasksResponse(rawResp []byte) ([]types.Task, error) {
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(rawResp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(apiResp.Output) == 0 {
		return nil, fmt.Errorf("no output found in OpenAI response")
	}

	var messageContent []types.OpenAIContent
	for _, output := range apiResp.Output {
		if output.Type == "message" && len(output.Content) > 0 {
			messageContent = output.Content
			break
		}
	}

	if len(messageContent) == 0 {
		return nil, fmt.Errorf("no content found in OpenAI output")
	}

	rawJSON := messageContent[0].Text

	var result struct {
		Tasks []types.Task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
		return nil, fmt.Errorf("failed to parse tasks JSON: %w", err)
	}

	return result.Tasks, nil
}

// makeOpenAIRequest is the shared HTTP client for all OpenAI API calls
// Handles authentication, timeouts, and error responses
//
// Used by both generateMacroMap and generateMilestoneMap
func makeOpenAIRequest(ctx context.Context, reqBody AIRequest) ([]byte, error) {
	// Serialize the request body to JSON
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP POST request to OpenAI
	req, err := http.NewRequestWithContext(ctx, "POST", openAIURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers (API key from environment variable)
	apiKey := os.Getenv("OPENAI_API_KEY")
	log.Info(ctx, "OpenAI API Key", fmt.Sprintf("Using API key: %s...%s", apiKey[:10], apiKey[len(apiKey)-5:]))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Execute request with 3-minute timeout (AI generation can take time)
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read the full response body
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response for debugging
	log.Info(ctx, fmt.Sprintf("OpenAI Response Status: %d", resp.StatusCode))

	// Check for HTTP errors (4xx, 5xx status codes)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("OpenAI error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	// Return raw response bytes for parsing by caller
	return respBytes, nil
}

// ParseRoadmap extracts the detailed milestone map (roadmap) from OpenAI's response
// Similar to parseMacroMapResponse but for the tactical/detailed view
//
// This parses the response from generateMilestoneMap into a Roadmap structure
func ParseRoadmap(raw []byte) (*types.Roadmap, error) {
	// Parse outer OpenAI response wrapper
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	// Validate content exists
	if len(apiResp.Output) == 0 {
		return nil, fmt.Errorf("no output found in OpenAI response")
	}

	// Find the message output (skip reasoning outputs)
	var messageContent []types.OpenAIContent
	for _, output := range apiResp.Output {
		if output.Type == "message" && len(output.Content) > 0 {
			messageContent = output.Content
			break
		}
	}

	if len(messageContent) == 0 {
		return nil, fmt.Errorf("no content found in OpenAI output")
	}

	// Extract the inner JSON string (the actual roadmap data)
	rawJSON := messageContent[0].Text

	// Parse into Roadmap structure
	var roadmap types.Roadmap
	if err := json.Unmarshal([]byte(rawJSON), &roadmap); err != nil {
		return nil, fmt.Errorf("failed to parse embedded goal roadmap JSON: %w", err)
	}

	return &roadmap, nil
}

// ============================================================================
// AI SESSION FUNCTIONS (NOT YET IMPLEMENTED)
// ============================================================================
// These functions are placeholders for a future interactive AI session feature
// where the AI asks clarifying questions before generating a roadmap.
//
// Current flow: User provides goal → Generate roadmap immediately
// Future flow: User provides goal → AI asks questions → Generate tailored roadmap
// ============================================================================

// ContinueAISession would handle multi-turn conversations with the AI
// NOT CURRENTLY USED - placeholder for future interactive sessions
func ContinueAISession(messages []Message) (reply string, isComplete bool, roadmap *types.Roadmap, err error) {
	// TODO: Implement interactive session logic
	// - If AI needs more info: return question, isComplete=false
	// - If AI has enough info: generate roadmap, isComplete=true
	return
}

// UpdateAISessionContext would append new messages to a session's context
// NOT CURRENTLY USED - placeholder for future feature
func UpdateAISessionContext(sessionID uuid.UUID, message []Message) {
	// TODO: Update session context in database
}

// UpdateAISession would modify an existing AI session
// NOT CURRENTLY USED - placeholder for future feature
func UpdateAISession(sessionID *AISession) {
	// TODO: Update session metadata in database
}

// SaveAISession would persist a session to the database
// NOT CURRENTLY USED - placeholder for future feature
func SaveAISession(sessionID *AISession) {
	// TODO: Save session to database
}

// LoadAISession would retrieve a session from the database
// NOT CURRENTLY USED - placeholder for future feature
func LoadAISession(sessionID string) (AISession, error) {
	var session AISession
	// TODO: Load session from database
	return session, nil
}
