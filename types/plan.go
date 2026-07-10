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
