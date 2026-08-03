package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dr/server/types"
)

// GenerateValidatedGoalOptions is the guard between a model and the user: it
// must never let an incoherent option set through, must repair at most once,
// and must mint the option ids itself. These tests drive it with a scripted
// provider so no network or API key is involved.

// scriptedProvider returns one queued result per call and records the inputs it
// was given, so a test can assert what the repair retry was actually told.
type scriptedProvider struct {
	results []*types.GoalOptionsOutput
	errs    []error
	calls   []types.GoalOptionsInput
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) GenerateGoalOptions(_ context.Context, in types.GoalOptionsInput) (*types.GoalOptionsOutput, error) {
	idx := len(p.calls)
	p.calls = append(p.calls, in)
	if idx < len(p.errs) && p.errs[idx] != nil {
		return nil, p.errs[idx]
	}
	if idx < len(p.results) {
		return p.results[idx], nil
	}
	return nil, errors.New("scriptedProvider: no result queued")
}

// The remaining Provider methods are unused here; goal discovery never calls them.
func (p *scriptedProvider) GenerateStrategicRoadmap(context.Context, types.GoalRequest) (*types.Roadmap, error) {
	return nil, errors.New("unused")
}
func (p *scriptedProvider) GenerateMilestoneTasks(context.Context, types.GoalRequest, *types.Milestone) ([]types.Task, error) {
	return nil, errors.New("unused")
}
func (p *scriptedProvider) GenerateNextMilestoneFromReview(context.Context, types.GoalRequest, *types.Milestone, types.MilestoneReviewAnswers) (*types.MilestoneAdjustments, []types.Task, error) {
	return nil, nil, errors.New("unused")
}

func goodEfforts() []types.GoalOptionEffort {
	return []types.GoalOptionEffort{
		{Pace: types.PaceFocused, HoursPerWeekMin: 8, HoursPerWeekMax: 10, EstimatedDurationMonthsMin: 2, EstimatedDurationMonthsMax: 3, DisplayText: "8-10 h/wk, 2-3 months"},
		{Pace: types.PaceSteady, HoursPerWeekMin: 4, HoursPerWeekMax: 6, EstimatedDurationMonthsMin: 4, EstimatedDurationMonthsMax: 5, DisplayText: "4-6 h/wk, 4-5 months"},
		{Pace: types.PaceLight, HoursPerWeekMin: 2, HoursPerWeekMax: 3, EstimatedDurationMonthsMin: 7, EstimatedDurationMonthsMax: 9, DisplayText: "2-3 h/wk, 7-9 months"},
	}
}

func goodOptions() []types.GoalOption {
	titles := []string{"Launch a paid landing page", "Publish a weekly newsletter", "Pass a cloud certification"}
	out := make([]types.GoalOption, 0, 3)
	for i, title := range titles {
		out = append(out, types.GoalOption{
			Rank:             i + 1,
			GoalTitle:        title,
			WhyThisFits:      "Matches the areas and outcomes you chose.",
			TargetOutcome:    "A concrete, checkable result exists.",
			WhyWorthPursuing: "It converts intent into evidence.",
			EffortExamples:   goodEfforts(),
			Difficulty:       types.GoalOptionDifficulty{Level: types.DifficultyModerate, Explanation: "Fits your starting point."},
			AccountabilityStrength: types.GoalOptionAccountability{
				Level: types.AccountabilityModerate, Explanation: "Needs a weekly check-in.",
			},
		})
	}
	return out
}

func sampleInput() types.GoalOptionsInput {
	return types.GoalOptionsInput{Answers: types.GoalDiscoveryAnswers{
		SchemaVersion:     types.GoalDiscoverySchemaVersion,
		Trigger:           types.TriggerFeelStuck,
		Areas:             []types.GoalDiscoveryArea{types.AreaCareer},
		SelfDescription:   types.SelfNeedAWin,
		RecurringThought:  "Starting something of my own",
		SuccessDefinition: "Something real shipped",
		Outcomes:          []types.GoalDiscoveryOutcome{types.OutcomeCreateIncome},
		TargetType:        types.TargetSkillProject,
	}}
}

