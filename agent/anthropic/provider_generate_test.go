package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dr/server/types"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Fake Anthropic server infrastructure ─────────────────────────────────────
//
// The SDK client's base URL is injectable via option.WithBaseURL, so these
// tests run fully hermetic against an httptest server that plays canned
// /v1/messages responses in sequence and records every request body for
// behavioral assertions on prompt/tool_choice construction.

// agCannedResponse is one scripted reply from the fake Anthropic server.
type agCannedResponse struct {
	status int
	body   string
}

// agFakeAnthropic is an httptest-backed fake for the Anthropic messages API.
type agFakeAnthropic struct {
	srv       *httptest.Server
	mu        sync.Mutex
	requests  [][]byte
	responses []agCannedResponse
}

// agNewFakeAnthropic starts a fake server that returns the given responses in
// order (repeating the last one if calls exceed the script). Closed via t.Cleanup.
func agNewFakeAnthropic(t *testing.T, responses ...agCannedResponse) *agFakeAnthropic {
	t.Helper()
	f := &agFakeAnthropic{responses: responses}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		f.requests = append(f.requests, body)
		idx := len(f.requests) - 1
		if idx >= len(f.responses) {
			idx = len(f.responses) - 1
		}
		resp := f.responses[idx]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// agRequest returns the i-th captured request body as a raw string.
func (f *agFakeAnthropic) agRequest(t *testing.T, i int) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Greater(t, len(f.requests), i, "expected at least %d captured requests", i+1)
	return string(f.requests[i])
}

