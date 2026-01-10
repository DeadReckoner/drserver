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

// GeneratedRoadmap contains both outputs from the two-step generation process:
// 1. MacroMap: Strategic high-level view (phases, macro milestones, risks, spikes)
// 2. Roadmap: Tactical detailed view (specific tasks and steps for the first milestone)
type GeneratedRoadmap struct {
	MacroMap *types.MacroMap `json:"macro_map"` // Strategic planning layer
	Roadmap  *types.Roadmap  `json:"roadmap"`   // Tactical execution layer
}

// buildGoalPrompt converts structured goal request into natural language prompt
func buildGoalPrompt(goalReq types.GoalRequest) string {
	return fmt.Sprintf(`
Based on the following goal information, create a detailed roadmap:

Goal: %s
Description: %s
Importance: %s
Target Completion: %s
Weekly Time Commitment: %s
Work Frequency: %s
Constraints: %s
Current Skill Level: %s

Please create a comprehensive roadmap with milestones, tasks, and actionable steps that takes into account the user's skill level, time commitment, and constraints.
`, goalReq.GoalName, goalReq.GoalDescription, goalReq.GoalImportance,
		goalReq.TargetCompletionDate, goalReq.WeeklyTimeCommitment,
		goalReq.WorkFrequency, goalReq.Constraints, goalReq.CurrentSkillLevel)
}

// GenerateGoalFromStructure is the main entry point when the user provides structured goal data
// (via a form with fields like goal name, time commitment, skill level, etc.)
// It converts the structured data into a natural language prompt and calls the generation pipeline
func GenerateGoalFromStructure(ctx context.Context, goalReq types.GoalRequest) (*GeneratedRoadmap, error) {
	prompt := buildGoalPrompt(goalReq)
	return generateRoadmapFromPrompt(ctx, prompt)
}

// GenerateMacroMapFromGoalRequest generates only the macro map (strategic view) from a goal request
// This allows for progress tracking between macro and milestone generation
func GenerateMacroMapFromGoalRequest(ctx context.Context, goalReq types.GoalRequest) (*types.MacroMap, error) {
	log.Info(ctx, "Starting macro map generation", fmt.Sprintf("Goal: %s", goalReq.GoalName))
	prompt := buildGoalPrompt(goalReq)
	log.Info(ctx, "User Prompt", prompt)

	// Generate macro map
	log.Info(ctx, "Calling OpenAI API for macro map generation", "")
	macroMapResp, err := generateMacroMap(ctx, prompt)
	if err != nil {
		log.Info(ctx, "Macro map API call failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to generate macro map: %w", err)
	}
	log.Info(ctx, "Macro map API call successful", fmt.Sprintf("Response size: %d bytes", len(macroMapResp)))

	// Parse the raw JSON response
	var macroMapData types.MacroMap
	if err := parseMacroMapResponse(macroMapResp, &macroMapData); err != nil {
		log.Info(ctx, "Macro map parsing failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to parse macro map: %w", err)
	}

	log.Info(ctx, "Macro map parsed successfully", fmt.Sprintf("Phases: %d", len(macroMapData.Phases)))
	return &macroMapData, nil
}

// GenerateMilestoneMapFromGoalRequest generates the milestone map (tactical view) using an existing macro map
// This allows for progress tracking between macro and milestone generation
func GenerateMilestoneMapFromGoalRequest(ctx context.Context, goalReq types.GoalRequest, macroMap *types.MacroMap) (*types.Roadmap, error) {
	log.Info(ctx, "Starting milestone map generation", fmt.Sprintf("Goal: %s, Phases: %d", goalReq.GoalName, len(macroMap.Phases)))
	prompt := buildGoalPrompt(goalReq)

	// Generate milestone map using the macro map as context
	log.Info(ctx, "Calling OpenAI API for milestone map generation", "")
	milestoneMapResp, err := generateMilestoneMap(ctx, prompt, macroMap)
	if err != nil {
		log.Info(ctx, "Milestone map API call failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to generate milestone map: %w", err)
	}
	log.Info(ctx, "Milestone map API call successful", fmt.Sprintf("Response size: %d bytes", len(milestoneMapResp)))

	// Parse the milestone map response
	roadmap, err := ParseRoadmap(milestoneMapResp)
	if err != nil {
		log.Info(ctx, "Milestone map parsing failed", fmt.Sprintf("Error: %v", err))
		return nil, fmt.Errorf("failed to parse roadmap: %w", err)
	}

	log.Info(ctx, "Milestone map parsed successfully", fmt.Sprintf("Milestones: %d", len(roadmap.Milestones)))
	return roadmap, nil
}