func TestGenerateValidatedGoalOptions_ReturnsThreeValidatedOptions(t *testing.T) {
	provider := &scriptedProvider{results: []*types.GoalOptionsOutput{{Options: goodOptions()}}}

	out, err := GenerateValidatedGoalOptions(context.Background(), provider, sampleInput())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(out.Options) != types.GoalDiscoveryOptionCount {
		t.Fatalf("expected %d options, got %d", types.GoalDiscoveryOptionCount, len(out.Options))
	}
	if len(provider.calls) != 1 {
		t.Fatalf("a valid first response must not be retried; got %d calls", len(provider.calls))
	}
}

// Ids are the server's proof that stage 2's selection is real, so they must be
// minted here and be unique, regardless of what the model returned.
func TestGenerateValidatedGoalOptions_MintsUniqueServerOwnedIDs(t *testing.T) {
	options := goodOptions()
	options[0].ID = "model-supplied"
	provider := &scriptedProvider{results: []*types.GoalOptionsOutput{{Options: options}}}

	out, err := GenerateValidatedGoalOptions(context.Background(), provider, sampleInput())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	seen := map[string]bool{}
	for _, opt := range out.Options {
		if opt.ID == "" || opt.ID == "model-supplied" {
			t.Fatalf("expected a server-minted id, got %q", opt.ID)
		}
		if seen[opt.ID] {
			t.Fatalf("duplicate option id %q", opt.ID)
		}
		seen[opt.ID] = true
	}
}

// One bad response gets exactly one repair attempt, and that attempt is told
// what was wrong rather than just being asked again.
func TestGenerateValidatedGoalOptions_RepairsOnceWithSpecificFeedback(t *testing.T) {
	broken := goodOptions()
	broken[2].Rank = 1 // duplicate rank

	provider := &scriptedProvider{results: []*types.GoalOptionsOutput{
		{Options: broken},
		{Options: goodOptions()},
	}}

	out, err := GenerateValidatedGoalOptions(context.Background(), provider, sampleInput())
	if err != nil {
		t.Fatalf("expected the repair attempt to succeed, got %v", err)
	}
	if len(out.Options) != types.GoalDiscoveryOptionCount {
		t.Fatalf("expected %d options, got %d", types.GoalDiscoveryOptionCount, len(out.Options))
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected exactly one repair attempt, got %d calls", len(provider.calls))
	}
	if provider.calls[0].RepairHint != "" {
		t.Fatal("the first attempt must not carry a repair hint")
	}
	if !strings.Contains(provider.calls[1].RepairHint, "duplicate rank") {
		t.Fatalf("repair attempt was not told what was wrong: %q", provider.calls[1].RepairHint)
	}
}

// The repair loop is bounded: a persistently broken model gets one retry, then
// a typed terminal failure — it does not loop burning tokens.
func TestGenerateValidatedGoalOptions_BoundedRetriesThenTypedFailure(t *testing.T) {
	broken := goodOptions()[:1] // wrong count, unfixable by the script

	provider := &scriptedProvider{results: []*types.GoalOptionsOutput{
		{Options: broken}, {Options: broken}, {Options: broken},
	}}

	_, err := GenerateValidatedGoalOptions(context.Background(), provider, sampleInput())
	if err == nil {
		t.Fatal("expected a terminal failure")
	}
	if !errors.Is(err, ErrGoalOptionsInvalid) {
		t.Fatalf("expected ErrGoalOptionsInvalid, got %v", err)
	}
	if len(provider.calls) != goalOptionsRepairAttempts+1 {
		t.Fatalf("expected %d attempts, got %d", goalOptionsRepairAttempts+1, len(provider.calls))
	}
}

