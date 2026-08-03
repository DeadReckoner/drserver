package types

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

/******************************************************************************
 * GOAL DISCOVERY
 *
 * Form-based goal discovery: a user who does not yet know WHICH goal to pursue
 * answers a structured questionnaire (stage 1), the model returns three ranked
 * goal options, the user picks one, fills in roadmap details (stage 2), and the
 * result is fed into the EXISTING roadmap generator.
 *
 * Everything the client sends is a stable machine-readable enum; display copy
 * lives in the mobile translation files. Free text is bounded and trimmed.
 ******************************************************************************/

// GoalDiscoverySchemaVersion is the contract version for the stage-1 answers and
// the stage-2 roadmap payload. Clients send it so the server can reject (or
// later migrate) drafts written by an incompatible app build.
const GoalDiscoverySchemaVersion = 1

// GoalDiscoverySource marks a roadmap request as originating from goal
// discovery rather than the known-goal/chat flow. Sent on GoalRequest.Source,
// which is additive and empty for every existing client.
const GoalDiscoverySource = "goal_discovery"

// Free-text bounds. Mirrored by GoalDiscoveryLimits in
// drmobile/src/hooks/goalDiscovery/goalDiscovery.types.ts — change both together.
const (
	GoalDiscoveryShortTextMax  = 500  // optional one-liners ("Anything else?", custom area/outcome)
	GoalDiscoveryTitleMax      = 300  // goal title
	GoalDiscoveryLongTextMax   = 2000 // standard long-form answers
	GoalDiscoveryDetailTextMax = 3000 // baseline / combined constraint detail
	GoalDiscoveryCustomPaceMax = 250  // "Describe your weekly commitment"

	// GoalDiscoveryMaxAreas / MaxOutcomes count a non-empty custom entry as one
	// selection, matching the mobile UI rule.
	GoalDiscoveryMaxAreas    = 3
	GoalDiscoveryMaxOutcomes = 2

	// GoalDiscoveryOptionCount is the exact number of options a generation must
	// produce. Not a maximum — fewer or more is a schema violation.
	GoalDiscoveryOptionCount = 3
)

// ─── Stage 1 enums ───────────────────────────────────────────────────────────

type GoalDiscoveryTrigger string

const (
	TriggerFeelStuck           GoalDiscoveryTrigger = "feel_stuck"
	TriggerWantDiscipline      GoalDiscoveryTrigger = "want_more_discipline"
	TriggerWantToBuild         GoalDiscoveryTrigger = "want_to_build_something"
	TriggerWantHealthier       GoalDiscoveryTrigger = "want_to_get_healthier"
	TriggerWantMoreMoney       GoalDiscoveryTrigger = "want_to_make_more_money"
	TriggerWantNewHobby        GoalDiscoveryTrigger = "want_a_new_hobby"
	TriggerWantToFeelProud     GoalDiscoveryTrigger = "want_to_feel_proud"
	TriggerSomethingMustChange GoalDiscoveryTrigger = "something_needs_to_change"
)

var goalDiscoveryTriggers = map[GoalDiscoveryTrigger]bool{
	TriggerFeelStuck: true, TriggerWantDiscipline: true, TriggerWantToBuild: true,
	TriggerWantHealthier: true, TriggerWantMoreMoney: true, TriggerWantNewHobby: true,
	TriggerWantToFeelProud: true, TriggerSomethingMustChange: true,
}

type GoalDiscoveryArea string

const (
	AreaCareer          GoalDiscoveryArea = "career"
	AreaBusiness        GoalDiscoveryArea = "business"
	AreaMoney           GoalDiscoveryArea = "money"
	AreaLearning        GoalDiscoveryArea = "learning"
	AreaCreativity      GoalDiscoveryArea = "creativity"
	AreaRelationships   GoalDiscoveryArea = "relationships"
	AreaHealth          GoalDiscoveryArea = "health"
	AreaHomeEnvironment GoalDiscoveryArea = "home_environment"
	AreaConfidence      GoalDiscoveryArea = "confidence"
	AreaDiscipline      GoalDiscoveryArea = "discipline"
	AreaAdventureFun    GoalDiscoveryArea = "adventure_fun"
	AreaPurpose         GoalDiscoveryArea = "purpose_direction"
)

var goalDiscoveryAreas = map[GoalDiscoveryArea]bool{
	AreaCareer: true, AreaBusiness: true, AreaMoney: true, AreaLearning: true,
	AreaCreativity: true, AreaRelationships: true, AreaHealth: true,
	AreaHomeEnvironment: true, AreaConfidence: true, AreaDiscipline: true,
	AreaAdventureFun: true, AreaPurpose: true,
}

type GoalDiscoverySelfDescription string