// agParsedRequest is the subset of the messages API request body the tests
// make structural assertions on.
type agParsedRequest struct {
	Model  string `json:"model"`
	System []struct {
		Text string `json:"text"`
	} `json:"system"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	ToolChoice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tool_choice"`
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

// agParseRequest decodes the i-th captured request body.
func (f *agFakeAnthropic) agParseRequest(t *testing.T, i int) agParsedRequest {
	t.Helper()
	var parsed agParsedRequest
	require.NoError(t, json.Unmarshal([]byte(f.agRequest(t, i)), &parsed))
	return parsed
}

// agNewTestProvider builds a Provider whose SDK client points at the fake
// server. MaxRetries(0) keeps error-path tests from sleeping through the
// SDK's retry backoff.
func agNewTestProvider(f *agFakeAnthropic) *Provider {
	return &Provider{client: anthropic.NewClient(
		option.WithBaseURL(f.srv.URL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(0),
	)}
}

// agToolUseBody builds a canned messages-API response whose content is a
// single tool_use block for the named tool.
func agToolUseBody(toolName, inputJSON string) string {
	return fmt.Sprintf(`{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-6",
		"content": [{"type": "tool_use", "id": "tu_1", "name": %q, "input": %s}],
		"stop_reason": "tool_use",
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`, toolName, inputJSON)
}

// agTextBody builds a canned messages-API response with a single text block.
func agTextBody(text, stopReason string) string {
	return fmt.Sprintf(`{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-6",
		"content": [{"type": "text", "text": %q}],
		"stop_reason": %q,
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`, text, stopReason)
}

// agErrorBody is a canned Anthropic API error payload.
const agErrorBody = `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`

// agGoalRequest returns a fully-populated GoalRequest for prompt assertions.
func agGoalRequest() types.GoalRequest {
	return types.GoalRequest{
		GoalName:             "Learn Spanish",
		GoalDescription:      "Reach conversational fluency",
		GoalImportance:       "Family connection",
		TargetCompletionDate: "2027-01-01",
		WeeklyTimeCommitment: "5 hours",
		WorkFrequency:        "daily",
		Constraints:          "Evenings only",
		CurrentSkillLevel:    "Beginner",
	}
}

// agMilestoneFixture returns a milestone with strategic fields populated.
func agMilestoneFixture() *types.Milestone {
	rf := `{"apps":["Anki"]}`
	return &types.Milestone{
		MilestoneID:       "mi_1",
		SubGoal:           "Master present tense",
		Description:       "Core verb conjugation",
		AcceptanceTest:    "Hold a 5 minute conversation",
		DurationDaysMin:   7,
		DurationDaysMax:   14,
		ResourcesForecast: &rf,
		Spikes:            []types.Spike{{Description: "Compare textbooks", TimeboxDays: 1}},
		Risks:             []types.Risk{{Description: "Motivation dip", Severity: "medium"}},
	}
}

// agStrategicInput is a valid generate_strategic_roadmap tool input.
const agStrategicInput = `{
	"goal": "Learn Spanish",
	"description": "A structured plan",
	"days": 90,
	"phases": [
		{
			"name": "Foundation",
			"description": "Basics",
			"order_index": 1,
			"duration_weeks": "4",
			"milestones": [
				{"sub_goal": "Alphabet and sounds", "day": 1, "duration_days_min": 3, "duration_days_max": 5},
				{"sub_goal": "Basic phrases", "day": 6}
			]
		},
		{
			"name": "Conversation",
			"order_index": 2,
			"milestones": [{"sub_goal": "First dialogue", "day": 30}]
		}
	]
}`

// agTasksInput is a valid generate_milestone_tasks tool input whose first
// task carries dependencies in Claude's array form (exercising normalization).
const agTasksInput = `{
	"tasks": [
		{
			"title": "Install Anki",
			"description": "Set up flashcards",
			"rationale": "Spaced repetition works",
			"est_minutes": 20,
			"dependencies": ["Pick a textbook"],
			"confidence": "high",
			"day": 1,
			"completed": false,
			"steps": [{"description": "Download app", "completed": false, "number": 1}]
		},
		{
			"title": "Learn 20 verbs",
			"description": "Common verbs first",
			"rationale": "Coverage",
			"est_minutes": 45,
			"dependencies": [],
			"confidence": "medium",
			"day": 2,
			"completed": false,
			"steps": []
		}
	]
}`

// ─── GenerateStrategicRoadmap ─────────────────────────────────────────────────

func TestProviderGenerateStrategicRoadmapHappyPath(t *testing.T) {
	fake := agNewFakeAnthropic(t, agCannedResponse{200, agToolUseBody("generate_strategic_roadmap", agStrategicInput)})
	p := agNewTestProvider(fake)

	roadmap, err := p.GenerateStrategicRoadmap(context.Background(), agGoalRequest())
	require.NoError(t, err)

	assert.Equal(t, "Learn Spanish", roadmap.Goal)
	assert.Equal(t, 90, roadmap.Days)
	require.Len(t, roadmap.Phases, 2)
	assert.Equal(t, "Foundation", roadmap.Phases[0].Name)
	require.Len(t, roadmap.Phases[0].Milestones, 2)
	assert.Equal(t, "Alphabet and sounds", roadmap.Phases[0].Milestones[0].SubGoal)
	require.Len(t, roadmap.Phases[1].Milestones, 1)

	// Behavioral assertions on the constructed request.
	req := fake.agParseRequest(t, 0)
	assert.Equal(t, string(anthropic.ModelClaudeSonnet4_6), req.Model)
	assert.Equal(t, "tool", req.ToolChoice.Type)
	assert.Equal(t, "generate_strategic_roadmap", req.ToolChoice.Name)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "generate_strategic_roadmap", req.Tools[0].Name)
	require.NotEmpty(t, req.System)
	assert.Contains(t, req.System[0].Text, "STRATEGIC PLANNING")
	// The user message carries the goal prompt with the goal fields.
	raw := fake.agRequest(t, 0)
	assert.Contains(t, raw, "Goal: Learn Spanish")
	assert.Contains(t, raw, "Current Skill Level: Beginner")
}

func TestProviderGenerateStrategicRoadmapErrors(t *testing.T) {
	t.Run("no tool use in response", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{200, agTextBody("Here is your roadmap in prose.", "end_turn")})
		p := agNewTestProvider(fake)

		_, err := p.GenerateStrategicRoadmap(context.Background(), agGoalRequest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no tool use found in response")
	})

	t.Run("malformed tool input fails roadmap parse", func(t *testing.T) {
		// days must be an integer; a string breaks unmarshaling into types.Roadmap.
		fake := agNewFakeAnthropic(t, agCannedResponse{200, agToolUseBody("generate_strategic_roadmap", `{"goal":"G","days":"x"}`)})
		p := agNewTestProvider(fake)

		_, err := p.GenerateStrategicRoadmap(context.Background(), agGoalRequest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse roadmap from tool output")
	})

	t.Run("HTTP 400 wraps anthropic API error", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{400, agErrorBody})
		p := agNewTestProvider(fake)

		_, err := p.GenerateStrategicRoadmap(context.Background(), agGoalRequest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "anthropic API error")
	})
}