// generateRoadmapFromPrompt is the core orchestrator for the two-step generation process
// It coordinates both strategic (macro) and tactical (milestone) planning
//
// FLOW:
// 1. Generate macro map (strategic view: phases, macro milestones, risks, spikes)
// 2. Parse the macro map response
// 3. Generate milestone map using macro map as context (tactical view: detailed tasks)
// 4. Parse the milestone map response
// 5. Return both structures
func generateRoadmapFromPrompt(ctx context.Context, userPrompt string) (*GeneratedRoadmap, error) {

	log.Info(ctx, "User Prompt", userPrompt)
	// STEP 1: Generate Macro Map (Strategic View)
	// This creates the high-level plan with 2-4 phases and macro milestones
	macroMapResp, err := generateMacroMap(ctx, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate macro map: %w", err)
	}

	// Parse the raw JSON response from OpenAI into our MacroMap structure
	var macroMapData types.MacroMap
	if err := parseMacroMapResponse(macroMapResp, &macroMapData); err != nil {
		return nil, fmt.Errorf("failed to parse macro map: %w", err)
	}

	// STEP 2: Generate Milestone Map (Tactical View) based on the macro map
	// This creates detailed tasks aligned with the strategic plan from step 1
	milestoneMapResp, err := generateMilestoneMap(ctx, userPrompt, &macroMapData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate milestone map: %w", err)
	}

	// Parse the milestone map response into our Roadmap structure
	roadmap, err := ParseRoadmap(milestoneMapResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse roadmap: %w", err)
	}

	// Return both the strategic and tactical views together
	return &GeneratedRoadmap{
		MacroMap: &macroMapData,
		Roadmap:  roadmap,
	}, nil
}

