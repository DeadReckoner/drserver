package types

import (
	"strings"
	"testing"
	"time"
)

// Validation rules for the goal discovery questionnaire, the generated option
// set, and the stage-2 roadmap payload. These are the rules the mobile client
// mirrors, so a change here is a contract change.

func validAnswers() GoalDiscoveryAnswers {
	return GoalDiscoveryAnswers{
		SchemaVersion:     GoalDiscoverySchemaVersion,
		Trigger:           TriggerFeelStuck,
		Areas:             []GoalDiscoveryArea{AreaCareer, AreaMoney},
		SelfDescription:   SelfKnowButDontDo,
		RecurringThought:  "Starting a side business",
		SuccessDefinition: "One paying customer and a repeatable weekly routine",
		Outcomes:          []GoalDiscoveryOutcome{OutcomeCreateIncome},
		TargetType:        TargetMoneyProject,
	}
}

func TestAnswersValidate_Valid(t *testing.T) {
	a := validAnswers()
	if err := a.Validate(); err != nil {
		t.Fatalf("expected valid answers, got %v", err)
	}
}

func TestAnswersValidate_RejectsUnsupportedSchemaVersion(t *testing.T) {
	a := validAnswers()
	a.SchemaVersion = GoalDiscoverySchemaVersion + 1
	if err := a.Validate(); err == nil {
		t.Fatal("expected a schema_version rejection")
	}
}

