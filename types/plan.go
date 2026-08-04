package types

// Plan identifiers stored in entitlements.plan.
const (
	PlanFree = "free"
	PlanPaid = "paid"
)

// Entitlement sources stored in entitlements.source.
const (
	EntitlementSourceDefault = "default"
	EntitlementSourceApple   = "apple"
	EntitlementSourceGoogle  = "google"
	EntitlementSourceStripe  = "stripe"
	EntitlementSourceComp    = "comp"
)

// Subscription status values stored in entitlements.status.
const (
	EntitlementStatusActive    = "active"
	EntitlementStatusTrialing  = "trialing"
	EntitlementStatusInGrace   = "in_grace"
	EntitlementStatusCancelled = "cancelled"
	EntitlementStatusExpired   = "expired"
)

// PlanLimits describes the numeric caps applied to a plan.
// A value of -1 means unlimited.
type PlanLimits struct {
	ActiveRoadmaps    int `json:"active_roadmaps"`
	ActiveHabits      int `json:"active_habits"`
	AnchorsPerRoadmap int `json:"anchors_per_roadmap"`
	// AIGenerationsPerDay is a daily cap (drmobile#183) so free users are
	// never blocked for a whole month by one lifetime-ish limit; the counter
	// resets when the UTC calendar day rolls.
	AIGenerationsPerDay int `json:"ai_generations_per_day"`
}

// FreeLimits are the caps applied to free-plan users.
// Paid users get unlimited (PaidLimits below).
var FreeLimits = PlanLimits{
	ActiveRoadmaps:      1,
	ActiveHabits:        3,
	AnchorsPerRoadmap:   1,
	AIGenerationsPerDay: 3,
}

// PaidLimits represents unlimited usage. -1 sentinel is interpreted by callers
// as "no enforcement".
var PaidLimits = PlanLimits{
	ActiveRoadmaps:      -1,
	ActiveHabits:        -1,
	AnchorsPerRoadmap:   -1,
	AIGenerationsPerDay: -1,
}

// Goal-option generation budget.
//
// These are cost bounds, not a plan feature gate: the option step is a cheap
// generation whose whole purpose is to lead into a roadmap, so it is metered
// separately from AIGenerationsPerDay rather than competing with it. See
// database.GoalOptionsLimitsFor for why.
//
// Every value is enforced server-side inside the admission transaction, so a
// modified client cannot exceed them.
const (
	// GoalOptionsPerDraftLimit bounds generations against ONE set of stage-1
	// answers: the first run plus two "show me different options". Applies to
	// free and paid alike — it bounds abuse, not entitlement.
	GoalOptionsPerDraftLimit = 3

	// GoalOptionsFreeDailyLimit bounds a free user's option generations per UTC
	// day. Two full drafts' worth, so abandoning one set of answers and
	// starting over is possible without unbounded spend.
	GoalOptionsFreeDailyLimit = 6

	// GoalOptionsPaidDailyLimit is the same bound for paid users. Not
	// unlimited: a compromised or scripted paid account is still real money.
	GoalOptionsPaidDailyLimit = 30
)

// LimitsForPlan returns the appropriate limit set for a plan identifier.
func LimitsForPlan(plan string) PlanLimits {
	if plan == PlanPaid {
		return PaidLimits
	}
	return FreeLimits
}

// FeatureKey enumerates boolean-gated features. Used by upcoming enforcement
// (Step 2) and by mobile to render lock states.
type FeatureKey string

const (
	FeatureCustomReminders    FeatureKey = "custom_reminders"
	FeatureAnchorSettings     FeatureKey = "anchor_settings"
	FeatureHabitAnchors       FeatureKey = "habit_anchors"
	FeatureAdvancedStats      FeatureKey = "advanced_stats"
	FeatureHistoricalPerf     FeatureKey = "historical_performance"
	FeatureArchive            FeatureKey = "archive"
	FeatureWeeklyReview       FeatureKey = "weekly_review"
	FeatureMessagingReactions FeatureKey = "messaging_reactions"
)
