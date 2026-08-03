package agent

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"

	"dr/server/types"
)

// MockProvider is a deterministic, fully offline AI backend for local
// development and tests. It never makes a network call and never needs a key.
//
// It is NOT a shortcut past validation: everything it returns goes through the
// same runtime schema validation as a real provider's output, so exercising the
// async flow against it proves the real validation path works.
//
// Selected only by AI_PROVIDER=mock, and refused outright on a hosted runtime
// (see DefaultProvider) so a misconfigured deploy fails loudly at startup
// instead of quietly serving fake plans.
type MockProvider struct{}

func NewMockProvider() *MockProvider { return &MockProvider{} }

func (p *MockProvider) Name() string { return string(ProviderMock) }

// mockCrisisMarkers is the smallest possible stand-in for the real model's
// safety judgement: it lets local validation exercise the safety-blocked branch
// deterministically. It is not, and must not be mistaken for, content moderation.
var mockCrisisMarkers = []string{"crisis-test-marker"}

func (p *MockProvider) GenerateGoalOptions(ctx context.Context, in types.GoalOptionsInput) (*types.GoalOptionsOutput, error) {
	haystack := strings.ToLower(strings.Join([]string{
		in.Answers.RecurringThought, in.Answers.SuccessDefinition, in.Answers.TriggerNote,
	}, " "))

	// Deterministic failure path. Lets local validation prove that a failed
	// regeneration leaves the previously generated options intact, without
	// needing a real provider outage to reproduce it.
	if strings.Contains(haystack, mockFailureMarker) {
		return nil, fmt.Errorf("mock provider: deterministic failure marker present")
	}

	for _, marker := range mockCrisisMarkers {
		if strings.Contains(haystack, marker) {
			return &types.GoalOptionsOutput{Safety: &types.GoalOptionsSafety{
				Blocked: true,
				Reason:  "mock provider crisis marker",
				Message: types.GoalOptionsSafetyMessage,
			}}, nil
		}
	}

	// A regeneration must return a materially different set. The variant is
	// derived from the CONTENT of the exclusions, not their count: every
	// regeneration excludes exactly three titles, so counting them would hand
	// back the same three goals forever and make the offline provider useless
	// for checking that a replacement actually replaced anything.
	return &types.GoalOptionsOutput{
		Options: mockGoalOptions(in.Answers, in.Exclude),
	}, nil
}

// mockVariant maps a set of excluded titles onto a starting theme offset.
// Deterministic (same exclusions in, same options out) but content-sensitive, so
// each newly rejected set moves the offline provider on to different goals.
func mockVariant(exclude []string) int {
	if len(exclude) == 0 {
		return 0
	}
	sorted := append([]string(nil), exclude...)
	sort.Strings(sorted)

	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.Join(sorted, "|")))
	return int(h.Sum32() % uint32(len(mockThemes)))
}

// mockThemes is the offline provider's pool of goal shapes: title and the
// concrete "done" condition that goes with it. Needs to be larger than
// GoalDiscoveryOptionCount so a regeneration has somewhere new to go.
var mockThemes = [][2]string{
	{"Ship a small real project", "one finished project other people can actually use"},
	{"Build a daily practice you keep", "an unbroken 60-day streak on one specific daily action"},
	{"Earn your first outside result", "a first paying customer, published piece, or completed certification"},
	{"Rebuild one weekly system", "a written weekly routine you have followed for eight straight weeks"},
	{"Learn one skill to a demo", "a recorded demonstration of the skill at an intermediate level"},
	{"Clear one long-standing backlog", "the backlog list emptied and a rule in place that stops it refilling"},
	{"Publish something on a schedule", "eight pieces published on the dates you committed to"},
	{"Build a small paying offer", "one offer priced, listed, and sold to someone you do not know"},
}