func TestAnswersValidate_RequiredFields(t *testing.T) {
	cases := map[string]func(*GoalDiscoveryAnswers){
		"missing trigger":            func(a *GoalDiscoveryAnswers) { a.Trigger = "" },
		"missing self description":   func(a *GoalDiscoveryAnswers) { a.SelfDescription = "" },
		"missing target type":        func(a *GoalDiscoveryAnswers) { a.TargetType = "" },
		"missing recurring thought":  func(a *GoalDiscoveryAnswers) { a.RecurringThought = "" },
		"missing success definition": func(a *GoalDiscoveryAnswers) { a.SuccessDefinition = "" },
		"no areas":                   func(a *GoalDiscoveryAnswers) { a.Areas = nil },
		"no outcomes":                func(a *GoalDiscoveryAnswers) { a.Outcomes = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a := validAnswers()
			mutate(&a)
			if err := a.Validate(); err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}

// Whitespace-only text must not satisfy a required field. Normalize trims first
// so the check downstream sees an empty string.
func TestAnswersValidate_RejectsWhitespaceOnlyRequiredText(t *testing.T) {
	a := validAnswers()
	a.RecurringThought = "   \t\n  "
	a.Normalize()
	if err := a.Validate(); err == nil {
		t.Fatal("expected whitespace-only recurring_thought to be rejected")
	}
}

func TestAnswersValidate_AreaMaximumCountsCustomArea(t *testing.T) {
	// Three enum areas is the limit.
	a := validAnswers()
	a.Areas = []GoalDiscoveryArea{AreaCareer, AreaMoney, AreaHealth}
	if err := a.Validate(); err != nil {
		t.Fatalf("three areas should be allowed, got %v", err)
	}

	// A custom area on top of three is a fourth selection.
	a.CustomArea = "Community volunteering"
	if err := a.Validate(); err == nil {
		t.Fatal("a custom area on top of three enum areas must be rejected")
	}

	// Two enum areas plus a custom one is exactly three.
	a.Areas = []GoalDiscoveryArea{AreaCareer, AreaMoney}
	if err := a.Validate(); err != nil {
		t.Fatalf("two areas plus a custom area should be allowed, got %v", err)
	}
}

func TestAnswersValidate_OutcomeMaximumCountsCustomOutcome(t *testing.T) {
	a := validAnswers()
	a.Outcomes = []GoalDiscoveryOutcome{OutcomeCreateIncome, OutcomeBuildConfidence}
	if err := a.Validate(); err != nil {
		t.Fatalf("two outcomes should be allowed, got %v", err)
	}

	a.CustomOutcome = "Stop dreading Mondays"
	if err := a.Validate(); err == nil {
		t.Fatal("a custom outcome on top of two enum outcomes must be rejected")
	}

	a.Outcomes = []GoalDiscoveryOutcome{OutcomeCreateIncome}
	if err := a.Validate(); err != nil {
		t.Fatalf("one outcome plus a custom outcome should be allowed, got %v", err)
	}
}

func TestAnswersValidate_RejectsUnknownAndDuplicateEnums(t *testing.T) {
	a := validAnswers()
	a.Areas = []GoalDiscoveryArea{"teleportation"}
	if err := a.Validate(); err == nil {
		t.Fatal("expected unknown area to be rejected")
	}

	a = validAnswers()
	a.Areas = []GoalDiscoveryArea{AreaCareer, AreaCareer}
	if err := a.Validate(); err == nil {
		t.Fatal("expected duplicate area to be rejected")
	}

	a = validAnswers()
	a.Outcomes = []GoalDiscoveryOutcome{"become immortal"}
	if err := a.Validate(); err == nil {
		t.Fatal("expected unknown outcome to be rejected")
	}
}

func TestAnswersValidate_RejectsOversizedText(t *testing.T) {
	a := validAnswers()
	a.RecurringThought = strings.Repeat("x", GoalDiscoveryLongTextMax+1)
	if err := a.Validate(); err == nil {
		t.Fatal("expected oversized recurring_thought to be rejected")
	}

	a = validAnswers()
	a.TriggerNote = strings.Repeat("y", GoalDiscoveryShortTextMax+1)
	if err := a.Validate(); err == nil {
		t.Fatal("expected oversized trigger_note to be rejected")
	}
}

// ─── generated option validation ─────────────────────────────────────────────

func coherentEfforts() []GoalOptionEffort {
	return []GoalOptionEffort{
		{Pace: PaceFocused, HoursPerWeekMin: 8, HoursPerWeekMax: 10, EstimatedDurationMonthsMin: 2, EstimatedDurationMonthsMax: 3, DisplayText: "8-10 h/wk, 2-3 months"},
		{Pace: PaceSteady, HoursPerWeekMin: 4, HoursPerWeekMax: 6, EstimatedDurationMonthsMin: 4, EstimatedDurationMonthsMax: 5, DisplayText: "4-6 h/wk, 4-5 months"},
		{Pace: PaceLight, HoursPerWeekMin: 2, HoursPerWeekMax: 3, EstimatedDurationMonthsMin: 7, EstimatedDurationMonthsMax: 9, DisplayText: "2-3 h/wk, 7-9 months"},
	}
}

func validOptions() []GoalOption {
	titles := []string{"Ship a paid landing page", "Publish a weekly newsletter", "Earn a cloud certification"}
	options := make([]GoalOption, 0, GoalDiscoveryOptionCount)
	for i, title := range titles {
		options = append(options, GoalOption{
			Rank:             i + 1,
			GoalTitle:        title,
			WhyThisFits:      "Matches the area and outcome you chose.",
			TargetOutcome:    "A concrete, checkable result exists.",
			WhyWorthPursuing: "It turns intent into evidence.",
			EffortExamples:   coherentEfforts(),
			Difficulty:       GoalOptionDifficulty{Level: DifficultyModerate, Explanation: "Fits your stated starting point."},
			AccountabilityStrength: GoalOptionAccountability{
				Level: AccountabilityModerate, Explanation: "Needs a weekly check-in to stick.",
			},
		})
	}
	return options
}

func TestValidateGoalOptions_Valid(t *testing.T) {
	if err := ValidateGoalOptions(validOptions()); err != nil {
		t.Fatalf("expected valid option set, got %v", err)
	}
}

func TestValidateGoalOptions_RequiresExactlyThree(t *testing.T) {
	if err := ValidateGoalOptions(validOptions()[:2]); err == nil {
		t.Fatal("expected two options to be rejected")
	}
	four := append(validOptions(), validOptions()[0])
	if err := ValidateGoalOptions(four); err == nil {
		t.Fatal("expected four options to be rejected")
	}
}

func TestValidateGoalOptions_RejectsDuplicateRanks(t *testing.T) {
	options := validOptions()
	options[2].Rank = 1
	err := ValidateGoalOptions(options)
	if err == nil || !strings.Contains(err.Error(), "duplicate rank") {
		t.Fatalf("expected a duplicate-rank rejection, got %v", err)
	}
}

func TestValidateGoalOptions_RejectsOutOfRangeRank(t *testing.T) {
	options := validOptions()
	options[1].Rank = 7
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected an out-of-range rank to be rejected")
	}
}

// Three rewordings of the same goal are not three options. Case and spacing
// differences must not sneak a duplicate through.
func TestValidateGoalOptions_RejectsDuplicateTitles(t *testing.T) {
	options := validOptions()
	options[1].GoalTitle = "  ship a PAID landing   page "
	err := ValidateGoalOptions(options)
	if err == nil || !strings.Contains(err.Error(), "duplicates another option") {
		t.Fatalf("expected a duplicate-title rejection, got %v", err)
	}
}

func TestValidateGoalOptions_RejectsMissingPaceLane(t *testing.T) {
	options := validOptions()
	options[0].EffortExamples = options[0].EffortExamples[:2] // drops light
	err := ValidateGoalOptions(options)
	if err == nil || !strings.Contains(err.Error(), "missing light effort example") {
		t.Fatalf("expected a missing-lane rejection, got %v", err)
	}
}

func TestValidateGoalOptions_RejectsDuplicatePaceLane(t *testing.T) {
	options := validOptions()
	options[0].EffortExamples[2].Pace = PaceSteady
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected a duplicated pace lane to be rejected")
	}
}