// generateMacroMap creates the strategic high-level plan
// Returns raw JSON response from OpenAI containing phases and macro milestones
//
// OUTPUT STRUCTURE:
// - 2-4 Phases (major stages like "Foundation", "Intermediate", "Advanced")
// - Each phase has 2-5 Macro Milestones with:
//   - Acceptance tests (how to know it's done)
//   - Duration estimates (min/max days)
//   - Resources forecast (tools, materials needed)
//   - Spikes (time-boxed investigations)
//   - Risks (potential blockers with severity)
func generateMacroMap(ctx context.Context, userPrompt string) ([]byte, error) {
	// Define the strict JSON schema that OpenAI must follow
	// NOTE: With strict=true, ALL properties MUST be in the required array
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
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
						// Nested macro milestones within this phase
						"macro_milestones": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									// Macro milestone properties
									"name": map[string]interface{}{
										"type": "string", // e.g., "Environment Setup Complete"
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
								// All macro milestone properties are required (OpenAI strict mode)
								"required":             []string{"name", "acceptance_test", "duration_days_min", "duration_days_max", "order_index", "resources_forecast", "spikes", "risks"},
								"additionalProperties": false,
							},
						},
					},
					// All phase properties are required
					"required":             []string{"name", "description", "order_index", "duration_weeks", "macro_milestones"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"phases"},
		"additionalProperties": false,
	}

	systemPrompt := `You are a Goal Achievement Coach Agent focused on creating strategic roadmaps.

				CORE DIRECTIVES:
				- Be autonomous: Make informed assumptions rather than asking for clarification
				- Be thorough: Create comprehensive, actionable plans
				- Be realistic: Provide achievable timelines with buffer for learning curves
				- Be empowering: Frame guidance as enabling the user, not prescribing

				YOUR TASK - STRATEGIC PLANNING (MACRO MAP):
				Generate a high-level strategic roadmap (macro map) with 2-4 major phases representing the journey from current state to goal achievement.

				For each phase, create 2-5 macro milestones that represent significant achievements. Each macro milestone must include:
				- The title roadmap, phases, and macro-milestones should be short and concise (no more than 5 words)
				- Description should be informative but not overly descriptive
				- Clear acceptance test (how to know it's complete)
				- Duration estimates (min/max days, accounting for learning curves)
				- Resources forecast (tools, materials, courses needed as JSON string)
				- Spikes: Time-boxed investigations for unknowns (use empty array if none needed)
				- Risks: Potential obstacles with severity level (use empty array if no significant risks)

				STRATEGIC PRINCIPLES:
				1. Start with fundamentals before advanced concepts
				2. Build milestones that validate progress before moving forward
				3. Include learning time - people need to absorb and practice
				4. Anticipate common obstacles and plan mitigations
				5. Balance ambition with achievability

				ASSUMPTIONS POLICY:
				If the goal is vague, assume the most common interpretation and proceed. Document your assumptions in the phase descriptions.

				Resources forecast should be a JSON string like: {"books": ["Book Title"], "tools": ["Tool Name"], "courses": ["Course Name"], "budget": "$X"}

				This is the STRATEGIC VIEW - keep it high-level and focused on major phases and milestones.`

	// Build the OpenAI request with system prompt and user prompt
	reqBody := AIRequest{
		Model: "gpt-5-nano", // Using GPT-4o for structured output
		Input: []Message{
			{
				Role: "system",
				// Goal Achievement Coach Agent - Strategic Planning Phase
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt, // The user's goal description
			},
		},
	}
	// Configure structured output with strict schema validation
	reqBody.Text.Format.Type = "json_schema"
	reqBody.Text.Format.Name = "macro_map"
	reqBody.Text.Format.Schema = schema
	reqBody.Text.Format.Strict = true // Enforce ALL properties must be provided

	// Make the HTTP request to OpenAI and return raw JSON response
	return makeOpenAIRequest(ctx, reqBody)
}

