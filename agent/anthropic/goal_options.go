package anthropic

import (
	"context"
	"fmt"

	"dr/server/log"
	"dr/server/types"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// GoalOptionsToolName is the forced tool for goal-option generation. Exported so
// the agent package can decode the tool input without restating the literal.
const GoalOptionsToolName = "generate_goal_options"

// GenerateGoalOptions implements agent.Provider for the Anthropic backend using
// forced tool use, the same structured-output mechanism as roadmap generation.
//
// Decoding goes through types.DecodeGoalOptionsPayload, which the OpenAI backend
// also uses, so both produce identical domain values from identical JSON.
func (p *Provider) GenerateGoalOptions(ctx context.Context, in types.GoalOptionsInput) (*types.GoalOptionsOutput, error) {
	raw, err := p.generateGoalOptionsRaw(ctx, in.Answers, in.Exclude, in.RepairHint)
	if err != nil {
		return nil, err
	}
	return types.DecodeGoalOptionsPayload(raw)
}

// generateGoalOptionsRaw performs the tool-use call and returns the raw tool
// input JSON.
func (p *Provider) generateGoalOptionsRaw(ctx context.Context, answers types.GoalDiscoveryAnswers, exclude []string, repairHint string) ([]byte, error) {
	log.Info(ctx, "Starting Anthropic goal options generation", types.GoalOptionsPromptVersion)

	tool := goalOptionsTool()
	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: types.GoalOptionsMaxOutputTokens,
		System: []anthropic.TextBlockParam{
			{Text: types.GoalOptionsSystemPrompt()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(
				types.GoalOptionsUserPrompt(answers, exclude, repairHint))),
		},
		Tools: []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: GoalOptionsToolName},
		},
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == GoalOptionsToolName {
			return block.Input, nil
		}
	}
	return nil, fmt.Errorf("no tool use found in response")
}

// goalOptionsTool mirrors the OpenAI strict schema exactly: both are built from
// types.GoalOptionsSchemaProperties, so the two backends cannot describe
// different contracts to their models.
func goalOptionsTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name:        GoalOptionsToolName,
		Description: param.NewOpt("Return exactly three ranked, materially different goal options for the user's questionnaire answers"),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: types.GoalOptionsSchemaProperties(),
			Required:   types.GoalOptionsSchemaRequired(),
		},
	}
}