// Focused must never ask for fewer weekly hours than steady, and steady never
// fewer than light — the whole point of offering three lanes.
func TestValidateGoalOptions_RejectsIncoherentWeeklyHours(t *testing.T) {
	options := validOptions()
	options[0].EffortExamples[2].HoursPerWeekMax = 99 // light now heavier than steady
	err := ValidateGoalOptions(options)
	if err == nil || !strings.Contains(err.Error(), "more weekly hours") {
		t.Fatalf("expected an incoherent-hours rejection, got %v", err)
	}
}

// A lower weekly commitment finishing sooner is not a plan, it is a mistake.
func TestValidateGoalOptions_RejectsLighterPaceFinishingSooner(t *testing.T) {
	options := validOptions()
	options[0].EffortExamples[2].EstimatedDurationMonthsMin = 1
	options[0].EffortExamples[2].EstimatedDurationMonthsMax = 1
	err := ValidateGoalOptions(options)
	if err == nil || !strings.Contains(err.Error(), "finishes sooner") {
		t.Fatalf("expected an incoherent-timeline rejection, got %v", err)
	}
}

func TestValidateGoalOptions_RejectsNonPositiveOrInvertedRanges(t *testing.T) {
	options := validOptions()
	options[0].EffortExamples[0].HoursPerWeekMin = 0
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected zero weekly hours to be rejected")
	}

	options = validOptions()
	options[0].EffortExamples[0].HoursPerWeekMin = 20
	options[0].EffortExamples[0].HoursPerWeekMax = 10
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected an inverted hours range to be rejected")
	}
}

func TestValidateGoalOptions_RejectsEmptyNarrativeAndUnknownLevels(t *testing.T) {
	options := validOptions()
	options[0].TargetOutcome = "   "
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected an empty target_outcome to be rejected")
	}

	options = validOptions()
	options[1].Difficulty.Level = "impossible"
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected an unknown difficulty level to be rejected")
	}

	options = validOptions()
	options[2].AccountabilityStrength.Level = "extreme"
	if err := ValidateGoalOptions(options); err == nil {
		t.Fatal("expected an unknown accountability level to be rejected")
	}
}

// ─── stage 2 payload validation ──────────────────────────────────────────────

var fixedToday = time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)

