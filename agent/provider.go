package agent

import (
	"context"
	"os"

	"dr/server/agent/anthropic"
	"dr/server/types"
)

// Provider defines the interface for AI generation backends.
// Both OpenAI and Anthropic implementations can satisfy this interface
// for the structured roadmap generation flow.
type Provider interface {
	// GenerateStrategicRoadmap generates the high-level roadmap with phases and milestones
	GenerateStrategicRoadmap(ctx context.Context, goalReq types.GoalRequest) (*types.Roadmap, error)

	// GenerateMilestoneTasks generates tasks for a specific milestone
	GenerateMilestoneTasks(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone) ([]types.Task, error)

	// GenerateNextMilestoneFromReview generates the next milestone based on user review
	GenerateNextMilestoneFromReview(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone, review types.MilestoneReviewAnswers) (*types.MilestoneAdjustments, []types.Task, error)
}

// ProviderType identifies which AI provider to use
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
)

// OpenAIProvider wraps the existing OpenAI functions to implement Provider
type OpenAIProvider struct{}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{}
}

func (p *OpenAIProvider) GenerateStrategicRoadmap(ctx context.Context, goalReq types.GoalRequest) (*types.Roadmap, error) {
	return GenerateStrategicRoadmapFromGoalRequest(ctx, goalReq)
}

func (p *OpenAIProvider) GenerateMilestoneTasks(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone) ([]types.Task, error) {
	return GenerateMilestoneTasksFromGoalRequest(ctx, goalReq, milestone)
}

func (p *OpenAIProvider) GenerateNextMilestoneFromReview(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone, review types.MilestoneReviewAnswers) (*types.MilestoneAdjustments, []types.Task, error) {
	return GenerateNextMilestoneFromReview(ctx, goalReq, milestone, review)
}

// GetProvider returns the appropriate provider based on the type
func GetProvider(providerType ProviderType) Provider {
	switch providerType {
	case ProviderAnthropic:
		return anthropic.NewProvider()
	case ProviderOpenAI:
		fallthrough
	default:
		return NewOpenAIProvider()
	}
}

// DefaultProvider returns the default provider based on environment configuration.
// Set AI_PROVIDER=anthropic to use Anthropic, otherwise defaults to OpenAI.
func DefaultProvider() Provider {
	if os.Getenv("AI_PROVIDER") == "anthropic" {
		return anthropic.NewProvider()
	}
	return NewOpenAIProvider()
}
