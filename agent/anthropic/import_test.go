package anthropic

import (
	"strings"
	"testing"
	"time"

	"dr/server/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// samplePlanPayload is a transpose_plan tool payload shaped the way the model
// actually answers: dependencies as a real JSON array (not the string literal
// types.Task.Dependencies stores), milestone days that stall and then run
// backwards, task days outside their milestone's window, and a step with no
// description.
const samplePlanPayload = `{
  "roadmap": {
    "goal": "Ship the mobile app",
    "description": "  Q3 launch plan  ",
    "days": 0,
    "phases": [
      {
        "name": "Build",
        "description": "Core feature work",
        "order_index": 7,
        "milestones": [
          {
            "sub_goal": "Auth flow done",
            "description": "Sign-in and sign-up",
            "acceptance_test": "A new user can register and log in",
            "duration_days_min": 10,
            "duration_days_max": 0,
            "order_index": 3,
            "day": 0,
            "tasks": [
              {
                "title": "Wire Supabase auth",
                "description": "Hook up the client",
                "day": 0,
                "completed": true,
                "dependencies": [],
                "steps": [
                  {"description": "Add the SDK", "number": 4, "completed": true},
                  {"description": "   ", "number": 5, "completed": false}
                ]
              },
              {
                "title": "Build sign-in screen",
                "description": "Form plus validation",
                "day": 99,
                "completed": false,
                "dependencies": ["Wire Supabase auth"],
                "steps": []
              }
            ]
          },
          {
            "sub_goal": "",
            "description": "Offline support",
            "acceptance_test": "App opens with no network",
            "duration_days_min": 0,
            "duration_days_max": 0,
            "order_index": 0,
            "day": 2,
            "tasks": []
          }
        ]
      },
      {
        "name": "",
        "description": "Store submission",
        "order_index": 0,
        "milestones": [
          {
            "sub_goal": "Submitted to review",
            "description": "TestFlight then App Store",
            "acceptance_test": "Build is in review",
            "duration_days_min": 5,
            "duration_days_max": 9,
            "order_index": 0,
            "day": 1,
            "tasks": []
          }
        ]
      }
    ]
  },
  "import_notes": {
    "source_summary": "A one-page Q3 launch plan.",
    "fidelity": "high",
    "inferred": ["No durations in the source; estimated from scope"],
    "dropped": ["Budget table on page 2"]
  }
}`

func TestParsePlanImport(t *testing.T) {
	result, err := parsePlanImport([]byte(samplePlanPayload))
	require.NoError(t, err)

	// Notes survive alongside the roadmap so the client can show what was
	// inferred versus preserved.
	assert.Equal(t, "high", result.Notes.Fidelity)
	assert.Equal(t, []string{"Budget table on page 2"}, result.Notes.Dropped)

	r := result.Roadmap
	assert.Equal(t, "Ship the mobile app", r.Goal)
	assert.Equal(t, "Q3 launch plan", r.Description, "descriptions are trimmed")
	require.Len(t, r.Phases, 2)

	// Order indexes are reassigned from the order the model returned things in,
	// which is the order the document had them.
	assert.Equal(t, 0, r.Phases[0].OrderIndex)
	assert.Equal(t, 1, r.Phases[1].OrderIndex)
	assert.Equal(t, "Phase 2", r.Phases[1].Name, "an unnamed phase gets a positional name")

	auth := r.Phases[0].Milestones[0]
	offline := r.Phases[0].Milestones[1]
	submit := r.Phases[1].Milestones[0]

	// Milestone order_index is global across phases, matching the tactical
	// milestone list the user sees.
	assert.Equal(t, 0, auth.OrderIndex)
	assert.Equal(t, 1, offline.OrderIndex)
	assert.Equal(t, 2, submit.OrderIndex)
	assert.Equal(t, "Milestone 2", offline.SubGoal, "an unnamed milestone gets a positional name")

	// Days are forced non-decreasing: the source said 0, 2, 1.
	assert.Equal(t, 0, auth.Day)
	assert.Equal(t, 10, offline.Day, "starts after the 10-day auth milestone")
	assert.Equal(t, 24, submit.Day, "starts after the defaulted 14-day offline milestone")
	assert.Equal(t, defaultMilestoneDurationDays, offline.DurationDaysMin, "no stated duration falls back")
	assert.Equal(t, 10, auth.DurationDaysMax, "max is raised to meet min")

	// Total span covers the last milestone's window.
	assert.Equal(t, 33, r.Days)

	require.Len(t, auth.Tasks, 2)
	assert.Equal(t, 0, auth.Tasks[0].Day)
	assert.Equal(t, 9, auth.Tasks[1].Day, "a day past the milestone window is clamped to its last day")
	assert.False(t, auth.Tasks[0].Completed, "an import always starts fresh")

	// Dependencies arrive as a JSON array and are stored as the array literal
	// types.Task.Dependencies expects.
	require.NotNil(t, auth.Tasks[1].Dependencies)
	assert.Equal(t, `["Wire Supabase auth"]`, *auth.Tasks[1].Dependencies)

	// Blank steps are dropped and the rest renumbered from 1.
	require.Len(t, auth.Tasks[0].Steps, 1)
	assert.Equal(t, 1, auth.Tasks[0].Steps[0].Number)
	assert.False(t, auth.Tasks[0].Steps[0].Completed)
	assert.Equal(t, string(types.StepStatusIncomplete), auth.Tasks[0].Steps[0].Status)
}

func TestParsePlanImport_NoMilestones(t *testing.T) {
	_, err := parsePlanImport([]byte(`{"roadmap":{"goal":"Something","phases":[]},"import_notes":{}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no milestones")
}

func TestNormalizeDependencyArrays(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "array becomes a JSON string literal",
			input: `{"tasks":[{"dependencies":["A","B"]}]}`,
			want:  `{"tasks":[{"dependencies":"[\"A\",\"B\"]"}]}`,
		},
		{
			name:  "empty array",
			input: `{"dependencies":[]}`,
			want:  `{"dependencies":"[]"}`,
		},
		{
			name:  "already a string is left alone",
			input: `{"dependencies":"[\"A\"]"}`,
			want:  `{"dependencies":"[\"A\"]"}`,
		},
		{
			name:  "null is left alone",
			input: `{"dependencies":null}`,
			want:  `{"dependencies":null}`,
		},
		{
			name:  "rewritten at any depth",
			input: `{"a":{"b":[{"c":{"dependencies":["X"]}}]}}`,
			want:  `{"a":{"b":[{"c":{"dependencies":"[\"X\"]"}}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeDependencyArrays([]byte(tt.input))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

// TestNormalizeImportedRoadmap_TaskSequencing covers the case a plan with no
// timing produces: every task claims day 0, which would collapse the ordering
// that StartRoadmap and the status computation rely on.
func TestNormalizeImportedRoadmap_TaskSequencing(t *testing.T) {
	r := types.Roadmap{
		Phases: []types.Phase{{
			Name: "Only phase",
			Milestones: []types.Milestone{{
				SubGoal:         "First",
				DurationDaysMin: 3,
				Tasks: []types.Task{
					{Title: "One"}, {Title: "Two"}, {Title: "Three"}, {Title: "Four"},
				},
			}},
		}},
	}

	require.NoError(t, normalizeImportedRoadmap(&r))

	tasks := r.Phases[0].Milestones[0].Tasks
	days := []int{tasks[0].Day, tasks[1].Day, tasks[2].Day, tasks[3].Day}
	// Spread one per day, then held at the last day of the 3-day window rather
	// than spilling into the next milestone.
	assert.Equal(t, []int{0, 1, 2, 2}, days)
}

// TestNormalizeImportedRoadmap_TopLevelMilestones guards the fallback shape:
// milestones outside any phase still get sequenced into the same global day
// ordering.
func TestNormalizeImportedRoadmap_TopLevelMilestones(t *testing.T) {
	r := types.Roadmap{
		Milestones: []types.Milestone{
			{SubGoal: "A", DurationDaysMin: 5},
			{SubGoal: "B", DurationDaysMin: 5},
		},
	}

	require.NoError(t, normalizeImportedRoadmap(&r))

	assert.Equal(t, 0, r.Milestones[0].Day)
	assert.Equal(t, 5, r.Milestones[1].Day)
	assert.Equal(t, 10, r.Days)
}

func TestBuildImportPrompt(t *testing.T) {
	now := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)

	t.Run("no hints", func(t *testing.T) {
		got := buildImportPrompt(types.PlanImportContext{}, now)
		assert.Contains(t, got, "2026-07-28")
		assert.Contains(t, got, "rely entirely on the document")
	})

	t.Run("with hints", func(t *testing.T) {
		got := buildImportPrompt(types.PlanImportContext{
			GoalName:             "Ship the app",
			WeeklyTimeCommitment: "10 hours",
		}, now)
		assert.Contains(t, got, "- Goal name: Ship the app")
		assert.Contains(t, got, "- Weekly time commitment: 10 hours")
		assert.NotContains(t, got, "Constraints", "blank hints are omitted")
		assert.Contains(t, got, "the document wins", "hints must not override the document")
	})
}

// TestTransposePlanTool_Schema pins the parts of the tool contract the parsing
// and persistence code depends on.
func TestTransposePlanTool_Schema(t *testing.T) {
	tool := transposePlanTool()
	assert.Equal(t, importToolName, tool.Name)
	assert.ElementsMatch(t, []string{"roadmap", "import_notes"}, tool.InputSchema.Required)

	roadmap, ok := tool.InputSchema.Properties.(map[string]interface{})["roadmap"].(map[string]interface{})
	require.True(t, ok)
	props, ok := roadmap["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "phases")
	assert.Contains(t, props, "goal")
}

// TestTransposePlanSystemPrompt_GeneratesEverythingUpFront pins the two rules
// that make an import self-sufficient.
//
// Tasks: an earlier version filled in tasks for the FIRST milestone only and
// left the rest to the next-milestone flow — which deletes a milestone's tasks
// before regenerating them, so on an imported roadmap it would have destroyed
// the user's own content. Importing a plan the user already has means every
// milestone is populated in one pass and that flow is never needed.
//
// Steps: an earlier version skipped steps entirely for tasks carried over
// intact, leaving imported tasks with empty checklists. Steps are required now,
// but a task with no sub-bullets in the source gets ONE step restating it
// rather than invented filler.
func TestTransposePlanSystemPrompt_GeneratesEverythingUpFront(t *testing.T) {
	prompt := transposePlanSystemPrompt()

	assert.Contains(t, prompt, "EVERY milestone needs tasks")
	assert.NotContains(t, prompt, "ONLY for the FIRST milestone")

	assert.Contains(t, prompt, "a SINGLE step restating the task")
	assert.NotContains(t, prompt, "needs no invented steps")
}

func TestTransposePlanSystemPrompt_StatesDayContract(t *testing.T) {
	prompt := transposePlanSystemPrompt()
	// StartRoadmap dates milestones and tasks as startDate + Day, so the prompt
	// has to ask for global offsets, not per-milestone ones.
	assert.True(t, strings.Contains(prompt, "GLOBAL across the roadmap"),
		"the prompt must state that day is a roadmap-global offset")
	assert.Contains(t, prompt, "Day 0 is the start day")
}