func validRoadmapRequest() GoalDiscoveryRoadmapRequest {
	return GoalDiscoveryRoadmapRequest{
		SchemaVersion:        GoalDiscoverySchemaVersion,
		DiscoveryJobID:       "b0a1c2d3-0000-0000-0000-000000000001",
		SelectedOptionID:     "b0a1c2d3-0000-0000-0000-000000000002",
		Answers:              validAnswers(),
		SelectedGoal:         "Ship a paid landing page",
		CompletionDefinition: "The page is live and has taken one real payment.",
		WhyNow:               "I keep planning and never shipping.",
		Pace:                 GoalDiscoveryPaceSelection{Mode: PaceModeGenerated, SelectedPace: PaceSteady},
		Baseline:             "I can write basic HTML and have a domain already.",
		Constraints:          []GoalDiscoveryConstraint{ConstraintLimitedTime},
		IdempotencyKey:       "client-key-1",
	}
}

// Without a key the server cannot recognise a retry, so a dropped response would
// leave the user with two roadmaps.
func TestRoadmapRequestValidate_RequiresIdempotencyKey(t *testing.T) {
	r := validRoadmapRequest()
	r.IdempotencyKey = ""
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected a missing idempotency_key to be rejected")
	}
}

func TestRoadmapRequestValidate_Valid(t *testing.T) {
	if err := validRoadmapRequest().Validate(fixedToday); err != nil {
		t.Fatalf("expected a valid request, got %v", err)
	}
}

func TestRoadmapRequestValidate_RequiredText(t *testing.T) {
	cases := map[string]func(*GoalDiscoveryRoadmapRequest){
		"empty selected goal":     func(r *GoalDiscoveryRoadmapRequest) { r.SelectedGoal = "" },
		"empty completion":        func(r *GoalDiscoveryRoadmapRequest) { r.CompletionDefinition = "" },
		"empty why now":           func(r *GoalDiscoveryRoadmapRequest) { r.WhyNow = "" },
		"empty baseline":          func(r *GoalDiscoveryRoadmapRequest) { r.Baseline = "" },
		"missing discovery job":   func(r *GoalDiscoveryRoadmapRequest) { r.DiscoveryJobID = "" },
		"missing selected option": func(r *GoalDiscoveryRoadmapRequest) { r.SelectedOptionID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validRoadmapRequest()
			mutate(&r)
			if err := r.Validate(fixedToday); err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}

func TestRoadmapRequestValidate_WhitespaceOnlyGoalRejected(t *testing.T) {
	r := validRoadmapRequest()
	r.SelectedGoal = "     "
	r.Normalize()
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected a whitespace-only selected_goal to be rejected")
	}
}

func TestRoadmapRequestValidate_OversizedFieldsRejected(t *testing.T) {
	r := validRoadmapRequest()
	r.SelectedGoal = strings.Repeat("g", GoalDiscoveryTitleMax+1)
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an oversized selected_goal to be rejected")
	}

	r = validRoadmapRequest()
	r.Baseline = strings.Repeat("b", GoalDiscoveryDetailTextMax+1)
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an oversized baseline to be rejected")
	}
}

func TestPaceValidate_GeneratedMode(t *testing.T) {
	r := validRoadmapRequest()
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeGenerated, SelectedPace: PaceFocused}
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("focused should be accepted, got %v", err)
	}

	r.Pace.SelectedPace = "sprint"
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an unknown pace to be rejected")
	}

	// Carrying another mode's data is a client bug, not a valid submission.
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeGenerated, SelectedPace: PaceLight, TargetDate: "2027-01-01"}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected generated mode carrying a target date to be rejected")
	}
}

func TestPaceValidate_IdealDeadline(t *testing.T) {
	r := validRoadmapRequest()
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeIdealDeadline, TargetDate: "2026-12-01"}
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("a future date should be accepted, got %v", err)
	}

	// Today is not "in the future" — a same-day deadline leaves no time to plan.
	r.Pace.TargetDate = fixedToday.Format(DateOnlyLayout)
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected today's date to be rejected")
	}

	r.Pace.TargetDate = "2020-01-01"
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected a past date to be rejected")
	}

	// Date-only means date-only: a timestamp is not an acceptable target date.
	r.Pace.TargetDate = "2026-12-01T00:00:00Z"
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an RFC3339 timestamp to be rejected")
	}
}