// generateMilestoneMap creates the tactical detailed plan aligned with the strategic macro map
// Takes the macro map as input and generates EXACTLY 1 milestone with 10-14 detailed tasks
//
// INPUT: macroMap - The strategic plan from step 1
// OUTPUT: Raw JSON response with detailed tasks, steps, dependencies, confidence levels
//
// KEY DIFFERENCE FROM MACRO MAP:
// - Macro map = "What phases and major milestones?" (weeks/months view)
// - Milestone map = "What specific tasks this week?" (days/tasks view)
func generateMilestoneMap(ctx context.Context, userPrompt string, macroMap *types.MacroMap) ([]byte, error) {
	// Convert the macro map into a text summary to inject into the AI prompt
	// This ensures the detailed tasks align with the strategic plan
	macroContext := buildMacroContext(macroMap)

	// Schema for Milestone Map - defines structure for detailed tasks
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			// Top-level roadmap properties
			"message": map[string]interface{}{
				"type": "string", // Friendly intro message (e.g., "Here's your first milestone!")
			},
			"goal": map[string]interface{}{
				"type": "string", // The overall goal title
			},
			"description": map[string]interface{}{
				"type": "string", // Description of the roadmap
			},
			"days": map[string]interface{}{
				"type": "integer", // Total estimated days
			},
			"completed": map[string]interface{}{
				"type": "boolean", // Always false for new roadmaps
			},
			"start_date": map[string]interface{}{
				"type":   "string",
				"format": "date-time", // ISO 8601 format
			},
			// Array of milestones (we enforce exactly 1 milestone for the first phase)
			"milestones": map[string]interface{}{
				"type":     "array",
				"minItems": 1, // Must have at least 1 milestone
				"maxItems": 1, // Generate ONLY the first milestone
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						// Milestone-level properties
						"sub_goal": map[string]interface{}{
							"type": "string", // Title of this milestone (e.g., "Setup Development Environment")
						},
						"description": map[string]interface{}{
							"type": "string", // What this milestone accomplishes
						},
						"day": map[string]interface{}{
							"type": "integer", // Which day this milestone starts on
						},
						"reached": map[string]interface{}{
							"type": "boolean", // Always false for new milestones
						},
						// Array of tasks (10-14 specific actionable items)
						"tasks": map[string]interface{}{
							"type":     "array",
							"minItems": 10, // At least 10 tasks
							"maxItems": 14, // At most 14 tasks
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									// Task-level properties (these are the detailed actionable items)
									"title": map[string]interface{}{
										"type": "string", // Task name (e.g., "Install Node.js and npm")
									},
									"description": map[string]interface{}{
										"type": "string", // What this task involves
									},
									"rationale": map[string]interface{}{
										"type": "string", // Why this task matters (NEW field from macro map changes)
									},
									"est_minutes": map[string]interface{}{
										"type": "integer", // Estimated time to complete (NEW field)
									},
									"dependencies": map[string]interface{}{
										"type": "string", // JSON array of task titles this depends on (NEW field)
									},
									"confidence": map[string]interface{}{
										"type": "string", // How confident the AI is in this estimate (NEW field)
										"enum": []string{"low", "medium", "high"},
									},
									"day": map[string]interface{}{
										"type": "integer", // Which day within the milestone
									},
									"completed": map[string]interface{}{
										"type": "boolean", // Always false for new tasks
									},
									// Steps: granular sub-actions within each task (2-4 steps per task)
									"steps": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"description": map[string]interface{}{
													"type": "string", // Specific instruction
												},
												"completed": map[string]interface{}{
													"type": "boolean", // Track completion
												},
												"number": map[string]interface{}{
													"type": "integer", // Step number (1, 2, 3...)
												},
											},
											"required":             []string{"description", "completed", "number"},
											"additionalProperties": false,
										},
									},
								},
								// All task properties must be provided (strict mode)
								"required":             []string{"title", "description", "rationale", "est_minutes", "dependencies", "confidence", "day", "completed", "steps"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"sub_goal", "description", "day", "reached", "tasks"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"message", "goal", "description", "days", "completed", "start_date", "milestones"},
		"additionalProperties": false,
	}

	// Build a system prompt that includes the strategic context from the macro map
	// This is the KEY connection between step 1 (macro) and step 2 (milestone)
	systemPrompt := fmt.Sprintf(`You are a Goal Achievement Coach Agent focused on creating detailed, actionable tactical plans.

									CORE DIRECTIVES:
									- Be thorough: Break down complex actions into clear, achievable steps
									- Be specific: Every task should have concrete actions, not vague suggestions
									- Be realistic: Account for learning time, practice, and potential setbacks
									- Be empowering: Include rationales that help users understand WHY each task matters

									STRATEGIC CONTEXT (from macro map):
									%s

									YOUR TASK - TACTICAL PLANNING (MILESTONE MAP):
									Generate EXACTLY 1 detailed milestone representing the FIRST actionable phase (typically 1-2 weeks) that aligns with the first macro milestone above.

									MILESTONE REQUIREMENTS:
									- The title of the milestones and tasks should be short and concise (no more than 5 words)
									- Create 10-14 specific, actionable tasks that build toward the milestone goal
									- Tasks should progress logically - each building on previous work
									- Mix learning tasks with practice/application tasks
									- Include validation/checkpoint tasks to measure progress

									EACH TASK MUST INCLUDE:
									- Title: Clear, action-oriented (e.g., "Practice C Major Scale with Metronome")
									- Description: What this task involves and how to approach it
									- Rationale: Why this task matters for the overall goal (builds motivation and understanding)
									- Estimated minutes: Realistic time estimate (account for learning curve)
									- Dependencies: JSON array of prerequisite task titles (e.g., ["Task 1 Title", "Task 2 Title"] or [] for none)
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

									MESSAGE FIELD:
									Add a friendly, encouraging message that introduces the milestone and sets expectations (e.g., "Here's your first milestone! This will build your foundation...")

									FORMATTING:
									- Set all 'completed' and 'reached' fields to false (user hasn't started yet)
									- Day numbers should be sequential starting from day 1
									- Dependencies should be exact task titles from earlier tasks in the list`, macroContext)

	reqBody := AIRequest{
		Model: "gpt-5-mini",
		Input: []Message{
			{
				Role:    "system",
				Content: systemPrompt, // Includes macro map context
			},
			{
				Role:    "user",
				Content: userPrompt, // Original user goal
			},
		},
	}
	// Configure structured output for milestone map
	reqBody.Text.Format.Type = "json_schema"
	reqBody.Text.Format.Name = "dead_reckoner_roadmap"
	reqBody.Text.Format.Schema = schema
	reqBody.Text.Format.Strict = true

	return makeOpenAIRequest(ctx, reqBody)
}

