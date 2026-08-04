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
	// Name returns a short identifier for the backend ("openai", "anthropic").
	// Used for observability so logs/dashboards show which provider ran a job.
	Name() string

	// GenerateStrategicRoadmap generates the high-level roadmap with phases and milestones
	GenerateStrategicRoadmap(ctx context.Context, goalReq types.GoalRequest) (*types.Roadmap, error)

	// GenerateMilestoneTasks generates tasks for a specific milestone
	GenerateMilestoneTasks(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone) ([]types.Task, error)

	// GenerateNextMilestoneFromReview generates the next milestone based on user review
	GenerateNextMilestoneFromReview(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone, review types.MilestoneReviewAnswers) (*types.MilestoneAdjustments, []types.Task, error)

	// GenerateGoalOptions produces candidate goals for a user who does not yet
	// know what to pursue (form-based goal discovery). The raw result is not
	// trusted: callers go through GenerateValidatedGoalOptions, which enforces
	// the structural and coherence rules and bounds the repair retries.
	GenerateGoalOptions(ctx context.Context, in types.GoalOptionsInput) (*types.GoalOptionsOutput, error)
}

// ProviderType identifies which AI provider to use
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	// ProviderMock is the deterministic offline backend. Local development and
	// tests only — DefaultProvider refuses it on a hosted runtime.
	ProviderMock ProviderType = "mock"
)

// OpenAIProvider wraps the existing OpenAI functions to implement Provider
type OpenAIProvider struct{}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{}
}

func (p *OpenAIProvider) Name() string { return string(ProviderOpenAI) }

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
	case ProviderMock:
		return NewMockProvider()
	case ProviderOpenAI:
		fallthrough
	default:
		return NewOpenAIProvider()
	}
}

// DefaultProvider returns the default provider based on environment configuration.
// Set AI_PROVIDER=anthropic to use Anthropic, otherwise defaults to OpenAI.
//
// AI_PROVIDER=mock selects the deterministic offline backend for local
// development. That value is refused on a hosted runtime: a deploy configured
// with it panics during router construction rather than silently serving
// fabricated plans to real users. An unrecognised value panics for the same
// reason — quietly falling back to OpenAI turned a typo in a deploy config into
// traffic on a backend nobody chose.
//
// This resolves SELECTION only. Key presence is a startup concern and belongs
// to ValidateProviderConfig, which main() calls before serving; keeping it out
// of here is what lets the handler tests construct a router without a real key.
func DefaultProvider() Provider {
	provider, err := ResolveProviderType(os.Getenv("AI_PROVIDER"))
	if err != nil {
		panic(err.Error())
	}

	switch provider {
	case ProviderAnthropic:
		return anthropic.NewProvider()
	case ProviderMock:
		if isHostedRuntime() {
			panic("AI_PROVIDER=mock is refused on a hosted runtime: the mock backend returns fabricated plans and must never serve real users")
		}
		return NewMockProvider()
	default:
		return NewOpenAIProvider()
	}
}
