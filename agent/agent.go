package agent

import (
	"bytes"
	"context"
	"dr/server/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

const openAIURL = "https://api.openai.com/v1/responses"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AISession struct {
	SessionID uuid.UUID      `json:"session_id"`
	UserID    string         `json:"user_id"`
	Status    string         `json:"status"`  // "collecting" or "complete"
	Context   []Message      `json:"context"` // chat history
	Roadmap   *types.Roadmap `json:"roadmap,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type AIRequest struct {
	Model string    `json:"model"`
	Input []Message `json:"input"`
	Text  struct {
		Format struct {
			Type   string                 `json:"type"`
			Name   string                 `json:"name"`
			Schema map[string]interface{} `json:"schema"`
			Strict bool                   `json:"strict"`
		} `json:"format"`
	} `json:"text"`
}

type GeneratedRoadmap struct {
	MacroMap *types.MacroMap `json:"macro_map"`
	Roadmap  *types.Roadmap  `json:"roadmap"`
}

func GenerateGoalFromStructure(goalReq types.GoalRequest) (*GeneratedRoadmap, error) {
	// Create a detailed prompt from the structured data
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

Please create a comprehensive roadmap with milestones, tasks, and actionable steps that takes into account the user's skill level, time commitment, and constraints.
`, goalReq.GoalName, goalReq.GoalDescription, goalReq.GoalImportance,
		goalReq.TargetCompletionDate, goalReq.WeeklyTimeCommitment,
		goalReq.WorkFrequency, goalReq.Constraints, goalReq.CurrentSkillLevel)

	return generateRoadmapFromPrompt(prompt)
}

func GenerateGoalBlueprint(userPrompt string) (*GeneratedRoadmap, error) {
	return generateRoadmapFromPrompt(userPrompt)
}

func generateRoadmapFromPrompt(userPrompt string) (*GeneratedRoadmap, error) {
	// STEP 1: Generate Macro Map (Strategic View)
	macroMapResp, err := generateMacroMap(userPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate macro map: %w", err)
	}

	// Parse the macro map response
	var macroMapData types.MacroMap
	if err := parseMacroMapResponse(macroMapResp, &macroMapData); err != nil {
		return nil, fmt.Errorf("failed to parse macro map: %w", err)
	}

	// STEP 2: Generate Milestone Map (Tactical View) based on the macro map
	milestoneMapResp, err := generateMilestoneMap(userPrompt, &macroMapData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate milestone map: %w", err)
	}

	// Parse the milestone map response
	roadmap, err := ParseRoadmap(milestoneMapResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse roadmap: %w", err)
	}

	// Return both the macro map and roadmap
	return &GeneratedRoadmap{
		MacroMap: &macroMapData,
		Roadmap:  roadmap,
	}, nil
}