// A provider/transport error is not a schema problem, so re-prompting is
// pointless — it must fail immediately rather than spend a second call.
func TestGenerateValidatedGoalOptions_ProviderErrorIsNotRetried(t *testing.T) {
	provider := &scriptedProvider{errs: []error{errors.New("upstream 503")}}

	_, err := GenerateValidatedGoalOptions(context.Background(), provider, sampleInput())
	if err == nil {
		t.Fatal("expected the provider error to surface")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("a provider error must not be retried; got %d calls", len(provider.calls))
	}
}

// A safety block is terminal: no validation, no retry asking the model to
// reconsider its refusal.
func TestGenerateValidatedGoalOptions_SafetyBlockIsTerminalSuccess(t *testing.T) {
	provider := &scriptedProvider{results: []*types.GoalOptionsOutput{
		{Safety: &types.GoalOptionsSafety{Blocked: true, Reason: "crisis", Message: types.GoalOptionsSafetyMessage}},
	}}

	out, err := GenerateValidatedGoalOptions(context.Background(), provider, sampleInput())
	if err != nil {
		t.Fatalf("a safety block is not an error, got %v", err)
	}
	if out.Safety == nil || !out.Safety.Blocked {
		t.Fatal("expected the safety block to be preserved")
	}
	if len(out.Options) != 0 {
		t.Fatal("a safety block must not carry goal options")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("a safety block must not be retried; got %d calls", len(provider.calls))
	}
}

// The exclusion list is what makes "Show me different options" produce
// genuinely different options, so it has to reach the provider.
func TestGenerateValidatedGoalOptions_PassesExclusionsThrough(t *testing.T) {
	provider := &scriptedProvider{results: []*types.GoalOptionsOutput{{Options: goodOptions()}}}
	in := sampleInput()
	in.Exclude = []string{"Old goal A", "Old goal B"}

	if _, err := GenerateValidatedGoalOptions(context.Background(), provider, in); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(provider.calls[0].Exclude) != 2 {
		t.Fatalf("exclusions did not reach the provider: %#v", provider.calls[0].Exclude)
	}
}

// ─── mock provider ───────────────────────────────────────────────────────────

// The offline provider exists so local runs exercise the real async path. If its
// output could not pass ValidateGoalOptions, that exercise would prove nothing.
func TestMockProvider_OutputPassesTheRealValidation(t *testing.T) {
	out, err := NewMockProvider().GenerateGoalOptions(context.Background(), sampleInput())
	if err != nil {
		t.Fatalf("mock provider failed: %v", err)
	}
	if err := types.ValidateGoalOptions(out.Options); err != nil {
		t.Fatalf("mock output must satisfy the same validation as a real provider: %v", err)
	}
}

// A regeneration must not hand back the same three goals.
func TestMockProvider_RegenerationReturnsDifferentTitles(t *testing.T) {
	provider := NewMockProvider()
	ctx := context.Background()

	first, err := provider.GenerateGoalOptions(ctx, sampleInput())
	if err != nil {
		t.Fatalf("first generation failed: %v", err)
	}

	in := sampleInput()
	for _, opt := range first.Options {
		in.Exclude = append(in.Exclude, opt.GoalTitle)
	}
	second, err := provider.GenerateGoalOptions(ctx, in)
	if err != nil {
		t.Fatalf("regeneration failed: %v", err)
	}

	previous := map[string]bool{}
	for _, opt := range first.Options {
		previous[opt.GoalTitle] = true
	}
	for _, opt := range second.Options {
		if previous[opt.GoalTitle] {
			t.Fatalf("regeneration repeated a rejected goal: %q", opt.GoalTitle)
		}
	}
	if err := types.ValidateGoalOptions(second.Options); err != nil {
		t.Fatalf("regenerated set must also validate: %v", err)
	}
}