const (
	SelfKnowButDontDo        GoalDiscoverySelfDescription = "know_but_dont_do"
	SelfTooManyIdeas         GoalDiscoverySelfDescription = "too_many_ideas"
	SelfDontKnowWhereToStart GoalDiscoverySelfDescription = "dont_know_where_to_start"
	SelfStartAndQuit         GoalDiscoverySelfDescription = "start_and_quit"
	SelfWantHarderTarget     GoalDiscoverySelfDescription = "fine_but_want_harder_target"
	SelfNeedAWin             GoalDiscoverySelfDescription = "need_a_win"
)

var goalDiscoverySelfDescriptions = map[GoalDiscoverySelfDescription]bool{
	SelfKnowButDontDo: true, SelfTooManyIdeas: true, SelfDontKnowWhereToStart: true,
	SelfStartAndQuit: true, SelfWantHarderTarget: true, SelfNeedAWin: true,
}

type GoalDiscoveryOutcome string

const (
	OutcomeBuildConfidence    GoalDiscoveryOutcome = "build_confidence"
	OutcomeCreateIncome       GoalDiscoveryOutcome = "create_income"
	OutcomeReduceStress       GoalDiscoveryOutcome = "reduce_stress"
	OutcomeBuildDiscipline    GoalDiscoveryOutcome = "build_discipline"
	OutcomeLearnSkill         GoalDiscoveryOutcome = "learn_useful_skill"
	OutcomeCreateTangible     GoalDiscoveryOutcome = "create_something_tangible"
	OutcomeMoreInteresting    GoalDiscoveryOutcome = "become_more_interesting"
	OutcomeFixIgnoredProblem  GoalDiscoveryOutcome = "fix_ignored_problem"
	OutcomeProveFollowThrough GoalDiscoveryOutcome = "prove_follow_through"
	OutcomeImproveHealth      GoalDiscoveryOutcome = "improve_health"
)

var goalDiscoveryOutcomes = map[GoalDiscoveryOutcome]bool{
	OutcomeBuildConfidence: true, OutcomeCreateIncome: true, OutcomeReduceStress: true,
	OutcomeBuildDiscipline: true, OutcomeLearnSkill: true, OutcomeCreateTangible: true,
	OutcomeMoreInteresting: true, OutcomeFixIgnoredProblem: true,
	OutcomeProveFollowThrough: true, OutcomeImproveHealth: true,
}

type GoalDiscoveryTargetType string

const (
	TargetEasyWin          GoalDiscoveryTargetType = "easy_win"
	TargetSteadyHabit      GoalDiscoveryTargetType = "steady_habit"
	TargetSeriousChallenge GoalDiscoveryTargetType = "serious_challenge"
	TargetSkillProject     GoalDiscoveryTargetType = "skill_building_project"
	TargetMoneyProject     GoalDiscoveryTargetType = "money_making_project"
	TargetLifeCleanup      GoalDiscoveryTargetType = "life_cleanup"
	TargetNewExperience    GoalDiscoveryTargetType = "new_experience"
	TargetOpenToBestFit    GoalDiscoveryTargetType = "open_to_best_fit"
)

var goalDiscoveryTargetTypes = map[GoalDiscoveryTargetType]bool{
	TargetEasyWin: true, TargetSteadyHabit: true, TargetSeriousChallenge: true,
	TargetSkillProject: true, TargetMoneyProject: true, TargetLifeCleanup: true,
	TargetNewExperience: true, TargetOpenToBestFit: true,
}

// GoalDiscoveryAnswers is the normalized stage-1 questionnaire. This is what the
// mobile client submits, what gets stored on the background job payload, and
// what the goal-options prompt is built from.
type GoalDiscoveryAnswers struct {
	SchemaVersion int `json:"schema_version"`

	Trigger     GoalDiscoveryTrigger `json:"trigger"`
	TriggerNote string               `json:"trigger_note,omitempty"`

	Areas      []GoalDiscoveryArea `json:"areas"`
	CustomArea string              `json:"custom_area,omitempty"`

	SelfDescription GoalDiscoverySelfDescription `json:"self_description"`

	RecurringThought  string `json:"recurring_thought"`
	SuccessDefinition string `json:"success_definition"`

	Outcomes      []GoalDiscoveryOutcome `json:"outcomes"`
	CustomOutcome string                 `json:"custom_outcome,omitempty"`

	TargetType GoalDiscoveryTargetType `json:"target_type"`
}

// Normalize trims every free-text field in place. Called before validation so
// whitespace-only answers are rejected rather than stored.
func (a *GoalDiscoveryAnswers) Normalize() {
	a.TriggerNote = strings.TrimSpace(a.TriggerNote)
	a.CustomArea = strings.TrimSpace(a.CustomArea)
	a.RecurringThought = strings.TrimSpace(a.RecurringThought)
	a.SuccessDefinition = strings.TrimSpace(a.SuccessDefinition)
	a.CustomOutcome = strings.TrimSpace(a.CustomOutcome)
}

