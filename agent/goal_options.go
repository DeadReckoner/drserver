package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"dr/server/log"
	"dr/server/types"

	"github.com/google/uuid"
)

// newGoalOptionID mints the server-owned identifier for a generated option.
var newGoalOptionID = uuid.NewString

// goalOptionsRepairAttempts bounds the structured-output repair loop. One
// initial attempt plus one repair: enough to recover from a single malformed
// response, few enough that a persistently broken model can't burn budget.
const goalOptionsRepairAttempts = 1

// ErrGoalOptionsInvalid is the typed terminal failure returned when the model
// could not produce a schema-valid option set within the retry budget. Callers
// surface a generic message to the client — the wrapped detail is for logs only.
var ErrGoalOptionsInvalid = errors.New("goal options failed validation")

// GenerateValidatedGoalOptions runs a provider and guarantees the caller either
// gets a fully validated set of exactly three options, a typed safety block, or
// an error. An invalid response triggers at most goalOptionsRepairAttempts
// retries, each told exactly what was wrong with the previous one.
func GenerateValidatedGoalOptions(ctx context.Context, provider Provider, in types.GoalOptionsInput) (*types.GoalOptionsOutput, error) {
	var lastErr error

	for attempt := 0; attempt <= goalOptionsRepairAttempts; attempt++ {
		out, err := provider.GenerateGoalOptions(ctx, in)
		if err != nil {
			// A transport/provider error is not repairable by re-prompting.
			return nil, fmt.Errorf("goal options generation failed: %w", err)
		}
		if out == nil {
			lastErr = errors.New("provider returned no result")
			continue
		}

		// A safety block is a terminal success: there is nothing to validate,
		// and retrying would just ask the model to reconsider a refusal.
		if out.Safety != nil && out.Safety.Blocked {
			return out, nil
		}

		if err := types.ValidateGoalOptions(out.Options); err != nil {
			lastErr = err
			log.Info(ctx, "goal options failed validation",
				fmt.Sprintf("attempt=%d provider=%s error=%v", attempt+1, provider.Name(), err))
			in.RepairHint = err.Error()
			continue
		}

		assignGoalOptionIDs(out.Options)
		return out, nil
	}

	return nil, fmt.Errorf("%w after %d attempts: %v", ErrGoalOptionsInvalid, goalOptionsRepairAttempts+1, lastErr)
}

// assignGoalOptionIDs stamps server-owned IDs over whatever the model produced.
// Stage 2 proves the user picked a real option by looking its id up in the
// stored result, so the id must never be model- or client-controlled.
func assignGoalOptionIDs(options []types.GoalOption) {
	for i := range options {
		options[i].ID = newGoalOptionID()
	}
}

// ─── OpenAI implementation ───────────────────────────────────────────────────

// GenerateGoalOptions implements Provider for the OpenAI backend using the same
// strict json_schema structured-output mode as the roadmap generator.
func (p *OpenAIProvider) GenerateGoalOptions(ctx context.Context, in types.GoalOptionsInput) (*types.GoalOptionsOutput, error) {
	log.Info(ctx, "Starting OpenAI goal options generation", types.GoalOptionsPromptVersion)

	reqBody := AIRequest{
		Model:     "gpt-5-nano",
		MaxTokens: types.GoalOptionsMaxOutputTokens,
		Input: []Message{
			{Role: "system", Content: types.GoalOptionsSystemPrompt()},
			{Role: "user", Content: types.GoalOptionsUserPrompt(in.Answers, in.Exclude, in.RepairHint)},
		},
	}
	reqBody.Text.Format.Type = "json_schema"
	reqBody.Text.Format.Name = "goal_options"
	reqBody.Text.Format.Schema = goalOptionsJSONSchema()
	reqBody.Text.Format.Strict = true

	raw, err := makeOpenAIRequest(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	return parseGoalOptionsResponse(raw)
}

// parseGoalOptionsResponse unwraps the OpenAI responses envelope and decodes the
// goal-options payload from the first message block, skipping reasoning blocks.
func parseGoalOptionsResponse(raw []byte) (*types.GoalOptionsOutput, error) {
	var apiResp types.OpenAIResponse
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	for _, output := range apiResp.Output {
		if output.Type != "message" || len(output.Content) == 0 {
			continue
		}
		return types.DecodeGoalOptionsPayload([]byte(output.Content[0].Text))
	}
	return nil, fmt.Errorf("no content found in OpenAI output")
}

// goalOptionsJSONSchema wraps the shared properties for OpenAI strict mode,
// which requires every property to be listed in `required`. The Anthropic tool
// definition is built from the same two functions, so the backends cannot drift.
func goalOptionsJSONSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           types.GoalOptionsSchemaProperties(),
		"required":             types.GoalOptionsSchemaRequired(),
		"additionalProperties": false,
	}
}