// ─── GenerateMilestoneTasks ───────────────────────────────────────────────────

func TestProviderGenerateMilestoneTasksHappyPath(t *testing.T) {
	fake := agNewFakeAnthropic(t, agCannedResponse{200, agToolUseBody("generate_milestone_tasks", agTasksInput)})
	p := agNewTestProvider(fake)

	tasks, err := p.GenerateMilestoneTasks(context.Background(), agGoalRequest(), agMilestoneFixture())
	require.NoError(t, err)

	require.Len(t, tasks, 2)
	assert.Equal(t, "Install Anki", tasks[0].Title)
	assert.Equal(t, 20, tasks[0].EstMinutes)
	require.Len(t, tasks[0].Steps, 1)
	// Dependencies arrive as a JSON array and must be normalized into the
	// *string JSON-array-literal shape end-to-end (parseMilestoneTasks path).
	require.NotNil(t, tasks[0].Dependencies)
	assert.Equal(t, `["Pick a textbook"]`, *tasks[0].Dependencies)
	require.NotNil(t, tasks[1].Dependencies)
	assert.Equal(t, `[]`, *tasks[1].Dependencies)

	// System prompt embeds the milestone context.
	req := fake.agParseRequest(t, 0)
	require.NotEmpty(t, req.System)
	assert.Contains(t, req.System[0].Text, "Milestone: Master present tense")
	assert.Contains(t, req.System[0].Text, "Acceptance Test: Hold a 5 minute conversation")
	assert.Equal(t, "generate_milestone_tasks", req.ToolChoice.Name)
	// No review yet, so the review section must be absent.
	assert.NotContains(t, req.System[0].Text, "USER REVIEW OF PRIOR MILESTONE")
}

func TestProviderGenerateMilestoneTasksErrors(t *testing.T) {
	t.Run("no tool use in response", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{200, agTextBody("Sounds good!", "end_turn")})
		p := agNewTestProvider(fake)

		_, err := p.GenerateMilestoneTasks(context.Background(), agGoalRequest(), agMilestoneFixture())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no tool use found in response")
	})

	t.Run("malformed tasks input", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{200, agToolUseBody("generate_milestone_tasks", `{"tasks":[{"title":123}]}`)})
		p := agNewTestProvider(fake)

		_, err := p.GenerateMilestoneTasks(context.Background(), agGoalRequest(), agMilestoneFixture())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse tasks from tool output")
	})
}

// ─── GenerateNextMilestoneFromReview ──────────────────────────────────────────

