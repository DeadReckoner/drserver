package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agNewTestService builds a Service whose SDK client points at the fake
// Anthropic server (helpers shared from provider_generate_test.go).
func agNewTestService(f *agFakeAnthropic) *Service {
	return &Service{
		client: anthropic.NewClient(
			option.WithBaseURL(f.srv.URL),
			option.WithAPIKey("test-key"),
			option.WithMaxRetries(0),
		),
		sessionStore: NewSessionStore(),
	}
}

// agBareSession returns a Session constructed directly (no store, no ticker).
func agBareSession(mode AgentMode) *Session {
	return &Session{ID: "sess-test", Mode: mode}
}

// agValidRoadmapInput is a minimal valid create_roadmap tool input.
const agValidRoadmapInput = `{
	"goal_title": "Learn Guitar",
	"goal_summary": "Chords first, then songs",
	"total_weeks": 12,
	"macro_map": {
		"phases": [
			{"phase_number": 1, "title": "Fundamentals", "duration_weeks": 4,
			 "objectives": ["Hold chords"], "milestones": ["Open chords clean"]}
		]
	},
	"milestone_map": {
		"current_milestone": {
			"title": "First Sprint",
			"description": "Basics",
			"duration_days": 14,
			"tasks": [{"title": "Buy picks", "description": "Get gear", "priority": "high"}],
			"habits": [{"title": "Practice", "description": "Daily reps", "frequency_mask": 127, "duration_mins": 20}]
		}
	}
}`

// ─── executeTool ──────────────────────────────────────────────────────────────

