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
    Status    string         `json:"status"` // "collecting" or "complete"
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

func GenerateGoalBlueprint(userPrompt string) ([]byte, error) {
	// Schema for Dead Reckoner roadmap matching the Roadmap type
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
				"type": "string",
				"format": "date-time",
			},
			"milestones": map[string]interface{}{
				"type": "array",
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
							"type": "array",
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
											"required": []string{"description", "completed", "number"},
											"additionalProperties": false,
										},
									},
								},
								"required": []string{"title", "description", "day", "completed", "steps"},
								"additionalProperties": false,
							},
						},
					},
					"required": []string{"sub_goal", "description", "day", "reached", "tasks"},
					"additionalProperties": false,
				},
			},
		},
		"required": []string{"message", "goal", "description", "days", "completed", "start_date", "milestones"},
		"additionalProperties": false,
	}

	// Build the full request
	reqBody := AIRequest{
		Model: "gpt-4o-2024-08-06",
		Input: []Message{
			{
				Role:    "system",
				Content: "You are an AI assistant that helps users build structured achievement pathways in incremental steps. You must only respond with a JSON object that follows the provided schema. Generate EXACTLY 1 milestone that represents the first phase of their goal (typically 1-2 weeks). This milestone must contain between 10-14 tasks that are actionable and specific. Each task should have 2-4 detailed steps. Add a 'message' field with a short friendly intro like 'Here's your first milestone!'. Focus on creating a solid foundation that can be built upon in subsequent phases. Set all 'completed' and 'reached' fields to false by default. Ensure day numbers are sequential starting from day 1.",
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

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// HTTP request
	req, err := http.NewRequestWithContext(context.Background(), "POST", openAIURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
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


func UpdateAISessionContext(sessionID uuid.UUID, message []Message){

}


func UpdateAISession(sessionID *AISession){

}


func SaveAISession(sessionID *AISession){

}

func LoadAISession(sessionID string)(AISession, error){

	var session AISession


	return session, nil
}