// AreaSelectionCount counts enum areas plus a non-empty custom area, matching
// the mobile maximum-of-three rule. Call after Normalize.
func (a GoalDiscoveryAnswers) AreaSelectionCount() int {
	n := len(a.Areas)
	if a.CustomArea != "" {
		n++
	}
	return n
}

// OutcomeSelectionCount counts enum outcomes plus a non-empty custom outcome.
func (a GoalDiscoveryAnswers) OutcomeSelectionCount() int {
	n := len(a.Outcomes)
	if a.CustomOutcome != "" {
		n++
	}
	return n
}

// Validate reports the first problem with the answers, or nil. The message is
// safe to return to the client: it names the field, never echoes user content.
func (a GoalDiscoveryAnswers) Validate() error {
	if a.SchemaVersion != GoalDiscoverySchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (expected %d)", a.SchemaVersion, GoalDiscoverySchemaVersion)
	}

	if !goalDiscoveryTriggers[a.Trigger] {
		return fmt.Errorf("trigger is required and must be a known value")
	}
	if err := boundOptional("trigger_note", a.TriggerNote, GoalDiscoveryShortTextMax); err != nil {
		return err
	}

	if a.AreaSelectionCount() == 0 {
		return fmt.Errorf("at least one area is required")
	}
	if a.AreaSelectionCount() > GoalDiscoveryMaxAreas {
		return fmt.Errorf("at most %d areas may be selected (a custom area counts as one)", GoalDiscoveryMaxAreas)
	}
	seenArea := map[GoalDiscoveryArea]bool{}
	for _, area := range a.Areas {
		if !goalDiscoveryAreas[area] {
			return fmt.Errorf("unknown area value")
		}
		if seenArea[area] {
			return fmt.Errorf("duplicate area value")
		}
		seenArea[area] = true
	}
	if err := boundOptional("custom_area", a.CustomArea, GoalDiscoveryShortTextMax); err != nil {
		return err
	}

	if !goalDiscoverySelfDescriptions[a.SelfDescription] {
		return fmt.Errorf("self_description is required and must be a known value")
	}

	if err := requireBounded("recurring_thought", a.RecurringThought, GoalDiscoveryLongTextMax); err != nil {
		return err
	}
	if err := requireBounded("success_definition", a.SuccessDefinition, GoalDiscoveryLongTextMax); err != nil {
		return err
	}

	if a.OutcomeSelectionCount() == 0 {
		return fmt.Errorf("at least one desired outcome is required")
	}
	if a.OutcomeSelectionCount() > GoalDiscoveryMaxOutcomes {
		return fmt.Errorf("at most %d outcomes may be selected (a custom outcome counts as one)", GoalDiscoveryMaxOutcomes)
	}
	seenOutcome := map[GoalDiscoveryOutcome]bool{}
	for _, outcome := range a.Outcomes {
		if !goalDiscoveryOutcomes[outcome] {
			return fmt.Errorf("unknown outcome value")
		}
		if seenOutcome[outcome] {
			return fmt.Errorf("duplicate outcome value")
		}
		seenOutcome[outcome] = true
	}
	if err := boundOptional("custom_outcome", a.CustomOutcome, GoalDiscoveryShortTextMax); err != nil {
		return err
	}

	if !goalDiscoveryTargetTypes[a.TargetType] {
		return fmt.Errorf("target_type is required and must be a known value")
	}

	return nil
}

// ─── Generated goal options ──────────────────────────────────────────────────

type GoalOptionPace string

const (
	PaceFocused GoalOptionPace = "focused"
	PaceSteady  GoalOptionPace = "steady"
	PaceLight   GoalOptionPace = "light"
)

// GoalOptionPaceOrder is focused → steady → light, i.e. descending intensity.
// Validation uses it to check that weekly hours never increase and timelines
// never shrink as the pace gets lighter.
var GoalOptionPaceOrder = []GoalOptionPace{PaceFocused, PaceSteady, PaceLight}

type GoalOptionDifficultyLevel string

const (
	DifficultyStarter   GoalOptionDifficultyLevel = "starter"
	DifficultyModerate  GoalOptionDifficultyLevel = "moderate"
	DifficultyDemanding GoalOptionDifficultyLevel = "demanding"
	DifficultyIntense   GoalOptionDifficultyLevel = "intense"
)

var goalOptionDifficulties = map[GoalOptionDifficultyLevel]bool{
	DifficultyStarter: true, DifficultyModerate: true,
	DifficultyDemanding: true, DifficultyIntense: true,
}

type GoalOptionAccountabilityLevel string

const (
	AccountabilityLow      GoalOptionAccountabilityLevel = "low"
	AccountabilityModerate GoalOptionAccountabilityLevel = "moderate"
	AccountabilityHigh     GoalOptionAccountabilityLevel = "high"
)