func TestExecuteToolDispatch(t *testing.T) {
	t.Run("create_roadmap decodes RoadmapOutput", func(t *testing.T) {
		out, err := executeTool(ToolCreateRoadmap, json.RawMessage(agValidRoadmapInput))
		require.NoError(t, err)

		result, ok := out.(RoadmapOutput)
		require.True(t, ok, "expected RoadmapOutput, got %T", out)
		assert.Equal(t, "Learn Guitar", result.GoalTitle)
		assert.Equal(t, 12, result.TotalWeeks)
		require.Len(t, result.MacroMap.Phases, 1)
		assert.Equal(t, "Fundamentals", result.MacroMap.Phases[0].Title)
		assert.Equal(t, "First Sprint", result.MilestoneMap.CurrentMilestone.Title)
		require.Len(t, result.MilestoneMap.CurrentMilestone.Habits, 1)
		assert.Equal(t, 127, result.MilestoneMap.CurrentMilestone.Habits[0].FrequencyMask)
		assert.Nil(t, result.MilestoneMap.NextMilestone)
	})

	t.Run("edit_roadmap decodes RoadmapEditOutput", func(t *testing.T) {
		input := `{
			"edit_summary": "Reduced habit frequency",
			"changes": [
				{"change_type": "modify_habit", "target": "First Sprint", "description": "Practice 3x weekly"}
			]
		}`
		out, err := executeTool(ToolEditRoadmap, json.RawMessage(input))
		require.NoError(t, err)

		result, ok := out.(RoadmapEditOutput)
		require.True(t, ok, "expected RoadmapEditOutput, got %T", out)
		assert.Equal(t, "Reduced habit frequency", result.EditSummary)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, "modify_habit", result.Changes[0].ChangeType)
		assert.Equal(t, "First Sprint", result.Changes[0].Target)
	})

	t.Run("performance_report decodes PerformanceReportOutput", func(t *testing.T) {
		input := `{
			"overall_score": 82,
			"summary": "Solid week",
			"highlights": ["Streak intact"],
			"milestone_status": "on_track",
			"habit_insights": [
				{"habit_title": "Practice", "completion_rate": 0.85, "trend": "improving", "note": "Keep it up"}
			],
			"recommendations": ["Add ear training"]
		}`
		out, err := executeTool(ToolPerformanceReport, json.RawMessage(input))
		require.NoError(t, err)

		result, ok := out.(PerformanceReportOutput)
		require.True(t, ok, "expected PerformanceReportOutput, got %T", out)
		assert.Equal(t, 82, result.OverallScore)
		assert.Equal(t, "on_track", result.MilestoneStatus)
		require.Len(t, result.HabitInsights, 1)
		assert.InDelta(t, 0.85, result.HabitInsights[0].CompletionRate, 1e-9)
		assert.Equal(t, "improving", result.HabitInsights[0].Trend)
	})

	t.Run("malformed input errors per tool", func(t *testing.T) {
		malformed := []struct {
			tool  string
			input string
		}{
			{ToolCreateRoadmap, `{"goal_title": 123}`},
			{ToolEditRoadmap, `{"edit_summary": []}`},
			{ToolPerformanceReport, `{"overall_score": "high"}`},
		}
		for _, tc := range malformed {
			t.Run(tc.tool, func(t *testing.T) {
				_, err := executeTool(tc.tool, json.RawMessage(tc.input))
				assert.Error(t, err)
			})
		}
	})

	t.Run("unknown tool name", func(t *testing.T) {
		_, err := executeTool("summon_dragon", json.RawMessage(`{}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown tool: summon_dragon")
	})
}

// ─── sessionNeedsMoreInfo ─────────────────────────────────────────────────────

func TestSessionNeedsMoreInfo(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"empty string", "", false},
		{"clarifying question", "What is your goal?", true},
		{"question with trailing spaces and newlines", "How many hours per week?  \n\n", true},
		{"declarative sentence", "Your roadmap is ready.", false},
		{"only whitespace", "   \n \n", false},
		{"bare question mark", "?", true},
		{"question mid-text not at end", "Any questions? Let me know.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sessionNeedsMoreInfo(tc.text))
		})
	}
}

// ─── processResponse (direct, no HTTP) ────────────────────────────────────────

func TestProcessResponseConversationalTurns(t *testing.T) {
	svc := &Service{sessionStore: NewSessionStore()}
	req := AgentRequest{SessionID: "sess-test", Mode: ModeRoadmapGeneration}

	t.Run("text with end_turn and no question is complete", func(t *testing.T) {
		session := agBareSession(ModeRoadmapGeneration)
		msg := &anthropic.Message{
			Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "Your plan is set."}},
			StopReason: anthropic.StopReasonEndTurn,
		}

		resp, err := svc.processResponse(context.Background(), session, msg, req)
		require.NoError(t, err)
		assert.True(t, resp.IsComplete)
		assert.Equal(t, "Your plan is set.", resp.Message)
		assert.Empty(t, resp.ToolUsed)
		assert.Nil(t, resp.StructuredData)
		// The assistant turn was recorded.
		assert.Equal(t, 1, session.MessageCount())
	})

	t.Run("text ending in question mark is not complete", func(t *testing.T) {
		session := agBareSession(ModeRoadmapGeneration)
		msg := &anthropic.Message{
			Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "How much time do you have?"}},
			StopReason: anthropic.StopReasonEndTurn,
		}

		resp, err := svc.processResponse(context.Background(), session, msg, req)
		require.NoError(t, err)
		assert.False(t, resp.IsComplete)
		assert.Equal(t, "How much time do you have?", resp.Message)
	})

	t.Run("max_tokens stop reason is not complete", func(t *testing.T) {
		session := agBareSession(ModeRoadmapGeneration)
		msg := &anthropic.Message{
			Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "Truncated output"}},
			StopReason: anthropic.StopReasonMaxTokens,
		}

		resp, err := svc.processResponse(context.Background(), session, msg, req)
		require.NoError(t, err)
		assert.False(t, resp.IsComplete)
	})
}

func TestProcessResponseToolUse(t *testing.T) {
	svc := &Service{sessionStore: NewSessionStore()}
	req := AgentRequest{SessionID: "sess-test", Mode: ModeRoadmapGeneration}

	t.Run("tool_use sets structured data and appends two history messages", func(t *testing.T) {
		session := agBareSession(ModeRoadmapGeneration)
		msg := &anthropic.Message{
			Content: []anthropic.ContentBlockUnion{
				{Type: "tool_use", ID: "tu_1", Name: ToolCreateRoadmap, Input: json.RawMessage(agValidRoadmapInput)},
			},
			StopReason: anthropic.StopReasonToolUse,
		}

		resp, err := svc.processResponse(context.Background(), session, msg, req)
		require.NoError(t, err)
		assert.True(t, resp.IsComplete)
		assert.Equal(t, ToolCreateRoadmap, resp.ToolUsed)
		result, ok := resp.StructuredData.(RoadmapOutput)
		require.True(t, ok)
		assert.Equal(t, "Learn Guitar", result.GoalTitle)
		// Assistant raw message + tool_result message.
		assert.Equal(t, 2, session.MessageCount())

		history := session.Messages()
		assert.Equal(t, anthropic.MessageParamRoleAssistant, history[0].Role)
		assert.Equal(t, anthropic.MessageParamRoleUser, history[1].Role)
		require.NotEmpty(t, history[1].Content)
		require.NotNil(t, history[1].Content[0].OfToolResult)
		assert.Equal(t, "tu_1", history[1].Content[0].OfToolResult.ToolUseID)
	})

	t.Run("tool_use with bad input returns tool execution error", func(t *testing.T) {
		session := agBareSession(ModeRoadmapGeneration)
		msg := &anthropic.Message{
			Content: []anthropic.ContentBlockUnion{
				{Type: "tool_use", ID: "tu_1", Name: ToolCreateRoadmap, Input: json.RawMessage(`{"goal_title": 5}`)},
			},
			StopReason: anthropic.StopReasonToolUse,
		}

		_, err := svc.processResponse(context.Background(), session, msg, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool execution error")
		// Nothing was appended before the failure.
		assert.Equal(t, 0, session.MessageCount())
	})

	t.Run("mixed text and tool_use concatenates text into message", func(t *testing.T) {
		session := agBareSession(ModeRoadmapGeneration)
		msg := &anthropic.Message{
			Content: []anthropic.ContentBlockUnion{
				{Type: "text", Text: "Here is your roadmap. "},
				{Type: "text", Text: "It spans 12 weeks."},
				{Type: "tool_use", ID: "tu_2", Name: ToolCreateRoadmap, Input: json.RawMessage(agValidRoadmapInput)},
			},
			StopReason: anthropic.StopReasonToolUse,
		}

		resp, err := svc.processResponse(context.Background(), session, msg, req)
		require.NoError(t, err)
		assert.Equal(t, "Here is your roadmap. It spans 12 weeks.", resp.Message)
		assert.Equal(t, ToolCreateRoadmap, resp.ToolUsed)
		assert.True(t, resp.IsComplete)
	})
}

// ─── Service.Run (httptest fake) ──────────────────────────────────────────────

func TestServiceRunConversationalFlow(t *testing.T) {
	fake := agNewFakeAnthropic(t,
		agCannedResponse{200, agTextBody("What's your current skill level?", "end_turn")},
		agCannedResponse{200, agTextBody("Great, I'll plan for a beginner.", "end_turn")},
	)
	svc := agNewTestService(fake)

	// Turn 1: agent asks a clarifying question.
	resp1, err := svc.Run(context.Background(), AgentRequest{
		SessionID:   "conv-1",
		UserMessage: "I want to learn piano",
		Mode:        ModeRoadmapGeneration,
	})
	require.NoError(t, err)
	assert.Equal(t, "conv-1", resp1.SessionID)
	assert.Equal(t, "What's your current skill level?", resp1.Message)
	assert.False(t, resp1.IsComplete, "clarifying question should not complete the turn")

	// Mode-appropriate system prompt: persona + roadmap-generation section.
	req1 := fake.agParseRequest(t, 0)
	require.NotEmpty(t, req1.System)
	assert.Contains(t, req1.System[0].Text, "Dead Reckoner")
	assert.Contains(t, req1.System[0].Text, "create_roadmap")
	require.Len(t, req1.Tools, 1)
	assert.Equal(t, ToolCreateRoadmap, req1.Tools[0].Name)
	require.Len(t, req1.Messages, 1)
	assert.Equal(t, "user", req1.Messages[0].Role)

	// Turn 2: history persists across Run calls in the same session.
	resp2, err := svc.Run(context.Background(), AgentRequest{
		SessionID:   "conv-1",
		UserMessage: "Total beginner",
		Mode:        ModeRoadmapGeneration,
	})
	require.NoError(t, err)
	assert.True(t, resp2.IsComplete)

	req2 := fake.agParseRequest(t, 1)
	require.Len(t, req2.Messages, 3, "user + assistant + user history expected")
	raw2 := fake.agRequest(t, 1)
	assert.Contains(t, raw2, "I want to learn piano")
	assert.Contains(t, raw2, "What's your current skill level?")
	assert.Contains(t, raw2, "Total beginner")

	// Session now holds all four messages.
	session, err := svc.sessionStore.GetOrCreate("conv-1", ModeRoadmapGeneration)
	require.NoError(t, err)
	assert.Equal(t, 4, session.MessageCount())
}

func TestServiceRunToolUseTurn(t *testing.T) {
	fake := agNewFakeAnthropic(t,
		agCannedResponse{200, agToolUseBody(ToolCreateRoadmap, agValidRoadmapInput)},
	)
	svc := agNewTestService(fake)

	resp, err := svc.Run(context.Background(), AgentRequest{
		SessionID:   "tool-1",
		UserMessage: "Make the roadmap now",
		Mode:        ModeRoadmapGeneration,
	})
	require.NoError(t, err)
	assert.True(t, resp.IsComplete)
	assert.Equal(t, ToolCreateRoadmap, resp.ToolUsed)
	result, ok := resp.StructuredData.(RoadmapOutput)
	require.True(t, ok)
	assert.Equal(t, "Learn Guitar", result.GoalTitle)

	// user + assistant-raw + tool_result
	session, err := svc.sessionStore.GetOrCreate("tool-1", ModeRoadmapGeneration)
	require.NoError(t, err)
	assert.Equal(t, 3, session.MessageCount())
}

func TestServiceRunErrors(t *testing.T) {
	t.Run("empty session id", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{200, agTextBody("hi", "end_turn")})
		svc := agNewTestService(fake)

		_, err := svc.Run(context.Background(), AgentRequest{
			SessionID:   "",
			UserMessage: "hello",
			Mode:        ModeRoadmapGeneration,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session error")
		// No request should have reached the API.
		fake.mu.Lock()
		defer fake.mu.Unlock()
		assert.Empty(t, fake.requests)
	})

	t.Run("HTTP 400 surfaces anthropic API error, user message still recorded", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{400, agErrorBody})
		svc := agNewTestService(fake)

		_, err := svc.Run(context.Background(), AgentRequest{
			SessionID:   "err-1",
			UserMessage: "hello",
			Mode:        ModeRoadmapGeneration,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "anthropic API error")

		// Documents current behavior: the user message is appended to the
		// session BEFORE the API call, so a failed turn leaves it in history
		// (it will be re-sent alongside the retry's message).
		session, gerr := svc.sessionStore.GetOrCreate("err-1", ModeRoadmapGeneration)
		require.NoError(t, gerr)
		assert.Equal(t, 1, session.MessageCount())
	})
}

// ─── DeleteSession / NewService ───────────────────────────────────────────────

func TestServiceDeleteSession(t *testing.T) {
	fake := agNewFakeAnthropic(t, agCannedResponse{200, agTextBody("Noted.", "end_turn")})
	svc := agNewTestService(fake)

	_, err := svc.Run(context.Background(), AgentRequest{
		SessionID:   "del-1",
		UserMessage: "remember this",
		Mode:        ModeRoadmapGeneration,
	})
	require.NoError(t, err)

	session, err := svc.sessionStore.GetOrCreate("del-1", ModeRoadmapGeneration)
	require.NoError(t, err)
	require.Equal(t, 2, session.MessageCount())

	svc.DeleteSession("del-1")

	fresh, err := svc.sessionStore.GetOrCreate("del-1", ModeRoadmapGeneration)
	require.NoError(t, err)
	assert.Equal(t, 0, fresh.MessageCount(), "session should start empty after delete")
	assert.NotSame(t, session, fresh)
}

func TestNewServiceSmoke(t *testing.T) {
	svc := NewService("test-key")
	require.NotNil(t, svc)
	require.NotNil(t, svc.sessionStore)
	// The store is usable immediately.
	s, err := svc.sessionStore.GetOrCreate("smoke-1", ModePerformanceCoach)
	require.NoError(t, err)
	assert.Equal(t, ModePerformanceCoach, s.Mode)
}