// buildMacroContext converts the MacroMap structure into a human-readable text summary
// This summary gets injected into the milestone generation prompt to provide strategic context
//
// WHY THIS MATTERS:
// Without this, the AI would generate tasks blindly. With this context, the AI can align
// detailed tasks with the high-level strategy (e.g., if macro map says "setup environment",
// detailed tasks will include installing specific tools)
func buildMacroContext(macroMap *types.MacroMap) string {
	if macroMap == nil || len(macroMap.Phases) == 0 {
		return "No strategic context available"
	}

	context := ""
	// Iterate through each phase and format it as text
	for _, phase := range macroMap.Phases {
		context += fmt.Sprintf("\nPhase %d: %s (%s)\n", phase.OrderIndex, phase.Name, phase.DurationWeeks)
		context += fmt.Sprintf("  Description: %s\n", phase.Description)

		// Include all macro milestones within this phase
		for _, mm := range phase.MacroMilestones {
			context += fmt.Sprintf("  - Milestone: %s (Days: %d-%d)\n", mm.Name, mm.DurationDaysMin, mm.DurationDaysMax)
			context += fmt.Sprintf("    Acceptance: %s\n", mm.AcceptanceTest)

			// Include spikes if any (investigations needed)
			if len(mm.Spikes) > 0 {
				context += "    Spikes: "
				for _, spike := range mm.Spikes {
					context += fmt.Sprintf("%s (%d days); ", spike.Description, spike.TimeboxDays)
				}
				context += "\n"
			}

			// Include risks if any (potential blockers)
			if len(mm.Risks) > 0 {
				context += "    Risks: "
				for _, risk := range mm.Risks {
					context += fmt.Sprintf("%s [%s]; ", risk.Description, risk.Severity)
				}
				context += "\n"
			}
		}
	}

	return context
}

// parseMacroMapResponse extracts the MacroMap from OpenAI's response wrapper
//
// OpenAI responses have this structure:
//
//	{
//	  "output": [{
//	    "content": [{
//	      "text": "{\"phases\": [...]}"  <-- Our actual macro map JSON is HERE
//	    }]
//	  }]
//	}
//
// This function unwraps the response and parses the inner JSON into our MacroMap type
func parseMacroMapResponse(rawResp []byte, macroMap *types.MacroMap) error {
	// First, parse the outer OpenAI response wrapper
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(rawResp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	// Validate that we got content back
	if len(apiResp.Output) == 0 {
		return fmt.Errorf("no output found in OpenAI response")
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
		return fmt.Errorf("no content found in OpenAI output")
	}

	// Extract the inner JSON string (the actual macro map data)
	rawJSON := messageContent[0].Text

	// Parse the macro map JSON into our Go struct
	if err := json.Unmarshal([]byte(rawJSON), macroMap); err != nil {
		return fmt.Errorf("failed to parse macro map JSON: %w", err)
	}

	return nil
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
	defer resp.Body.Close()

	// Read the full response body
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response for debugging
	log.Info(ctx, fmt.Sprintf("OpenAI Response Status: %d", resp.StatusCode))

	// Check for HTTP errors (4xx, 5xx status codes)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("OpenAI error (status %d): %s", resp.StatusCode)
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