// The stored date must survive validation byte-for-byte: no timezone shifting
// is allowed to turn the user's chosen calendar day into the previous one.
func TestPaceValidate_IdealDeadlinePreservesCalendarDate(t *testing.T) {
	const chosen = "2026-09-15"
	r := validRoadmapRequest()
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeIdealDeadline, TargetDate: chosen}
	r.Normalize()
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("expected the date to validate, got %v", err)
	}
	if r.Pace.TargetDate != chosen {
		t.Fatalf("target date changed during normalize/validate: %q", r.Pace.TargetDate)
	}
}

func TestPaceValidate_WeeklyLimit(t *testing.T) {
	r := validRoadmapRequest()
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeWeeklyLimit, Preset: WeeklyFourToSixHours}
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("a known preset should be accepted, got %v", err)
	}

	r.Pace.Preset = "whenever"
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an unknown preset to be rejected")
	}

	// Custom demands the description.
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeWeeklyLimit, Preset: WeeklyCustom}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected the custom preset without a description to be rejected")
	}

	r.Pace.CustomDescription = "45 minutes on weekdays, three hours on Saturday"
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("custom preset with a description should be accepted, got %v", err)
	}

	r.Pace.CustomDescription = strings.Repeat("t", GoalDiscoveryCustomPaceMax+1)
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an oversized custom description to be rejected")
	}

	// A description without the custom preset means the client sent stale state.
	r.Pace = GoalDiscoveryPaceSelection{Mode: PaceModeWeeklyLimit, Preset: WeeklyTenPlusHours, CustomDescription: "leftover"}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected a stray custom description to be rejected")
	}
}

func TestPaceValidate_RejectsUnknownMode(t *testing.T) {
	r := validRoadmapRequest()
	r.Pace = GoalDiscoveryPaceSelection{Mode: "vibes"}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an unknown pace mode to be rejected")
	}
}

func TestConstraintsValidate_AtLeastOneRequired(t *testing.T) {
	r := validRoadmapRequest()
	r.Constraints = nil
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an empty constraint list to be rejected")
	}
}

// "No major constraints right now" is mutually exclusive with every real
// constraint — the two together describe nothing coherent.
func TestConstraintsValidate_NoneIsMutuallyExclusive(t *testing.T) {
	r := validRoadmapRequest()
	r.Constraints = []GoalDiscoveryConstraint{ConstraintNone, ConstraintLimitedTime}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected 'none' combined with a real constraint to be rejected")
	}

	r.Constraints = []GoalDiscoveryConstraint{ConstraintNone}
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("'none' on its own should be accepted, got %v", err)
	}
}

func TestConstraintsValidate_OtherRequiresDetails(t *testing.T) {
	r := validRoadmapRequest()
	r.Constraints = []GoalDiscoveryConstraint{ConstraintOther}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected 'other' without details to be rejected")
	}

	// Whitespace is not detail.
	r.ConstraintDetails = "   "
	r.Normalize()
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected whitespace-only details to be rejected for 'other'")
	}

	r.ConstraintDetails = "I travel two weeks a month for work."
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("'other' with details should be accepted, got %v", err)
	}
}

// Details stay OPTIONAL when "other" is not selected.
func TestConstraintsValidate_DetailsOptionalWithoutOther(t *testing.T) {
	r := validRoadmapRequest()
	r.Constraints = []GoalDiscoveryConstraint{ConstraintLimitedTime, ConstraintLowEnergy}
	r.ConstraintDetails = ""
	if err := r.Validate(fixedToday); err != nil {
		t.Fatalf("details should be optional without 'other', got %v", err)
	}
}

func TestConstraintsValidate_RejectsUnknownAndDuplicates(t *testing.T) {
	r := validRoadmapRequest()
	r.Constraints = []GoalDiscoveryConstraint{"mercury_retrograde"}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected an unknown constraint to be rejected")
	}

	r.Constraints = []GoalDiscoveryConstraint{ConstraintLimitedTime, ConstraintLimitedTime}
	if err := r.Validate(fixedToday); err == nil {
		t.Fatal("expected a duplicate constraint to be rejected")
	}
}

// ─── wire decoding ───────────────────────────────────────────────────────────