// mockGoalOptions builds three coherent, distinct options. The pace lanes are
// deliberately constructed to satisfy the coherence rules (heavier pace = more
// weekly hours and a shorter timeline) rather than by accident.
func mockGoalOptions(answers types.GoalDiscoveryAnswers, exclude []string) []types.GoalOption {
	area := "your focus area"
	if len(answers.Areas) > 0 {
		area = strings.ReplaceAll(string(answers.Areas[0]), "_", " ")
	}

	rejected := make(map[string]bool, len(exclude))
	for _, title := range exclude {
		rejected[title] = true
	}

	// Walk the theme pool from a content-derived offset and take the first
	// three that were not already rejected. Skipping explicitly (rather than
	// hoping an offset lands clear) is what makes a regeneration reliably
	// different — the pool is larger than three plus a rejected set, so this
	// always finds enough.
	start := mockVariant(exclude)
	chosen := make([][2]string, 0, types.GoalDiscoveryOptionCount)
	for step := 0; step < len(mockThemes) && len(chosen) < types.GoalDiscoveryOptionCount; step++ {
		theme := mockThemes[(start+step)%len(mockThemes)]
		if rejected[fmt.Sprintf("%s in %s", theme[0], area)] {
			continue
		}
		chosen = append(chosen, theme)
	}

	options := make([]types.GoalOption, 0, types.GoalDiscoveryOptionCount)
	for i, theme := range chosen {
		difficulty := []types.GoalOptionDifficultyLevel{
			types.DifficultyModerate, types.DifficultyStarter, types.DifficultyDemanding,
		}[i]
		accountability := []types.GoalOptionAccountabilityLevel{
			types.AccountabilityModerate, types.AccountabilityLow, types.AccountabilityHigh,
		}[i]

		options = append(options, types.GoalOption{
			Rank:             i + 1,
			GoalTitle:        fmt.Sprintf("%s in %s", theme[0], area),
			WhyThisFits:      fmt.Sprintf("You said %s feels below standard and that you want a %s.", area, answers.TargetType),
			TargetOutcome:    fmt.Sprintf("Done means %s.", theme[1]),
			WhyWorthPursuing: "It converts a vague intention into something you can point at and verify.",
			EffortExamples: []types.GoalOptionEffort{
				{
					Pace: types.PaceFocused, HoursPerWeekMin: 8, HoursPerWeekMax: 10,
					EstimatedDurationMonthsMin: 2, EstimatedDurationMonthsMax: 3,
					DisplayText: "About 8-10 hours a week, roughly 2-3 months",
				},
				{
					Pace: types.PaceSteady, HoursPerWeekMin: 4, HoursPerWeekMax: 6,
					EstimatedDurationMonthsMin: 4, EstimatedDurationMonthsMax: 5,
					DisplayText: "About 4-6 hours a week, roughly 4-5 months",
				},
				{
					Pace: types.PaceLight, HoursPerWeekMin: 2, HoursPerWeekMax: 3,
					EstimatedDurationMonthsMin: 7, EstimatedDurationMonthsMax: 9,
					DisplayText: "About 2-3 hours a week, roughly 7-9 months",
				},
			},
			Difficulty: types.GoalOptionDifficulty{
				Level:       difficulty,
				Explanation: "Based on the starting point and weekly time you described.",
			},
			AccountabilityStrength: types.GoalOptionAccountability{
				Level:       accountability,
				Explanation: "How much outside pressure this goal needs before it starts slipping.",
			},
		})
	}
	return options
}

func (p *MockProvider) GenerateStrategicRoadmap(ctx context.Context, goalReq types.GoalRequest) (*types.Roadmap, error) {
	goal := goalReq.GoalName
	if goal == "" {
		goal = "Your goal"
	}
	return &types.Roadmap{
		Goal:        goal,
		Description: goalReq.GoalDescription,
		Days:        84,
		Phases: []types.Phase{
			{
				Name:          "Foundation",
				Description:   "Set up what you need and prove the first small result.",
				OrderIndex:    1,
				DurationWeeks: "4 weeks",
				Milestones: []types.Milestone{
					{
						SubGoal:         "First working result",
						Description:     "Get one end-to-end result finished, however small.",
						AcceptanceTest:  "A finished artefact exists and can be shown to someone else.",
						DurationDaysMin: 10,
						DurationDaysMax: 14,
						OrderIndex:      1,
						Day:             1,
					},
				},
			},
			{
				Name:          "Build",
				Description:   "Scale the routine that produced the first result.",
				OrderIndex:    2,
				DurationWeeks: "8 weeks",
				Milestones: []types.Milestone{
					{
						SubGoal:         "Repeatable output",
						Description:     "Produce the same kind of result on a schedule.",
						AcceptanceTest:  "Three consecutive weeks hit the weekly output target.",
						DurationDaysMin: 14,
						DurationDaysMax: 21,
						OrderIndex:      1,
						Day:             15,
					},
				},
			},
		},
	}, nil
}

func (p *MockProvider) GenerateMilestoneTasks(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone) ([]types.Task, error) {
	return []types.Task{
		{Title: "Block the time on your calendar", Description: "Reserve the weekly slots you committed to.", Day: 1},
		{Title: "Gather what you need to start", Description: "Collect the tools, accounts, or materials required.", Day: 2},
		{Title: "Do the smallest complete version", Description: "Finish a tiny end-to-end version before improving it.", Day: 4},
	}, nil
}

func (p *MockProvider) GenerateNextMilestoneFromReview(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone, review types.MilestoneReviewAnswers) (*types.MilestoneAdjustments, []types.Task, error) {
	tasks, err := p.GenerateMilestoneTasks(ctx, goalReq, milestone)
	if err != nil {
		return nil, nil, err
	}
	return &types.MilestoneAdjustments{Adjusted: false}, tasks, nil
}

// mockFailureMarker lets local validation deterministically exercise the
// generation-failure path (and, on regeneration, prove the previous options
// survive a failed replacement) without needing a real provider outage.
const mockFailureMarker = "fail-test-marker"

// isHostedRuntime reports whether the process is running on the hosted
// platform. Fly injects FLY_APP_NAME into every machine, so its presence is a
// reliable "this is not a developer laptop" signal.
func isHostedRuntime() bool {
	return strings.TrimSpace(os.Getenv("FLY_APP_NAME")) != ""
}
