package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AgentMode controls which persona/toolset the agent uses
type AgentMode string

const (
	ModeRoadmapGeneration AgentMode = "roadmap_generation"
	ModePerformanceCoach  AgentMode = "performance_coach"
	ModeRoadmapEditor     AgentMode = "roadmap_editor"
)

// AgentRequest is the input for a single agent turn
type AgentRequest struct {
	SessionID   string         `json:"session_id"`
	UserMessage string         `json:"user_message"`
	Mode        AgentMode      `json:"mode"`
	Context     map[string]any `json:"context,omitempty"` // goal data, milestone data, habit stats, etc.
}

// AgentResponse is the output of a single agent turn
type AgentResponse struct {
	SessionID      string `json:"session_id"`
	Message        string `json:"message"`                   // Human-readable text to show the user
	StructuredData any    `json:"structured_data,omitempty"` // Non-nil when agent triggered a tool
	ToolUsed       string `json:"tool_used,omitempty"`
	IsComplete     bool   `json:"is_complete"` // true when agent has finished its task
}

// Service is the main Anthropic agent service
type Service struct {
	client       anthropic.Client
	sessionStore *SessionStore
}

// NewService creates a new Anthropic agent service
func NewService(apiKey string) *Service {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Service{
		client:       client,
		sessionStore: NewSessionStore(),
	}
}

// Run executes one turn of the agent conversation
func (s *Service) Run(ctx context.Context, req AgentRequest) (*AgentResponse, error) {
	// Get or create session
	session, err := s.sessionStore.GetOrCreate(req.SessionID, req.Mode)
	if err != nil {
		return nil, fmt.Errorf("session error: %w", err)
	}

	// Build system prompt for the current mode
	systemPrompt := buildSystemPrompt(req.Mode, req.Context)

	// Append the new user message
	session.AddUserMessage(req.UserMessage)

	// Select tools for this mode
	tools := getToolsForMode(req.Mode)

	// Convert tools to union params
	toolUnions := make([]anthropic.ToolUnionParam, len(tools))
	for i, tool := range tools {
		toolUnions[i] = anthropic.ToolUnionParam{OfTool: &tool}
	}

	// Call Anthropic
	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: session.Messages(),
		Tools:    toolUnions,
	}

	msg, err := s.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	// Process the response
	resp, err := s.processResponse(ctx, session, msg, req)
	if err != nil {
		return nil, err
	}

	// Persist updated session
	s.sessionStore.Save(session)

	return resp, nil
}

// DeleteSession removes a session from the store
func (s *Service) DeleteSession(sessionID string) {
	s.sessionStore.Delete(sessionID)
}

// processResponse handles the model output, including tool use loops
func (s *Service) processResponse(
	ctx context.Context,
	session *Session,
	msg *anthropic.Message,
	req AgentRequest,
) (*AgentResponse, error) {
	resp := &AgentResponse{
		SessionID: req.SessionID,
	}

	// Collect text from the response
	var textContent string
	for _, block := range msg.Content {
		if block.Type == "text" {
			textContent += block.Text
		}
	}

	// Check for tool use
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}

		toolName := block.Name
		toolInput := block.Input

		// Parse and execute the tool
		structured, err := executeTool(toolName, toolInput)
		if err != nil {
			return nil, fmt.Errorf("tool execution error (%s): %w", toolName, err)
		}

		// Add assistant message + tool result to session history
		session.AddAssistantMessageRaw(msg.Content)
		session.AddToolResult(block.ID, structured)

		resp.ToolUsed = toolName
		resp.StructuredData = structured
		resp.IsComplete = true // tool use signals task completion
	}

	// If no tool was used, it's a conversational turn
	if resp.ToolUsed == "" {
		session.AddAssistantText(textContent)
		resp.IsComplete = msg.StopReason == "end_turn" && !sessionNeedsMoreInfo(textContent)
	}

	resp.Message = textContent
	return resp, nil
}

// executeTool dispatches tool calls to the appropriate handler
func executeTool(name string, input json.RawMessage) (any, error) {
	switch name {
	case ToolCreateRoadmap:
		var result RoadmapOutput
		if err := json.Unmarshal(input, &result); err != nil {
			return nil, err
		}
		return result, nil

	case ToolEditRoadmap:
		var result RoadmapEditOutput
		if err := json.Unmarshal(input, &result); err != nil {
			return nil, err
		}
		return result, nil

	case ToolPerformanceReport:
		var result PerformanceReportOutput
		if err := json.Unmarshal(input, &result); err != nil {
			return nil, err
		}
		return result, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// sessionNeedsMoreInfo checks if the agent is asking a clarifying question
func sessionNeedsMoreInfo(text string) bool {
	// Heuristic: if the response ends with "?" it's asking for more info
	if len(text) == 0 {
		return false
	}
	trimmed := text
	for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == ' ' || trimmed[len(trimmed)-1] == '\n') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return len(trimmed) > 0 && trimmed[len(trimmed)-1] == '?'
}