func generateMacroMap(userPrompt string) ([]byte, error) {
	// Schema for Macro Map
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"phases": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type": "string",
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"order_index": map[string]interface{}{
							"type": "integer",
						},
						"duration_weeks": map[string]interface{}{
							"type": "string",
						},
						"macro_milestones": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"name": map[string]interface{}{
										"type": "string",
									},
									"acceptance_test": map[string]interface{}{
										"type": "string",
									},
									"duration_days_min": map[string]interface{}{
										"type": "integer",
									},
									"duration_days_max": map[string]interface{}{
										"type": "integer",
									},
									"order_index": map[string]interface{}{
										"type": "integer",
									},
									"resources_forecast": map[string]interface{}{
										"type": "string",
									},
									"spikes": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"description": map[string]interface{}{
													"type": "string",
												},
												"timebox_days": map[string]interface{}{
													"type": "integer",
												},
											},
											"required":             []string{"description", "timebox_days"},
											"additionalProperties": false,
										},
									},
									"risks": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"description": map[string]interface{}{
													"type": "string",
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
								"required":             []string{"name", "acceptance_test", "duration_days_min", "duration_days_max", "order_index"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"name", "description", "order_index", "duration_weeks", "macro_milestones"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"phases"},
		"additionalProperties": false,
	}

	reqBody := AIRequest{
		Model: "gpt-4o-2024-08-06",
		Input: []Message{
			{
				Role:    "system",
				Content: "You are an AI strategic planner that creates high-level roadmaps. Generate a macro map with 2-4 phases representing major stages of the goal. Each phase should have 2-5 macro milestones with acceptance tests, duration estimates (min/max days), and optional spikes (time-boxed investigations) and risks. This is a strategic overview - keep descriptions concise and focus on the big picture. Resources forecast should be a JSON string describing needed materials, tools, or prerequisites.",
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
	}
	reqBody.Text.Format.Type = "json_schema"
	reqBody.Text.Format.Name = "macro_map"
	reqBody.Text.Format.Schema = schema
	reqBody.Text.Format.Strict = true

	return makeOpenAIRequest(reqBody)
}

func generateMilestoneMap(userPrompt string, macroMap *types.MacroMap) ([]byte, error) {
	// Extract context from macro map to guide milestone generation
	macroContext := buildMacroContext(macroMap)

	// Schema for Milestone Map (existing roadmap structure)
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type": "string",
			},
			"goal": map[string]interface{}{
				"type": "string",
			},
			"description": map[string]interface{}{
				"type": "string",
			},
			"days": map[string]interface{}{
				"type": "integer",
			},
			"completed": map[string]interface{}{
				"type": "boolean",
			},
			"start_date": map[string]interface{}{
				"type":   "string",
				"format": "date-time",
			},
			"milestones": map[string]interface{}{
				"type":     "array",
				"minItems": 1,
				"maxItems": 1,
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"sub_goal": map[string]interface{}{
							"type": "string",
						},
						"description": map[string]interface{}{
							"type": "string",
						},
						"day": map[string]interface{}{
							"type": "integer",
						},
						"reached": map[string]interface{}{
							"type": "boolean",
						},
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
								"required":             []string{"title", "description", "day", "completed", "steps"},
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

	systemPrompt := fmt.Sprintf(`You are an AI tactical planner that creates detailed action plans based on a strategic roadmap.

STRATEGIC CONTEXT:
%s

Based on this strategic plan, generate EXACTLY 1 detailed milestone that represents the FIRST actionable phase (typically 1-2 weeks). This milestone must:
- Contain 10-14 specific, actionable tasks
- Each task should have: title, description, rationale (why it matters), estimated minutes, optional dependencies (JSON array of task titles), and confidence level (low/medium/high)
- Each task should have 2-4 detailed steps
- Add a 'message' field with a friendly intro
- Align with the first macro milestone in the strategic plan
- Set all 'completed' and 'reached' fields to false
- Ensure day numbers are sequential starting from day 1`, macroContext)

	reqBody := AIRequest{
		Model: "gpt-4o-2024-08-06",
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
	reqBody.Text.Format.Name = "dead_reckoner_roadmap"
	reqBody.Text.Format.Schema = schema
	reqBody.Text.Format.Strict = true

	return makeOpenAIRequest(reqBody)
}

func buildMacroContext(macroMap *types.MacroMap) string {
	if macroMap == nil || len(macroMap.Phases) == 0 {
		return "No strategic context available"
	}

	context := ""
	for _, phase := range macroMap.Phases {
		context += fmt.Sprintf("\nPhase %d: %s (%s)\n", phase.OrderIndex, phase.Name, phase.DurationWeeks)
		context += fmt.Sprintf("  Description: %s\n", phase.Description)

		for _, mm := range phase.MacroMilestones {
			context += fmt.Sprintf("  - Milestone: %s (Days: %d-%d)\n", mm.Name, mm.DurationDaysMin, mm.DurationDaysMax)
			context += fmt.Sprintf("    Acceptance: %s\n", mm.AcceptanceTest)

			if len(mm.Spikes) > 0 {
				context += "    Spikes: "
				for _, spike := range mm.Spikes {
					context += fmt.Sprintf("%s (%d days); ", spike.Description, spike.TimeboxDays)
				}
				context += "\n"
			}

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

func parseMacroMapResponse(rawResp []byte, macroMap *types.MacroMap) error {
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(rawResp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(apiResp.Output) == 0 || len(apiResp.Output[0].Content) == 0 {
		return fmt.Errorf("no content found in OpenAI output")
	}

	rawJSON := apiResp.Output[0].Content[0].Text
	if err := json.Unmarshal([]byte(rawJSON), macroMap); err != nil {
		return fmt.Errorf("failed to parse macro map JSON: %w", err)
	}

	return nil
}

func makeOpenAIRequest(reqBody AIRequest) ([]byte, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", openAIURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("OpenAI error: %s", string(respBytes))
	}

	return respBytes, nil
}

func ParseRoadmap(raw []byte) (*types.Roadmap, error) {
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(apiResp.Output) == 0 || len(apiResp.Output[0].Content) == 0 {
		return nil, fmt.Errorf("no content found in OpenAI output")
	}

	// Extract the JSON string from inside the OpenAI response
	rawJSON := apiResp.Output[0].Content[0].Text

	var roadmap types.Roadmap
	if err := json.Unmarshal([]byte(rawJSON), &roadmap); err != nil {
		return nil, fmt.Errorf("failed to parse embedded goal roadmap JSON: %w", err)
	}

	return &roadmap, nil
}

func ContinueAISession(messages []Message) (reply string, isComplete bool, roadmap *types.Roadmap, err error) {
	// Compose prompt as in your plan
	// Call OpenAI with full context
	// Parse response: if it's a question, return it; if it's a roadmap, return it and set isComplete=true
	// (You can use a special field in the AI response to distinguish)
	// For now, pseudo-code:
	// - If response contains "roadmap": isComplete = true, roadmap = parsed roadmap
	// - Else: isComplete = false, reply = next question
	return
}

func UpdateAISessionContext(sessionID uuid.UUID, message []Message) {

}

func UpdateAISession(sessionID *AISession) {

}

func SaveAISession(sessionID *AISession) {

}

func LoadAISession(sessionID string) (AISession, error) {

	var session AISession

	return session, nil
}