func TestProviderGenerateNextMilestoneFromReviewAdjusted(t *testing.T) {
	adjInput := `{
		"adjusted": true,
		"sub_goal": "Gentler present tense",
		"duration_days_min": 10,
		"duration_days_max": 21,
		"rationale": "User asked for slower pacing"
	}`
	fake := agNewFakeAnthropic(t,
		agCannedResponse{200, agToolUseBody("generate_milestone_adjustments", adjInput)},
		agCannedResponse{200, agToolUseBody("generate_milestone_tasks", agTasksInput)},
	)
	p := agNewTestProvider(fake)

	review := types.MilestoneReviewAnswers{PaceFeedback: "Too fast, slow it down"}
	adjustments, tasks, err := p.GenerateNextMilestoneFromReview(context.Background(), agGoalRequest(), agMilestoneFixture(), review)
	require.NoError(t, err)

	require.NotNil(t, adjustments)
	assert.True(t, adjustments.Adjusted)
	assert.Equal(t, "Gentler present tense", adjustments.SubGoal)
	assert.Equal(t, 10, adjustments.DurationDaysMin)
	require.Len(t, tasks, 2)
	assert.Equal(t, "Install Anki", tasks[0].Title)

	// Call 1: adjustments call carries the ORIGINAL milestone context and the review.
	req1 := fake.agParseRequest(t, 0)
	assert.Equal(t, "generate_milestone_adjustments", req1.ToolChoice.Name)
	assert.Contains(t, req1.System[0].Text, "Milestone: Master present tense")
	assert.Contains(t, req1.System[0].Text, "Too fast, slow it down")

	// Call 2: the tasks call must see the ADJUSTED sub-goal and duration,
	// proving applyAdjustments ran between the two calls.
	req2 := fake.agParseRequest(t, 1)
	assert.Equal(t, "generate_milestone_tasks", req2.ToolChoice.Name)
	assert.Contains(t, req2.System[0].Text, "Milestone: Gentler present tense")
	assert.Contains(t, req2.System[0].Text, "Duration: 10-21 days")
	assert.NotContains(t, req2.System[0].Text, "Milestone: Master present tense")
	// Review context is injected into the tasks prompt too.
	assert.Contains(t, req2.System[0].Text, "USER REVIEW OF PRIOR MILESTONE")
	assert.Contains(t, req2.System[0].Text, "Too fast, slow it down")
}

func TestProviderGenerateNextMilestoneFromReviewNotAdjusted(t *testing.T) {
	fake := agNewFakeAnthropic(t,
		agCannedResponse{200, agToolUseBody("generate_milestone_adjustments", `{"adjusted": false, "rationale": ""}`)},
		agCannedResponse{200, agToolUseBody("generate_milestone_tasks", agTasksInput)},
	)
	p := agNewTestProvider(fake)

	adjustments, tasks, err := p.GenerateNextMilestoneFromReview(context.Background(), agGoalRequest(), agMilestoneFixture(), types.MilestoneReviewAnswers{})
	require.NoError(t, err)

	assert.False(t, adjustments.Adjusted)
	require.Len(t, tasks, 2)

	// With adjusted=false the tasks call keeps the original milestone context.
	req2 := fake.agParseRequest(t, 1)
	assert.Contains(t, req2.System[0].Text, "Milestone: Master present tense")
	assert.Contains(t, req2.System[0].Text, "Duration: 7-14 days")
	// Empty review answers surface as the no-feedback sentinel.
	assert.Contains(t, req2.System[0].Text, "(User did not provide review feedback.)")
}

func TestProviderGenerateNextMilestoneFromReviewErrors(t *testing.T) {
	review := types.MilestoneReviewAnswers{PaceFeedback: "fine"}

	t.Run("adjustments HTTP error", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{400, agErrorBody})
		p := agNewTestProvider(fake)

		_, _, err := p.GenerateNextMilestoneFromReview(context.Background(), agGoalRequest(), agMilestoneFixture(), review)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to generate adjustments")
		assert.Contains(t, err.Error(), "anthropic API error")
	})

	t.Run("adjustments response without tool use", func(t *testing.T) {
		fake := agNewFakeAnthropic(t, agCannedResponse{200, agTextBody("No changes needed.", "end_turn")})
		p := agNewTestProvider(fake)

		_, _, err := p.GenerateNextMilestoneFromReview(context.Background(), agGoalRequest(), agMilestoneFixture(), review)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to generate adjustments")
		assert.Contains(t, err.Error(), "no tool use found in response")
	})

	t.Run("tasks response without tool use", func(t *testing.T) {
		fake := agNewFakeAnthropic(t,
			agCannedResponse{200, agToolUseBody("generate_milestone_adjustments", `{"adjusted": false, "rationale": ""}`)},
			agCannedResponse{200, agTextBody("Here are some ideas.", "end_turn")},
		)
		p := agNewTestProvider(fake)

		_, _, err := p.GenerateNextMilestoneFromReview(context.Background(), agGoalRequest(), agMilestoneFixture(), review)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no tool use found in response")
	})
}