func TestDecodeGoalOptionsPayload_SafetyBlockShortCircuits(t *testing.T) {
	out, err := DecodeGoalOptionsPayload([]byte(`{"safety_blocked":true,"safety_reason":"crisis","options":[]}`))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out.Safety == nil || !out.Safety.Blocked {
		t.Fatal("expected a blocked safety result")
	}
	if out.Safety.Message != GoalOptionsSafetyMessage {
		t.Fatal("expected the canonical safety message, not the model's own text")
	}
	if len(out.Options) != 0 {
		t.Fatal("a safety block must not carry goal options")
	}
}

func TestDecodeGoalOptionsPayload_MalformedJSONIsAnError(t *testing.T) {
	if _, err := DecodeGoalOptionsPayload([]byte(`{"options": `)); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}

// The model's id (if it invents one) must never survive decoding: ids are minted
// server-side after validation.
func TestDecodeGoalOptionsPayload_IgnoresModelSuppliedID(t *testing.T) {
	out, err := DecodeGoalOptionsPayload([]byte(`{
		"safety_blocked": false, "safety_reason": "",
		"options": [{"id":"attacker-controlled","rank":1,"goal_title":"T","why_this_fits":"f",
		"target_outcome":"o","why_worth_pursuing":"w","effort_examples":[],
		"difficulty":{"level":"moderate","explanation":"e"},
		"accountability_strength":{"level":"low","explanation":"e"}}]}`))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out.Options[0].ID != "" {
		t.Fatalf("model-supplied id leaked through decoding: %q", out.Options[0].ID)
	}
}

// ─── notification type registration ──────────────────────────────────────────

// Every notification type the goal discovery flow dispatches must resolve in
// MyGoalsPrefs.Lookup. The resolver DROPS unknown types (ReasonUnknownType), so
// an unregistered name means the notification is silently never delivered —
// a failure mode that no amount of asserting on a database row would catch.
func TestMyGoalsPrefs_ResolvesEveryGoalDiscoveryNotificationType(t *testing.T) {
	var prefs MyGoalsPrefs
	prefs.AgentUpdates = NotificationTypePref{Enabled: true, Delivery: DeliveryImmediate}

	for _, notificationType := range []string{
		"goal_options_complete",
		"goal_options_failed",
		// A safety-blocked result is its own type so it never borrows the
		// "your options are ready, choose one" copy.
		"goal_options_unavailable",
	} {
		t.Run(notificationType, func(t *testing.T) {
			pref, ok := prefs.Lookup(notificationType)
			if !ok {
				t.Fatalf("%s is not a known my-goals notification type; the resolver would drop it", notificationType)
			}
			// They share the single agent_updates toggle, so the user's
			// notification settings screen needs no new row.
			if pref != prefs.AgentUpdates {
				t.Fatalf("%s must map to the shared agent_updates toggle", notificationType)
			}
		})
	}
}

// The expiry reason is deliberately distinct from a generation failure so the
// client (and a support log) can tell "the server lost this" from "the model
// could not do it".
func TestJobExpiredError_IsDistinctAndActionable(t *testing.T) {
	if JobExpiredError == "" {
		t.Fatal("an expired job needs a typed, user-facing reason")
	}
	if !strings.Contains(JobExpiredError, "try again") {
		t.Fatal("an expired job must tell the user what to do next")
	}
	if !strings.Contains(JobExpiredError, "saved") {
		t.Fatal("an expired job must reassure the user their answers survived")
	}
}

func TestStaleAfterForJobType(t *testing.T) {
	// Every window must be far longer than a real generation so expiry can
	// never race work that is genuinely still running.
	if got := StaleAfterForJobType(JobTypeGoalDiscovery); got != 15*time.Minute {
		t.Fatalf("goal discovery window = %v", got)
	}
	if got := StaleAfterForJobType(JobTypeRoadmap); got != 30*time.Minute {
		t.Fatalf("roadmap window = %v", got)
	}
	// An unknown type is more likely a slow new generation than an orphan, so
	// the safe default is to wait longer.
	if got := StaleAfterForJobType(JobType("something_new")); got != DefaultJobStaleAfter {
		t.Fatalf("default window = %v", got)
	}
}