var goalOptionAccountability = map[GoalOptionAccountabilityLevel]bool{
	AccountabilityLow: true, AccountabilityModerate: true, AccountabilityHigh: true,
}

// GoalOptionEffort is one pace lane for an option. Hours and months are ranges
// so the model can express uncertainty without the client parsing prose;
// DisplayText is the human sentence the card shows.
type GoalOptionEffort struct {
	Pace                       GoalOptionPace `json:"pace"`
	HoursPerWeekMin            float64        `json:"hours_per_week_min"`
	HoursPerWeekMax            float64        `json:"hours_per_week_max"`
	EstimatedDurationMonthsMin float64        `json:"estimated_duration_months_min"`
	EstimatedDurationMonthsMax float64        `json:"estimated_duration_months_max"`
	DisplayText                string         `json:"display_text"`
}

type GoalOptionDifficulty struct {
	Level       GoalOptionDifficultyLevel `json:"level"`
	Explanation string                    `json:"explanation"`
}

type GoalOptionAccountability struct {
	Level       GoalOptionAccountabilityLevel `json:"level"`
	Explanation string                        `json:"explanation"`
}

// GoalOption is one generated candidate goal. IDs are assigned server-side after
// validation — a model-supplied id is never trusted, because the id is what
// stage 2 uses to prove the user picked something the server actually produced.
type GoalOption struct {
	ID                     string                   `json:"id"`
	Rank                   int                      `json:"rank"`
	GoalTitle              string                   `json:"goal_title"`
	WhyThisFits            string                   `json:"why_this_fits"`
	TargetOutcome          string                   `json:"target_outcome"`
	WhyWorthPursuing       string                   `json:"why_worth_pursuing"`
	EffortExamples         []GoalOptionEffort       `json:"effort_examples"`
	Difficulty             GoalOptionDifficulty     `json:"difficulty"`
	AccountabilityStrength GoalOptionAccountability `json:"accountability_strength"`
}

// EffortFor returns the effort lane for a pace, or false when absent.
func (o GoalOption) EffortFor(pace GoalOptionPace) (GoalOptionEffort, bool) {
	for _, e := range o.EffortExamples {
		if e.Pace == pace {
			return e, true
		}
	}
	return GoalOptionEffort{}, false
}

// GoalOptionsResult is the JSONB stored on a completed goal_discovery job and
// returned through the existing /jobs/:jobid/status endpoint.
type GoalOptionsResult struct {
	Options       []GoalOption `json:"options"`
	PromptVersion string       `json:"prompt_version"`
	GeneratedAt   time.Time    `json:"generated_at"`

	// Safety is set instead of Options when the model flags crisis or
	// imminent-harm content in the user's answers. Options is empty in that case
	// and the client shows the support message rather than goal cards.
	Safety *GoalOptionsSafety `json:"safety,omitempty"`
}

