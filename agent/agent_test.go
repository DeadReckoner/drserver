package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"dr/server/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Fixtures ─────────────────────────────────────────────────────────────────

// agStrPtr returns a pointer to s (helper for *string fields).
func agStrPtr(s string) *string { return &s }

// agFullGoalRequest returns a GoalRequest with every field populated with a
// distinct marker value so prompt tests can assert each field is included.
func agFullGoalRequest() types.GoalRequest {
	return types.GoalRequest{
		GoalName:             "Run a marathon",
		GoalDescription:      "Complete a full 26.2 mile race",
		GoalImportance:       "Lifelong dream",
		TargetCompletionDate: "2026-12-31",
		WeeklyTimeCommitment: "6 hours",
		WorkFrequency:        "4 days per week",
		Constraints:          "Knee injury history",
		CurrentSkillLevel:    "Casual 5k runner",
	}
}

// agOpenAIEnvelope wraps an inner JSON payload in the OpenAI responses-API
// envelope shape the parsers expect: a reasoning output (which must be
// skipped) followed by a message output carrying the inner text.
func agOpenAIEnvelope(t *testing.T, inner string) []byte {
	t.Helper()
	resp := types.OpenAIResponse{
		ID: "resp_1",
		Output: []types.OpenAIMessage{
			{ID: "rs_1", Type: "reasoning"},
			{
				ID:   "msg_1",
				Type: "message",
				Content: []types.OpenAIContent{
					{Type: "output_text", Text: inner},
				},
			},
		},
	}
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	return raw
}

// agRawEnvelope marshals an arbitrary OpenAIResponse for envelope edge cases.
func agRawEnvelope(t *testing.T, resp types.OpenAIResponse) []byte {
	t.Helper()
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	return raw
}

// ─── buildGoalPrompt ──────────────────────────────────────────────────────────

func TestBuildGoalPromptIncludesAllFields(t *testing.T) {
	req := agFullGoalRequest()
	prompt := buildGoalPrompt(req)

	for _, want := range []string{
		"Goal: Run a marathon",
		"Description: Complete a full 26.2 mile race",
		"Importance: Lifelong dream",
		"Target Completion: 2026-12-31",
		"Weekly Time Commitment: 6 hours",
		"Work Frequency: 4 days per week",
		"Constraints: Knee injury history",
		"Current Skill Level: Casual 5k runner",
	} {
		assert.Contains(t, prompt, want)
	}

	// The closing roadmap instruction is always appended.
	assert.Contains(t, prompt, "Please create a comprehensive roadmap")
}

func TestBuildGoalPromptAdditionalContext(t *testing.T) {
	t.Run("omitted when empty", func(t *testing.T) {
		prompt := buildGoalPrompt(agFullGoalRequest())
		assert.NotContains(t, prompt, "Additional Context:")
	})

	t.Run("included when set", func(t *testing.T) {
		req := agFullGoalRequest()
		req.AdditionalContext = "Training partner available"
		prompt := buildGoalPrompt(req)
		assert.Contains(t, prompt, "Additional Context: Training partner available")
		// Instruction still trails the context line.
		assert.Contains(t, prompt, "Please create a comprehensive roadmap")
	})
}

// ─── buildMilestoneContext ────────────────────────────────────────────────────

func TestBuildMilestoneContextNilMilestone(t *testing.T) {
	assert.Equal(t, "No milestone context available", buildMilestoneContext(nil))
}

func TestBuildMilestoneContextOptionalSections(t *testing.T) {
	t.Run("minimal milestone omits optional sections", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "Base fitness", Description: "Build aerobic base"}
		ctx := buildMilestoneContext(m)

		assert.Contains(t, ctx, "Milestone: Base fitness\n")
		assert.Contains(t, ctx, "Description: Build aerobic base\n")
		assert.NotContains(t, ctx, "Acceptance Test:")
		assert.NotContains(t, ctx, "Duration:")
		assert.NotContains(t, ctx, "Resources:")
		assert.NotContains(t, ctx, "Spikes to investigate:")
		assert.NotContains(t, ctx, "Risks to consider:")
	})

	t.Run("duration shown when only min set", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "M", DurationDaysMin: 3}
		assert.Contains(t, buildMilestoneContext(m), "Duration: 3-0 days\n")
	})

	t.Run("duration shown when only max set", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "M", DurationDaysMax: 7}
		assert.Contains(t, buildMilestoneContext(m), "Duration: 0-7 days\n")
	})

	t.Run("resources skipped when pointer nil", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "M", ResourcesForecast: nil}
		assert.NotContains(t, buildMilestoneContext(m), "Resources:")
	})

	t.Run("resources skipped when pointer to empty string", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "M", ResourcesForecast: agStrPtr("")}
		assert.NotContains(t, buildMilestoneContext(m), "Resources:")
	})

	t.Run("resources included when non-empty", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "M", ResourcesForecast: agStrPtr(`{"tools":["shoes"]}`)}
		assert.Contains(t, buildMilestoneContext(m), `Resources: {"tools":["shoes"]}`+"\n")
	})

	t.Run("acceptance test included when set", func(t *testing.T) {
		m := &types.Milestone{SubGoal: "M", AcceptanceTest: "Run 10k without stopping"}
		assert.Contains(t, buildMilestoneContext(m), "Acceptance Test: Run 10k without stopping\n")
	})
}