// Regenerating twice must keep moving. Keying the variant off the NUMBER of
// exclusions would fail this: every regeneration excludes exactly three titles,
// so the second one would hand back the first replacement again.
func TestMockProvider_SecondRegenerationDiffersFromTheFirst(t *testing.T) {
	provider := NewMockProvider()
	ctx := context.Background()

	first, err := provider.GenerateGoalOptions(ctx, sampleInput())
	if err != nil {
		t.Fatalf("first generation failed: %v", err)
	}

	titles := func(out *types.GoalOptionsOutput) []string {
		names := make([]string, 0, len(out.Options))
		for _, opt := range out.Options {
			names = append(names, opt.GoalTitle)
		}
		return names
	}

	secondIn := sampleInput()
	secondIn.Exclude = titles(first)
	second, err := provider.GenerateGoalOptions(ctx, secondIn)
	if err != nil {
		t.Fatalf("first regeneration failed: %v", err)
	}

	thirdIn := sampleInput()
	thirdIn.Exclude = titles(second)
	third, err := provider.GenerateGoalOptions(ctx, thirdIn)
	if err != nil {
		t.Fatalf("second regeneration failed: %v", err)
	}

	secondTitles := titles(second)
	for _, title := range titles(third) {
		for _, prior := range secondTitles {
			if title == prior {
				t.Fatalf("second regeneration repeated %q from the previous set", title)
			}
		}
	}
	if err := types.ValidateGoalOptions(third.Options); err != nil {
		t.Fatalf("second regenerated set must also validate: %v", err)
	}
}

// Same exclusions in, same options out — the offline provider must stay
// deterministic so a local run is reproducible.
func TestMockProvider_IsDeterministic(t *testing.T) {
	provider := NewMockProvider()
	in := sampleInput()
	in.Exclude = []string{"Old goal B", "Old goal A"}

	first, err := provider.GenerateGoalOptions(context.Background(), in)
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}
	second, err := provider.GenerateGoalOptions(context.Background(), in)
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}
	if first.Options[0].GoalTitle != second.Options[0].GoalTitle {
		t.Fatal("identical input produced different options")
	}
}

// The deterministic failure marker backs the local check that a failed
// regeneration leaves the previous options intact.
func TestMockProvider_FailureMarkerProducesAnError(t *testing.T) {
	in := sampleInput()
	in.Answers.RecurringThought = "please " + mockFailureMarker + " here"

	if _, err := NewMockProvider().GenerateGoalOptions(context.Background(), in); err == nil {
		t.Fatal("expected the deterministic failure marker to produce an error")
	}
}

func TestMockProvider_CrisisMarkerProducesSafetyBlock(t *testing.T) {
	in := sampleInput()
	in.Answers.RecurringThought = mockCrisisMarkers[0]

	out, err := NewMockProvider().GenerateGoalOptions(context.Background(), in)
	if err != nil {
		t.Fatalf("expected a safety block, not an error: %v", err)
	}
	if out.Safety == nil || !out.Safety.Blocked {
		t.Fatal("expected a blocked safety result")
	}
}

// ─── provider selection ──────────────────────────────────────────────────────

func TestDefaultProvider_SelectsMockOnlyOffHostedRuntime(t *testing.T) {
	t.Setenv("AI_PROVIDER", string(ProviderMock))
	t.Setenv("FLY_APP_NAME", "")

	if got := DefaultProvider().Name(); got != string(ProviderMock) {
		t.Fatalf("expected the mock provider locally, got %q", got)
	}
}

// A deploy configured with the mock backend must fail loudly, not quietly serve
// fabricated plans to real users.
func TestDefaultProvider_RefusesMockOnHostedRuntime(t *testing.T) {
	t.Setenv("AI_PROVIDER", string(ProviderMock))
	t.Setenv("FLY_APP_NAME", "drserver-purple-moon")

	defer func() {
		if recover() == nil {
			t.Fatal("expected AI_PROVIDER=mock to be refused on a hosted runtime")
		}
	}()
	_ = DefaultProvider()
}

func TestDefaultProvider_DefaultsToOpenAI(t *testing.T) {
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("FLY_APP_NAME", "")

	if got := DefaultProvider().Name(); got != string(ProviderOpenAI) {
		t.Fatalf("expected the OpenAI provider by default, got %q", got)
	}
}