// ─── applyAdjustments ─────────────────────────────────────────────────────────

func TestApplyAdjustmentsSentinelsAndOverrides(t *testing.T) {
	t.Run("all sentinels leave milestone untouched", func(t *testing.T) {
		m := agMilestoneFixture()
		original := *m
		applyAdjustments(m, &types.MilestoneAdjustments{
			SubGoal: "", Description: "", AcceptanceTest: "",
			DurationDaysMin: 0, DurationDaysMax: 0,
			ResourcesForecast: "", Spikes: []types.Spike{}, Risks: []types.Risk{},
		})

		assert.Equal(t, original.SubGoal, m.SubGoal)
		assert.Equal(t, original.Description, m.Description)
		assert.Equal(t, original.AcceptanceTest, m.AcceptanceTest)
		assert.Equal(t, original.DurationDaysMin, m.DurationDaysMin)
		assert.Equal(t, original.DurationDaysMax, m.DurationDaysMax)
		assert.Same(t, original.ResourcesForecast, m.ResourcesForecast)
		assert.Equal(t, original.Spikes, m.Spikes)
		assert.Equal(t, original.Risks, m.Risks)
	})

	t.Run("non-sentinel values overwrite fields", func(t *testing.T) {
		m := agMilestoneFixture()
		adj := &types.MilestoneAdjustments{
			SubGoal:           "New goal",
			Description:       "New description",
			AcceptanceTest:    "New test",
			DurationDaysMin:   3,
			DurationDaysMax:   6,
			ResourcesForecast: `{"books":["B"]}`,
			Spikes:            []types.Spike{{Description: "New spike", TimeboxDays: 2}},
			Risks:             []types.Risk{{Description: "New risk", Severity: "low"}},
		}
		applyAdjustments(m, adj)

		assert.Equal(t, "New goal", m.SubGoal)
		assert.Equal(t, "New description", m.Description)
		assert.Equal(t, "New test", m.AcceptanceTest)
		assert.Equal(t, 3, m.DurationDaysMin)
		assert.Equal(t, 6, m.DurationDaysMax)
		require.NotNil(t, m.ResourcesForecast)
		assert.Equal(t, `{"books":["B"]}`, *m.ResourcesForecast)
		// Non-empty spikes/risks fully replace the existing slices.
		require.Len(t, m.Spikes, 1)
		assert.Equal(t, "New spike", m.Spikes[0].Description)
		require.Len(t, m.Risks, 1)
		assert.Equal(t, "New risk", m.Risks[0].Description)
	})

	t.Run("resources forecast sets a fresh pointer", func(t *testing.T) {
		m := agMilestoneFixture()
		before := m.ResourcesForecast
		applyAdjustments(m, &types.MilestoneAdjustments{ResourcesForecast: "updated"})
		require.NotNil(t, m.ResourcesForecast)
		assert.NotSame(t, before, m.ResourcesForecast)
		assert.Equal(t, "updated", *m.ResourcesForecast)
		// Original pointer target is untouched.
		assert.Equal(t, `{"apps":["Anki"]}`, *before)
	})
}

// ─── Prompt-builder twins (anthropic copies) ──────────────────────────────────

