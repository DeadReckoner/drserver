package types

import (
	"encoding/json"
	"fmt"
)

// goalOptionsPayload is the JSON shape both AI backends produce: OpenAI returns
// it as strict json_schema output, Anthropic as forced tool-use input.
//
// It is deliberately separate from GoalOption so the model has no way to supply
// an option id. Stage 2 proves the user picked a real option by looking its id
// up in the stored result, so ids are minted server-side after validation.
type goalOptionsPayload struct {
	SafetyBlocked bool   `json:"safety_blocked"`
	SafetyReason  string `json:"safety_reason"`
	Options       []struct {
		Rank             int    `json:"rank"`
		GoalTitle        string `json:"goal_title"`
		WhyThisFits      string `json:"why_this_fits"`
		TargetOutcome    string `json:"target_outcome"`
		WhyWorthPursuing string `json:"why_worth_pursuing"`
		EffortExamples   []struct {
			Pace                       string  `json:"pace"`
			HoursPerWeekMin            float64 `json:"hours_per_week_min"`
			HoursPerWeekMax            float64 `json:"hours_per_week_max"`
			EstimatedDurationMonthsMin float64 `json:"estimated_duration_months_min"`
			EstimatedDurationMonthsMax float64 `json:"estimated_duration_months_max"`
			DisplayText                string  `json:"display_text"`
		} `json:"effort_examples"`
		Difficulty struct {
			Level       string `json:"level"`
			Explanation string `json:"explanation"`
		} `json:"difficulty"`
		AccountabilityStrength struct {
			Level       string `json:"level"`
			Explanation string `json:"explanation"`
		} `json:"accountability_strength"`
	} `json:"options"`
}

// DecodeGoalOptionsPayload converts a provider's raw JSON into the domain type.
// It does no validation beyond "is this JSON of the right shape" — coherence and
// completeness are ValidateGoalOptions's job, so a malformed set reaches the
// repair retry with a specific complaint rather than a decode error.
func DecodeGoalOptionsPayload(raw []byte) (*GoalOptionsOutput, error) {
	var payload goalOptionsPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse goal options payload: %w", err)
	}

	if payload.SafetyBlocked {
		return &GoalOptionsOutput{Safety: &GoalOptionsSafety{
			Blocked: true,
			Reason:  payload.SafetyReason,
			Message: GoalOptionsSafetyMessage,
		}}, nil
	}

	options := make([]GoalOption, 0, len(payload.Options))
	for _, item := range payload.Options {
		efforts := make([]GoalOptionEffort, 0, len(item.EffortExamples))
		for _, e := range item.EffortExamples {
			efforts = append(efforts, GoalOptionEffort{
				Pace:                       GoalOptionPace(e.Pace),
				HoursPerWeekMin:            e.HoursPerWeekMin,
				HoursPerWeekMax:            e.HoursPerWeekMax,
				EstimatedDurationMonthsMin: e.EstimatedDurationMonthsMin,
				EstimatedDurationMonthsMax: e.EstimatedDurationMonthsMax,
				DisplayText:                e.DisplayText,
			})
		}
		options = append(options, GoalOption{
			Rank:             item.Rank,
			GoalTitle:        item.GoalTitle,
			WhyThisFits:      item.WhyThisFits,
			TargetOutcome:    item.TargetOutcome,
			WhyWorthPursuing: item.WhyWorthPursuing,
			EffortExamples:   efforts,
			Difficulty: GoalOptionDifficulty{
				Level:       GoalOptionDifficultyLevel(item.Difficulty.Level),
				Explanation: item.Difficulty.Explanation,
			},
			AccountabilityStrength: GoalOptionAccountability{
				Level:       GoalOptionAccountabilityLevel(item.AccountabilityStrength.Level),
				Explanation: item.AccountabilityStrength.Explanation,
			},
		})
	}
	return &GoalOptionsOutput{Options: options}, nil
}