// GoalOptionsSafety is the typed safety outcome. The message is non-clinical and
// deliberately does not attempt diagnosis or crisis counselling.
type GoalOptionsSafety struct {
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// GoalOptionsInput is everything a provider needs to generate one set of goal
// options. Exclude carries the titles of options the user already rejected, so a
// regeneration explores a different angle instead of reshuffling the same ideas.
// RepairHint is set only by the bounded retry after a failed validation.
//
// Lives here rather than in the agent package for the same reason GoalRequest
// does: every Provider method signature is expressed in types, which is what
// lets the anthropic package implement the interface without an import cycle.
type GoalOptionsInput struct {
	Answers    GoalDiscoveryAnswers
	Exclude    []string
	RepairHint string
}

// GoalOptionsOutput is a provider's raw, still-unvalidated result. Safety is set
// instead of Options when the model flags crisis or imminent-harm content.
type GoalOptionsOutput struct {
	Options []GoalOption
	Safety  *GoalOptionsSafety
}

// GoalOptionsSafetyMessage is the copy shown when generation is safety-blocked.
const GoalOptionsSafetyMessage = "We can't turn this into a goal plan. Dead Reckoner is a goal-setting app, not a support service. " +
	"If you're going through something serious, please reach out to someone you trust or a local support line."

// ValidateGoalOptions enforces every structural and coherence rule the product
// requires of a generated option set. Returns a single error listing all
// violations so a repair retry can be given specific feedback.
//
// Rules: exactly three options; ranks are exactly 1/2/3 with no duplicates;
// titles are distinct; each option carries exactly one focused, one steady and
// one light effort lane; weekly hours never increase as the pace gets lighter;
// and a lighter pace never finishes sooner.
func ValidateGoalOptions(options []GoalOption) error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if len(options) != GoalDiscoveryOptionCount {
		add("expected exactly %d options, got %d", GoalDiscoveryOptionCount, len(options))
	}

	seenRank := map[int]bool{}
	seenTitle := map[string]bool{}

	for i, opt := range options {
		label := fmt.Sprintf("option %d", i+1)

		if opt.Rank < 1 || opt.Rank > GoalDiscoveryOptionCount {
			add("%s: rank must be 1, 2 or 3", label)
		} else if seenRank[opt.Rank] {
			add("%s: duplicate rank %d", label, opt.Rank)
		} else {
			seenRank[opt.Rank] = true
		}

		title := strings.TrimSpace(opt.GoalTitle)
		switch {
		case title == "":
			add("%s: goal_title is empty", label)
		case len(title) > GoalDiscoveryTitleMax:
			add("%s: goal_title exceeds %d characters", label, GoalDiscoveryTitleMax)
		default:
			key := strings.ToLower(strings.Join(strings.Fields(title), " "))
			if seenTitle[key] {
				add("%s: goal_title duplicates another option", label)
			}
			seenTitle[key] = true
		}

		for field, value := range map[string]string{
			"why_this_fits":      opt.WhyThisFits,
			"target_outcome":     opt.TargetOutcome,
			"why_worth_pursuing": opt.WhyWorthPursuing,
		} {
			if strings.TrimSpace(value) == "" {
				add("%s: %s is empty", label, field)
			} else if len(value) > GoalDiscoveryLongTextMax {
				add("%s: %s exceeds %d characters", label, field, GoalDiscoveryLongTextMax)
			}
		}

		if !goalOptionDifficulties[opt.Difficulty.Level] {
			add("%s: difficulty.level must be one of starter/moderate/demanding/intense", label)
		}
		if strings.TrimSpace(opt.Difficulty.Explanation) == "" {
			add("%s: difficulty.explanation is empty", label)
		}
		if !goalOptionAccountability[opt.AccountabilityStrength.Level] {
			add("%s: accountability_strength.level must be one of low/moderate/high", label)
		}
		if strings.TrimSpace(opt.AccountabilityStrength.Explanation) == "" {
			add("%s: accountability_strength.explanation is empty", label)
		}

		problems = append(problems, validateEffortLanes(label, opt)...)
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("invalid goal options: %s", strings.Join(problems, "; "))
}

// validateEffortLanes checks one option's three pace lanes for presence,
// per-lane sanity, and cross-lane coherence.
func validateEffortLanes(label string, opt GoalOption) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if len(opt.EffortExamples) != len(GoalOptionPaceOrder) {
		add("%s: expected exactly %d effort examples, got %d", label, len(GoalOptionPaceOrder), len(opt.EffortExamples))
	}

	lanes := map[GoalOptionPace]GoalOptionEffort{}
	for _, effort := range opt.EffortExamples {
		if _, known := laneIndex(effort.Pace); !known {
			add("%s: unknown pace value", label)
			continue
		}
		if _, dup := lanes[effort.Pace]; dup {
			add("%s: duplicate %s effort example", label, effort.Pace)
			continue
		}
		lanes[effort.Pace] = effort

		if effort.HoursPerWeekMin <= 0 || effort.HoursPerWeekMax <= 0 {
			add("%s/%s: weekly hours must be greater than zero", label, effort.Pace)
		}
		if effort.HoursPerWeekMin > effort.HoursPerWeekMax {
			add("%s/%s: hours_per_week_min exceeds hours_per_week_max", label, effort.Pace)
		}
		if effort.EstimatedDurationMonthsMin <= 0 || effort.EstimatedDurationMonthsMax <= 0 {
			add("%s/%s: estimated duration must be greater than zero", label, effort.Pace)
		}
		if effort.EstimatedDurationMonthsMin > effort.EstimatedDurationMonthsMax {
			add("%s/%s: estimated_duration_months_min exceeds estimated_duration_months_max", label, effort.Pace)
		}
		if strings.TrimSpace(effort.DisplayText) == "" {
			add("%s/%s: display_text is empty", label, effort.Pace)
		} else if len(effort.DisplayText) > GoalDiscoveryShortTextMax {
			add("%s/%s: display_text exceeds %d characters", label, effort.Pace, GoalDiscoveryShortTextMax)
		}
	}

	for _, pace := range GoalOptionPaceOrder {
		if _, ok := lanes[pace]; !ok {
			add("%s: missing %s effort example", label, pace)
		}
	}

	// Cross-lane coherence, walking focused → steady → light. A lighter pace
	// must not demand more weekly hours, and must not somehow finish sooner.
	for i := 0; i+1 < len(GoalOptionPaceOrder); i++ {
		heavier, okH := lanes[GoalOptionPaceOrder[i]]
		lighter, okL := lanes[GoalOptionPaceOrder[i+1]]
		if !okH || !okL {
			continue
		}
		if lighter.HoursPerWeekMin > heavier.HoursPerWeekMin || lighter.HoursPerWeekMax > heavier.HoursPerWeekMax {
			add("%s: %s requires more weekly hours than %s", label, lighter.Pace, heavier.Pace)
		}
		if lighter.EstimatedDurationMonthsMin < heavier.EstimatedDurationMonthsMin ||
			lighter.EstimatedDurationMonthsMax < heavier.EstimatedDurationMonthsMax {
			add("%s: %s finishes sooner than %s despite a lower weekly commitment", label, lighter.Pace, heavier.Pace)
		}
	}

	return problems
}