func TestBuildMilestoneContextSpikesAndRisks(t *testing.T) {
	m := &types.Milestone{
		SubGoal:     "M",
		Description: "D",
		Spikes: []types.Spike{
			{Description: "Test shoe brands", TimeboxDays: 2},
			{Description: "Evaluate training plans", TimeboxDays: 3},
		},
		Risks: []types.Risk{
			{Description: "Injury recurrence", Severity: "high"},
			{Description: "Weather", Severity: "low"},
		},
	}
	ctx := buildMilestoneContext(m)

	assert.Contains(t, ctx, "Spikes to investigate: Test shoe brands (2 days); Evaluate training plans (3 days); \n")
	assert.Contains(t, ctx, "Risks to consider: Injury recurrence [high]; Weather [low]; \n")
}

// ─── buildReviewContext ───────────────────────────────────────────────────────

func TestBuildReviewContext(t *testing.T) {
	t.Run("all empty answers return sentinel", func(t *testing.T) {
		got := buildReviewContext(types.MilestoneReviewAnswers{})
		assert.Equal(t, "(User did not provide review feedback.)", got)
	})

	t.Run("partial answers emit only non-empty lines", func(t *testing.T) {
		review := types.MilestoneReviewAnswers{
			CompletionStatus: "Completed everything",
			Obstacles:        "Busy work schedule",
		}
		got := buildReviewContext(review)

		assert.Contains(t, got, "- Completion status: Completed everything\n")
		assert.Contains(t, got, "- Obstacles: Busy work schedule\n")
		assert.NotContains(t, got, "What worked well")
		assert.NotContains(t, got, "Workload/pace feedback")
		assert.Equal(t, 2, strings.Count(got, "- "))
	})

	t.Run("all 7 fields emit all 7 labeled lines in order", func(t *testing.T) {
		review := types.MilestoneReviewAnswers{
			CompletionStatus: "a", WhatWorked: "b", WhatDidntWork: "c",
			Obstacles: "d", PaceFeedback: "e", ClarityFeedback: "f", GoingForward: "g",
		}
		got := buildReviewContext(review)

		labels := []string{
			"- Completion status: a",
			"- What worked well: b",
			"- What didn't work: c",
			"- Obstacles: d",
			"- Workload/pace feedback: e",
			"- Instruction clarity: f",
			"- Preferences for next milestone / life changes: g",
		}
		lastIdx := -1
		for _, label := range labels {
			idx := strings.Index(got, label)
			require.GreaterOrEqual(t, idx, 0, "missing line %q", label)
			assert.Greater(t, idx, lastIdx, "line %q out of order", label)
			lastIdx = idx
		}
		assert.Equal(t, 7, strings.Count(got, "- "))
	})
}

// ─── OpenAI envelope walk (shared by all four parsers) ────────────────────────

func TestOpenAIEnvelopeErrorsAcrossParsers(t *testing.T) {
	// All four parsers duplicate the same envelope walk, so run the same
	// malformed-envelope table across each of them.
	parsers := []struct {
		name  string
		parse func([]byte) error
	}{
		{"parseStrategicRoadmapResponse", func(b []byte) error {
			_, err := parseStrategicRoadmapResponse(b)
			return err
		}},
		{"parseMilestoneTasksResponse", func(b []byte) error {
			_, err := parseMilestoneTasksResponse(b)
			return err
		}},
		{"parseMilestoneAdjustmentsResponse", func(b []byte) error {
			_, err := parseMilestoneAdjustmentsResponse(b)
			return err
		}},
		{"ParseRoadmap", func(b []byte) error {
			_, err := ParseRoadmap(b)
			return err
		}},
	}

	cases := []struct {
		name    string
		raw     []byte
		wantErr string
	}{
		{
			name:    "malformed outer JSON",
			raw:     []byte(`{not json`),
			wantErr: "failed to parse OpenAI response",
		},
		{
			name:    "empty output array",
			raw:     agRawEnvelope(t, types.OpenAIResponse{Output: []types.OpenAIMessage{}}),
			wantErr: "no output found",
		},
		{
			name: "only reasoning outputs",
			raw: agRawEnvelope(t, types.OpenAIResponse{Output: []types.OpenAIMessage{
				{Type: "reasoning"},
				{Type: "reasoning"},
			}}),
			wantErr: "no content found",
		},
		{
			name: "message output with empty content",
			raw: agRawEnvelope(t, types.OpenAIResponse{Output: []types.OpenAIMessage{
				{Type: "message", Content: []types.OpenAIContent{}},
			}}),
			wantErr: "no content found",
		},
	}

	for _, p := range parsers {
		for _, tc := range cases {
			t.Run(p.name+"/"+tc.name, func(t *testing.T) {
				err := p.parse(tc.raw)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			})
		}
	}
}

