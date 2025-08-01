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
	// Schema for Dead Reckoner goal blueprint (with "message" added)
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type": "string",
			},
			"title":       map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"level": map[string]interface{}{
				"type": "string",
				"enum": []string{"YEAR", "QUARTER", "MONTH", "WEEK", "DAY"},
			},
			"start_time": map[string]interface{}{"type": "string", "format": "date"},
			"end_time":   map[string]interface{}{"type": "string", "format": "date"},
			"completed":  map[string]interface{}{"type": "boolean"},
			"subtasks": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"$ref": "#"},
			},
		},
		"required":             []string{"message", "title", "description", "level", "start_time", "end_time", "completed", "subtasks"},
		"additionalProperties": false,
	}

	// Build the full request
	reqBody := AIRequest{
		Model: "gpt-4o-2024-08-06",
		Input: []Message{
			{
				Role:    "system",
				Content: "You are an AI assistant that helps users build structured achievement pathways. You must only respond with a JSON object that follows the provided schema. Add a 'message' field with a short friendly intro like 'Here's what I made for you!'. Only include the necessary levels (YEAR, QUARTER, MONTH, WEEK, DAY) based on the goal’s scope. However, the roadmap should be as detailed as possible at the lowest level. The daily tasks should have detailed actionable steps. You should ask between 3-5 clarifying questions before generating the schema, things like the users experience with the task, or what their current proficency to make this roadmap as personalized as possible.",
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