func laneIndex(pace GoalOptionPace) (int, bool) {
	for i, p := range GoalOptionPaceOrder {
		if p == pace {
			return i, true
		}
	}
	return 0, false
}

// ─── Stage 2: roadmap details ────────────────────────────────────────────────

type GoalDiscoveryConstraint string

const (
	ConstraintLimitedTime        GoalDiscoveryConstraint = "limited_time"
	ConstraintLimitedBudget      GoalDiscoveryConstraint = "limited_budget"
	ConstraintLowEnergy          GoalDiscoveryConstraint = "low_energy"
	ConstraintLackExperience     GoalDiscoveryConstraint = "lack_of_experience"
	ConstraintMotivation         GoalDiscoveryConstraint = "motivation_consistency"
	ConstraintOverthinking       GoalDiscoveryConstraint = "overthinking_procrastination"
	ConstraintPrivacy            GoalDiscoveryConstraint = "privacy_concerns"
	ConstraintNeedAccountability GoalDiscoveryConstraint = "need_accountability"
	ConstraintNeedBeginnerSteps  GoalDiscoveryConstraint = "need_beginner_friendly_steps"
	ConstraintNeedFlexibility    GoalDiscoveryConstraint = "need_flexibility"
	ConstraintNeedSimplicity     GoalDiscoveryConstraint = "need_simplicity"
	ConstraintPhysical           GoalDiscoveryConstraint = "physical_limitations"
	ConstraintEquipmentAccess    GoalDiscoveryConstraint = "equipment_tools_space_access"
	ConstraintOther              GoalDiscoveryConstraint = "other"
	// ConstraintNone is mutually exclusive with every constraint above.
	ConstraintNone GoalDiscoveryConstraint = "no_major_constraints"
)

var goalDiscoveryConstraints = map[GoalDiscoveryConstraint]bool{
	ConstraintLimitedTime: true, ConstraintLimitedBudget: true, ConstraintLowEnergy: true,
	ConstraintLackExperience: true, ConstraintMotivation: true, ConstraintOverthinking: true,
	ConstraintPrivacy: true, ConstraintNeedAccountability: true, ConstraintNeedBeginnerSteps: true,
	ConstraintNeedFlexibility: true, ConstraintNeedSimplicity: true, ConstraintPhysical: true,
	ConstraintEquipmentAccess: true, ConstraintOther: true, ConstraintNone: true,
}

type GoalDiscoveryPaceMode string

const (
	PaceModeGenerated     GoalDiscoveryPaceMode = "generated"
	PaceModeIdealDeadline GoalDiscoveryPaceMode = "ideal_deadline"
	PaceModeWeeklyLimit   GoalDiscoveryPaceMode = "weekly_limit"
)

type GoalDiscoveryWeeklyPreset string

const (
	WeeklyTenMinutesDay      GoalDiscoveryWeeklyPreset = "ten_minutes_day"
	WeeklyThirtyMinutesDay   GoalDiscoveryWeeklyPreset = "thirty_minutes_day"
	WeeklyTwoToThreeSessions GoalDiscoveryWeeklyPreset = "two_to_three_sessions_week"
	WeeklyFourToSixHours     GoalDiscoveryWeeklyPreset = "four_to_six_hours_week"
	WeeklySevenToTenHours    GoalDiscoveryWeeklyPreset = "seven_to_ten_hours_week"
	WeeklyTenPlusHours       GoalDiscoveryWeeklyPreset = "ten_plus_hours_week"
	WeeklyCustom             GoalDiscoveryWeeklyPreset = "custom"
)

var goalDiscoveryWeeklyPresets = map[GoalDiscoveryWeeklyPreset]bool{
	WeeklyTenMinutesDay: true, WeeklyThirtyMinutesDay: true, WeeklyTwoToThreeSessions: true,
	WeeklyFourToSixHours: true, WeeklySevenToTenHours: true, WeeklyTenPlusHours: true,
	WeeklyCustom: true,
}