func TestOpenAIEnvelopeSkipsEmptyMessageThenUsesNext(t *testing.T) {
	// A message output with empty Content must be skipped in favor of a later
	// message output that actually carries content.
	raw := agRawEnvelope(t, types.OpenAIResponse{Output: []types.OpenAIMessage{
		{Type: "message", Content: []types.OpenAIContent{}},
		{Type: "message", Content: []types.OpenAIContent{
			{Type: "output_text", Text: `{"goal":"Learn Go","days":10}`},
		}},
	}})

	roadmap, err := ParseRoadmap(raw)
	require.NoError(t, err)
	assert.Equal(t, "Learn Go", roadmap.Goal)
	assert.Equal(t, 10, roadmap.Days)
}

// ─── parseStrategicRoadmapResponse ────────────────────────────────────────────

func TestParseStrategicRoadmapResponse(t *testing.T) {
	t.Run("happy path decodes phases and milestones", func(t *testing.T) {
		inner := `{
			"goal": "Learn Go",
			"description": "A 30 day plan",
			"days": 30,
			"phases": [
				{
					"name": "Foundation",
					"description": "Basics",
					"order_index": 1,
					"duration_weeks": "2",
					"milestones": [
						{"sub_goal": "Env Setup", "duration_days_min": 2, "duration_days_max": 4, "day": 1},
						{"sub_goal": "First Program", "day": 5}
					]
				},
				{
					"name": "Advanced",
					"order_index": 2,
					"milestones": [
						{"sub_goal": "Concurrency", "day": 15}
					]
				}
			]
		}`
		roadmap, err := parseStrategicRoadmapResponse(agOpenAIEnvelope(t, inner))
		require.NoError(t, err)

		assert.Equal(t, "Learn Go", roadmap.Goal)
		assert.Equal(t, "A 30 day plan", roadmap.Description)
		assert.Equal(t, 30, roadmap.Days)
		require.Len(t, roadmap.Phases, 2)
		assert.Equal(t, "Foundation", roadmap.Phases[0].Name)
		require.Len(t, roadmap.Phases[0].Milestones, 2)
		assert.Equal(t, "Env Setup", roadmap.Phases[0].Milestones[0].SubGoal)
		assert.Equal(t, 2, roadmap.Phases[0].Milestones[0].DurationDaysMin)
		assert.Equal(t, 4, roadmap.Phases[0].Milestones[0].DurationDaysMax)
		require.Len(t, roadmap.Phases[1].Milestones, 1)
		assert.Equal(t, "Concurrency", roadmap.Phases[1].Milestones[0].SubGoal)
	})

	t.Run("invalid inner JSON", func(t *testing.T) {
		_, err := parseStrategicRoadmapResponse(agOpenAIEnvelope(t, `{bad inner`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse strategic roadmap JSON")
	})
}

// ─── parseMilestoneTasksResponse ──────────────────────────────────────────────

func TestParseMilestoneTasksResponse(t *testing.T) {
	t.Run("happy path decodes titles days and steps", func(t *testing.T) {
		inner := `{
			"tasks": [
				{
					"title": "Install Go",
					"description": "Download and install",
					"est_minutes": 30,
					"day": 1,
					"completed": false,
					"steps": [
						{"description": "Visit go.dev", "completed": false, "number": 1},
						{"description": "Run installer", "completed": false, "number": 2}
					]
				},
				{"title": "Write hello world", "day": 2, "steps": []}
			]
		}`
		tasks, err := parseMilestoneTasksResponse(agOpenAIEnvelope(t, inner))
		require.NoError(t, err)

		require.Len(t, tasks, 2)
		assert.Equal(t, "Install Go", tasks[0].Title)
		assert.Equal(t, 1, tasks[0].Day)
		assert.Equal(t, 30, tasks[0].EstMinutes)
		require.Len(t, tasks[0].Steps, 2)
		assert.Equal(t, "Visit go.dev", tasks[0].Steps[0].Description)
		assert.Equal(t, 2, tasks[0].Steps[1].Number)
		assert.Equal(t, "Write hello world", tasks[1].Title)
		assert.Equal(t, 2, tasks[1].Day)
	})

	t.Run("empty tasks array returns empty non-error slice", func(t *testing.T) {
		tasks, err := parseMilestoneTasksResponse(agOpenAIEnvelope(t, `{"tasks":[]}`))
		require.NoError(t, err)
		assert.Empty(t, tasks)
	})

	t.Run("invalid inner JSON", func(t *testing.T) {
		_, err := parseMilestoneTasksResponse(agOpenAIEnvelope(t, `{"tasks": "nope"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse tasks JSON")
	})
}

// ─── parseMilestoneAdjustmentsResponse ────────────────────────────────────────

func TestParseMilestoneAdjustmentsResponse(t *testing.T) {
	t.Run("adjusted true decodes all fields", func(t *testing.T) {
		inner := `{
			"adjusted": true,
			"sub_goal": "Slower Base Building",
			"description": "Extended base phase",
			"acceptance_test": "Run 5k comfortably",
			"duration_days_min": 10,
			"duration_days_max": 20,
			"resources_forecast": "{\"tools\":[\"watch\"]}",
			"spikes": [{"description": "Test recovery drills", "timebox_days": 2}],
			"risks": [{"description": "Overtraining", "severity": "medium"}],
			"rationale": "User asked for slower pacing"
		}`
		adj, err := parseMilestoneAdjustmentsResponse(agOpenAIEnvelope(t, inner))
		require.NoError(t, err)

		assert.True(t, adj.Adjusted)
		assert.Equal(t, "Slower Base Building", adj.SubGoal)
		assert.Equal(t, "Extended base phase", adj.Description)
		assert.Equal(t, "Run 5k comfortably", adj.AcceptanceTest)
		assert.Equal(t, 10, adj.DurationDaysMin)
		assert.Equal(t, 20, adj.DurationDaysMax)
		assert.Equal(t, `{"tools":["watch"]}`, adj.ResourcesForecast)
		require.Len(t, adj.Spikes, 1)
		assert.Equal(t, 2, adj.Spikes[0].TimeboxDays)
		require.Len(t, adj.Risks, 1)
		assert.Equal(t, "medium", adj.Risks[0].Severity)
		assert.Equal(t, "User asked for slower pacing", adj.Rationale)
	})

	t.Run("adjusted false with sentinels decodes zero values", func(t *testing.T) {
		inner := `{
			"adjusted": false,
			"sub_goal": "",
			"description": "",
			"acceptance_test": "",
			"duration_days_min": 0,
			"duration_days_max": 0,
			"resources_forecast": "",
			"spikes": [],
			"risks": [],
			"rationale": ""
		}`
		adj, err := parseMilestoneAdjustmentsResponse(agOpenAIEnvelope(t, inner))
		require.NoError(t, err)

		assert.False(t, adj.Adjusted)
		assert.Empty(t, adj.SubGoal)
		assert.Zero(t, adj.DurationDaysMin)
		assert.Zero(t, adj.DurationDaysMax)
		assert.Empty(t, adj.ResourcesForecast)
		assert.Empty(t, adj.Spikes)
		assert.Empty(t, adj.Risks)
	})

	t.Run("invalid inner JSON", func(t *testing.T) {
		_, err := parseMilestoneAdjustmentsResponse(agOpenAIEnvelope(t, `{"adjusted": "maybe"`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse milestone adjustments JSON")
	})
}

// ─── ParseRoadmap ─────────────────────────────────────────────────────────────

func TestParseRoadmapHappyPath(t *testing.T) {
	inner := `{
		"goal": "Learn piano",
		"description": "Beginner plan",
		"days": 60,
		"milestones": [
			{
				"sub_goal": "Read sheet music",
				"day": 1,
				"tasks": [
					{"title": "Learn treble clef", "day": 1},
					{"title": "Learn bass clef", "day": 3}
				]
			},
			{"sub_goal": "First song", "day": 14}
		]
	}`
	roadmap, err := ParseRoadmap(agOpenAIEnvelope(t, inner))
	require.NoError(t, err)

	assert.Equal(t, "Learn piano", roadmap.Goal)
	assert.Equal(t, 60, roadmap.Days)
	require.Len(t, roadmap.Milestones, 2)
	assert.Equal(t, "Read sheet music", roadmap.Milestones[0].SubGoal)
	require.Len(t, roadmap.Milestones[0].Tasks, 2)
	assert.Equal(t, "Learn bass clef", roadmap.Milestones[0].Tasks[1].Title)
	assert.Empty(t, roadmap.Milestones[1].Tasks)
}

func TestParseRoadmapInvalidInnerJSON(t *testing.T) {
	_, err := ParseRoadmap(agOpenAIEnvelope(t, `not json at all`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse embedded goal roadmap JSON")
}