func TestAnthropicBuildGoalPrompt(t *testing.T) {
	t.Run("all fields present, no trailing instruction", func(t *testing.T) {
		prompt := buildGoalPrompt(agGoalRequest())
		for _, want := range []string{
			"Goal: Learn Spanish",
			"Description: Reach conversational fluency",
			"Importance: Family connection",
			"Target Completion: 2027-01-01",
			"Weekly Time Commitment: 5 hours",
			"Work Frequency: daily",
			"Constraints: Evenings only",
			"Current Skill Level: Beginner",
		} {
			assert.Contains(t, prompt, want)
		}
		// Unlike the OpenAI twin, the anthropic copy has no trailing
		// "comprehensive roadmap" instruction (the tool_choice forces output).
		assert.NotContains(t, prompt, "Please create a comprehensive roadmap")
	})

	t.Run("additional context conditional", func(t *testing.T) {
		req := agGoalRequest()
		assert.NotContains(t, buildGoalPrompt(req), "Additional Context:")
		req.AdditionalContext = "Has a tutor"
		assert.Contains(t, buildGoalPrompt(req), "Additional Context: Has a tutor")
	})
}

func TestAnthropicBuildMilestoneContext(t *testing.T) {
	t.Run("nil milestone", func(t *testing.T) {
		assert.Equal(t, "No milestone context available", buildMilestoneContext(nil))
	})

	t.Run("minimal milestone omits optional sections", func(t *testing.T) {
		ctx := buildMilestoneContext(&types.Milestone{SubGoal: "S", Description: "D"})
		assert.Contains(t, ctx, "Milestone: S\n")
		assert.NotContains(t, ctx, "Acceptance Test:")
		assert.NotContains(t, ctx, "Duration:")
		assert.NotContains(t, ctx, "Resources:")
		assert.NotContains(t, ctx, "Spikes to investigate:")
		assert.NotContains(t, ctx, "Risks to consider:")
	})

	t.Run("full milestone includes every section", func(t *testing.T) {
		ctx := buildMilestoneContext(agMilestoneFixture())
		assert.Contains(t, ctx, "Milestone: Master present tense\n")
		assert.Contains(t, ctx, "Acceptance Test: Hold a 5 minute conversation\n")
		assert.Contains(t, ctx, "Duration: 7-14 days\n")
		assert.Contains(t, ctx, `Resources: {"apps":["Anki"]}`+"\n")
		assert.Contains(t, ctx, "Spikes to investigate: Compare textbooks (1 days); \n")
		assert.Contains(t, ctx, "Risks to consider: Motivation dip [medium]; \n")
	})

	t.Run("empty resources pointer skipped", func(t *testing.T) {
		empty := ""
		ctx := buildMilestoneContext(&types.Milestone{SubGoal: "S", ResourcesForecast: &empty})
		assert.NotContains(t, ctx, "Resources:")
	})
}

func TestAnthropicBuildReviewContext(t *testing.T) {
	t.Run("empty review returns sentinel", func(t *testing.T) {
		assert.Equal(t, "(User did not provide review feedback.)",
			buildReviewContext(types.MilestoneReviewAnswers{}))
	})

	t.Run("all fields emit labeled lines", func(t *testing.T) {
		got := buildReviewContext(types.MilestoneReviewAnswers{
			CompletionStatus: "a", WhatWorked: "b", WhatDidntWork: "c",
			Obstacles: "d", PaceFeedback: "e", ClarityFeedback: "f", GoingForward: "g",
		})
		for _, want := range []string{
			"- Completion status: a\n",
			"- What worked well: b\n",
			"- What didn't work: c\n",
			"- Obstacles: d\n",
			"- Workload/pace feedback: e\n",
			"- Instruction clarity: f\n",
			"- Preferences for next milestone / life changes: g\n",
		} {
			assert.Contains(t, got, want)
		}
		assert.Equal(t, 7, strings.Count(got, "- "))
	})

	t.Run("partial review emits only answered lines", func(t *testing.T) {
		got := buildReviewContext(types.MilestoneReviewAnswers{WhatWorked: "flashcards"})
		assert.Equal(t, "- What worked well: flashcards\n", got)
	})
}

// ─── NewProvider ──────────────────────────────────────────────────────────────

func TestNewProviderNameSmoke(t *testing.T) {
	// NewProvider reads ANTHROPIC_API_KEY via os.Getenv; unset/empty is fine
	// for construction — no request is made here.
	p := NewProvider()
	require.NotNil(t, p)
	assert.Equal(t, "anthropic", p.Name())
}