// GoalDiscoveryPaceSelection is a discriminated union keyed on Mode. Only the
// fields belonging to the active mode are populated; Validate rejects a payload
// that carries data for a mode it did not select.
type GoalDiscoveryPaceSelection struct {
	Mode GoalDiscoveryPaceMode `json:"mode"`

	// Mode == generated
	SelectedPace GoalOptionPace `json:"selected_pace,omitempty"`

	// Mode == ideal_deadline. Date-only, YYYY-MM-DD, in the user's local
	// calendar — never converted to UTC anywhere in the pipeline.
	TargetDate string `json:"target_date,omitempty"`

	// Mode == weekly_limit
	Preset            GoalDiscoveryWeeklyPreset `json:"preset,omitempty"`
	CustomDescription string                    `json:"custom_description,omitempty"`
}

// GoalDiscoveryRoadmapRequest is the stage-2 submission: the full context needed
// to run the existing roadmap generator, plus the provenance linking it back to
// a server-generated option.
type GoalDiscoveryRoadmapRequest struct {
	SchemaVersion int `json:"schema_version"`

	// DiscoveryJobID is the completed goal_discovery job that produced the
	// options. Ownership and option membership are verified against it.
	DiscoveryJobID   string `json:"discovery_job_id"`
	SelectedOptionID string `json:"selected_option_id"`

	Answers GoalDiscoveryAnswers `json:"answers"`

	SelectedGoal         string `json:"selected_goal"`
	CompletionDefinition string `json:"completion_definition"`
	WhyNow               string `json:"why_now"`

	Pace GoalDiscoveryPaceSelection `json:"pace"`

	Baseline          string                    `json:"baseline"`
	Constraints       []GoalDiscoveryConstraint `json:"constraints"`
	ConstraintDetails string                    `json:"constraint_details,omitempty"`
	AdditionalContext string                    `json:"additional_context,omitempty"`

	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// Normalize trims the request's free text in place.
func (r *GoalDiscoveryRoadmapRequest) Normalize() {
	r.Answers.Normalize()
	r.SelectedGoal = strings.TrimSpace(r.SelectedGoal)
	r.CompletionDefinition = strings.TrimSpace(r.CompletionDefinition)
	r.WhyNow = strings.TrimSpace(r.WhyNow)
	r.Baseline = strings.TrimSpace(r.Baseline)
	r.ConstraintDetails = strings.TrimSpace(r.ConstraintDetails)
	r.AdditionalContext = strings.TrimSpace(r.AdditionalContext)
	r.Pace.CustomDescription = strings.TrimSpace(r.Pace.CustomDescription)
	r.Pace.TargetDate = strings.TrimSpace(r.Pace.TargetDate)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
}

// Validate checks the stage-2 payload. `today` is the user's local calendar date
// used for the future-deadline check; callers pass the request's local date so a
// timezone offset can never turn a valid tomorrow into an invalid yesterday.
func (r GoalDiscoveryRoadmapRequest) Validate(today time.Time) error {
	if r.SchemaVersion != GoalDiscoverySchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (expected %d)", r.SchemaVersion, GoalDiscoverySchemaVersion)
	}
	if strings.TrimSpace(r.DiscoveryJobID) == "" {
		return fmt.Errorf("discovery_job_id is required")
	}
	if strings.TrimSpace(r.SelectedOptionID) == "" {
		return fmt.Errorf("selected_option_id is required")
	}
	// Required, not optional: this key is the ONLY thing that stops a retried
	// submission producing a second roadmap, and the retry is exactly the case
	// where the client cannot tell whether the first attempt landed.
	if r.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key is required")
	}
	if len(r.IdempotencyKey) > GoalDiscoveryShortTextMax {
		return fmt.Errorf("idempotency_key exceeds %d characters", GoalDiscoveryShortTextMax)
	}
	if err := r.Answers.Validate(); err != nil {
		return fmt.Errorf("answers: %w", err)
	}

	if err := requireBounded("selected_goal", r.SelectedGoal, GoalDiscoveryTitleMax); err != nil {
		return err
	}
	if err := requireBounded("completion_definition", r.CompletionDefinition, GoalDiscoveryLongTextMax); err != nil {
		return err
	}
	if err := requireBounded("why_now", r.WhyNow, GoalDiscoveryLongTextMax); err != nil {
		return err
	}
	if err := requireBounded("baseline", r.Baseline, GoalDiscoveryDetailTextMax); err != nil {
		return err
	}
	if err := boundOptional("additional_context", r.AdditionalContext, GoalDiscoveryLongTextMax); err != nil {
		return err
	}

	if err := r.Pace.Validate(today); err != nil {
		return err
	}
	return r.validateConstraints()
}

func (r GoalDiscoveryRoadmapRequest) validateConstraints() error {
	if len(r.Constraints) == 0 {
		return fmt.Errorf("at least one constraint option is required")
	}

	seen := map[GoalDiscoveryConstraint]bool{}
	hasNone, hasReal, hasOther := false, false, false
	for _, constraint := range r.Constraints {
		if !goalDiscoveryConstraints[constraint] {
			return fmt.Errorf("unknown constraint value")
		}
		if seen[constraint] {
			return fmt.Errorf("duplicate constraint value")
		}
		seen[constraint] = true

		switch constraint {
		case ConstraintNone:
			hasNone = true
		case ConstraintOther:
			hasOther = true
			hasReal = true
		default:
			hasReal = true
		}
	}
	if hasNone && hasReal {
		return fmt.Errorf("%q cannot be combined with other constraints", ConstraintNone)
	}

	if hasOther {
		if err := requireBounded("constraint_details", r.ConstraintDetails, GoalDiscoveryDetailTextMax); err != nil {
			return err
		}
		return nil
	}
	return boundOptional("constraint_details", r.ConstraintDetails, GoalDiscoveryDetailTextMax)
}

// Validate checks the pace union: exactly the fields for the chosen mode, a
// known enum value, and a date-only target date strictly in the future.
func (p GoalDiscoveryPaceSelection) Validate(today time.Time) error {
	switch p.Mode {
	case PaceModeGenerated:
		if _, known := laneIndex(p.SelectedPace); !known {
			return fmt.Errorf("pace.selected_pace must be focused, steady or light")
		}
		if p.TargetDate != "" || p.Preset != "" || p.CustomDescription != "" {
			return fmt.Errorf("pace: generated mode must not carry deadline or weekly-limit fields")
		}

	case PaceModeIdealDeadline:
		if p.SelectedPace != "" || p.Preset != "" || p.CustomDescription != "" {
			return fmt.Errorf("pace: ideal_deadline mode must not carry pace or weekly-limit fields")
		}
		parsed, err := time.Parse(DateOnlyLayout, p.TargetDate)
		if err != nil {
			return fmt.Errorf("pace.target_date must be a YYYY-MM-DD date")
		}
		// Compare calendar dates only; both sides are already date-only.
		if !parsed.After(dateOnly(today)) {
			return fmt.Errorf("pace.target_date must be in the future")
		}

	case PaceModeWeeklyLimit:
		if p.SelectedPace != "" || p.TargetDate != "" {
			return fmt.Errorf("pace: weekly_limit mode must not carry pace or deadline fields")
		}
		if !goalDiscoveryWeeklyPresets[p.Preset] {
			return fmt.Errorf("pace.preset must be a known weekly-time option")
		}
		if p.Preset == WeeklyCustom {
			return requireBounded("pace.custom_description", p.CustomDescription, GoalDiscoveryCustomPaceMax)
		}
		if p.CustomDescription != "" {
			return fmt.Errorf("pace.custom_description is only allowed with the custom preset")
		}

	default:
		return fmt.Errorf("pace.mode must be generated, ideal_deadline or weekly_limit")
	}
	return nil
}

// DateOnlyLayout is the wire format for every date-only value in this feature.
// Deliberately not time.RFC3339: a target date is a calendar date, and parsing
// it with a timezone is how "tomorrow" becomes "today" for users west of UTC.
const DateOnlyLayout = "2006-01-02"

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ─── job payloads ────────────────────────────────────────────────────────────

// GoalDiscoveryJobPayload is the JSONB payload of a goal_discovery background
// job: the inputs a generation ran on, so a regeneration can reuse them and an
// audit can see exactly what produced a given option set.
//
// IdempotencyKey is stored here rather than in a new column because
// background_jobs.payload is already JSONB and Postgres can index/filter on
// payload->>'idempotency_key' — no schema change needed.
type GoalDiscoveryJobPayload struct {
	Answers        GoalDiscoveryAnswers `json:"answers"`
	ExcludeTitles  []string             `json:"exclude_titles,omitempty"`
	PreviousJobID  string               `json:"previous_job_id,omitempty"`
	IdempotencyKey string               `json:"idempotency_key,omitempty"`
	PromptVersion  string               `json:"prompt_version"`
}

// GoalDiscoveryRoadmapJobPayload is the payload of the roadmap_generation job
// created by stage 2. GoalRequest is embedded (and therefore flattened in JSON)
// so anything already reading a roadmap job's payload keeps working unchanged;
// the goal-discovery provenance fields sit alongside it.
type GoalDiscoveryRoadmapJobPayload struct {
	GoalRequest
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	DiscoveryJobID   string `json:"discovery_job_id,omitempty"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
}

// ─── shared field helpers ────────────────────────────────────────────────────

// requireBounded rejects empty/whitespace-only values and values over max.
// The caller has already trimmed, so an empty string here means the user typed
// nothing but spaces.
func requireBounded(field, value string, max int) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > max {
		return fmt.Errorf("%s exceeds %d characters", field, max)
	}
	return nil
}

func boundOptional(field, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s exceeds %d characters", field, max)
	}
	return nil
}
