package database

import (
	"dr/server/types"
)

// Plan gating helpers. Each Check* function returns nil if the user is
// allowed to perform the action, or a *types.PlanLimitError describing the
// block.
//
// Callers should already have validated user_id and the existence of any
// resource being acted upon; plan checks run AFTER auth and BEFORE business
// logic. These functions use the package-global *gorm.DB; tests swap it via
// SetDBForTest.
//
// All helpers tolerate a missing entitlement row by treating the user as free
// (GetEntitlement lazily creates one).

// CheckCreateRoadmap blocks free users at the active-roadmap cap.
func CheckCreateRoadmap(userID string) (*types.PlanLimitError, error) {
	ctx, err := GetUserPlanContext(userID)
	if err != nil {
		return nil, err
	}
	if ctx.Entitlement.IsPaid() {
		return nil, nil
	}
	limit := types.FreeLimits.ActiveRoadmaps
	if ctx.ActiveRoadmapCount >= limit {
		return &types.PlanLimitError{
			Code:    "plan_limit",
			Feature: "active_roadmaps",
			Limit:   limit,
			Current: ctx.ActiveRoadmapCount,
			Message: "Free users can have 1 active roadmap. Upgrade to create unlimited roadmaps.",
		}, nil
	}
	return nil, nil
}

// CheckCreateHabit blocks free users at the active-habit cap.
func CheckCreateHabit(userID string) (*types.PlanLimitError, error) {
	ctx, err := GetUserPlanContext(userID)
	if err != nil {
		return nil, err
	}
	if ctx.Entitlement.IsPaid() {
		return nil, nil
	}
	limit := types.FreeLimits.ActiveHabits
	if ctx.ActiveHabitCount >= limit {
		return &types.PlanLimitError{
			Code:    "plan_limit",
			Feature: "active_habits",
			Limit:   limit,
			Current: ctx.ActiveHabitCount,
			Message: "Free users can track up to 3 active habits. Upgrade for unlimited habits.",
		}, nil
	}
	return nil, nil
}

// CheckCreateRoadmapAnchor blocks free users from adding a roadmap anchor
// beyond the free cap (currently 1 anchored roadmap).
//
// Interpretation: the free tier allows ONE anchored roadmap at a time. We
// count active (non-archived) roadmaps where anchor_id IS NOT NULL. If the
// user is already at the cap, block.
func CheckCreateRoadmapAnchor(userID string) (*types.PlanLimitError, error) {
	ent, err := GetEntitlement(userID)
	if err != nil {
		return nil, err
	}
	if ent.IsPaid() {
		return nil, nil
	}
	var anchoredCount int64
	if err := db.Model(&types.Roadmap{}).
		Where("user_id = ? AND archived = ? AND anchor_id IS NOT NULL", userID, false).
		Count(&anchoredCount).Error; err != nil {
		return nil, err
	}
	limit := types.FreeLimits.AnchorsPerRoadmap
	if int(anchoredCount) >= limit {
		return &types.PlanLimitError{
			Code:    "plan_limit",
			Feature: "roadmap_anchors",
			Limit:   limit,
			Current: int(anchoredCount),
			Message: "Free users can use 1 accountability anchor. Upgrade to add more anchors.",
		}, nil
	}
	return nil, nil
}

// CheckCreateHabitAnchor blocks free users entirely from habit anchors -
// the free tier only allows anchors on roadmaps, not habits.
func CheckCreateHabitAnchor(userID string) (*types.PlanLimitError, error) {
	ent, err := GetEntitlement(userID)
	if err != nil {
		return nil, err
	}
	if ent.IsPaid() {
		return nil, nil
	}
	return &types.PlanLimitError{
		Code:    "plan_limit",
		Feature: "habit_anchors",
		Limit:   0,
		Current: 0,
		Message: "Habit anchors are available with Pro. Upgrade to add anchors to your habits.",
	}, nil
}

// CheckAIGeneration blocks free users at the daily AI generation cap
// (drmobile#183: 3/day instead of a monthly allowance, so a free user is
// never locked out for weeks). Does NOT increment the counter - call
// IncrementAIGenerations after the generation actually succeeds.
func CheckAIGeneration(userID string) (*types.PlanLimitError, error) {
	ctx, err := GetUserPlanContext(userID)
	if err != nil {
		return nil, err
	}
	if ctx.Entitlement.IsPaid() {
		return nil, nil
	}
	limit := types.FreeLimits.AIGenerationsPerDay
	if ctx.Entitlement.AIGenerationsUsed >= limit {
		return &types.PlanLimitError{
			Code:    "plan_limit",
			Feature: "ai_generations_per_day",
			Limit:   limit,
			Current: ctx.Entitlement.AIGenerationsUsed,
			Message: "You've used your free AI generations for today. Upgrade for unlimited AI roadmaps, or try again tomorrow.",
		}, nil
	}
	return nil, nil
}

// GoalOptionsLimitsFor returns the option-generation budget for a user.
//
// Goal-option generation is deliberately NOT charged to the roadmap counter
// (CheckAIGeneration / IncrementAIGenerations). Charging both to one 3-a-day
// allowance meant a free user who generated options and regenerated once had
// nothing left to actually build the roadmap those options existed to produce —
// the flow could sell you a goal it would then refuse to plan.
//
// Cost is still bounded, on two independent axes:
//   - per user per UTC day, so the total spend a single account can cause is
//     capped whether or not they are paying;
//   - per discovery draft, so "show me different options" cannot be tapped
//     indefinitely against one set of answers.
//
// Both are enforced inside the admission transaction (see AdmitGoalOptionsJob),
// never on the client.
func GoalOptionsLimitsFor(userID string) (daily, perDraft int, err error) {
	ent, err := GetEntitlement(userID)
	if err != nil {
		return 0, 0, err
	}
	if ent.IsPaid() {
		return types.GoalOptionsPaidDailyLimit, types.GoalOptionsPerDraftLimit, nil
	}
	return types.GoalOptionsFreeDailyLimit, types.GoalOptionsPerDraftLimit, nil
}

// CheckPaidFeature blocks free users from a boolean-gated paid feature. Use
// this for features that have no numeric limit (custom reminders, archive,
// weekly review, etc).
func CheckPaidFeature(userID string, feature types.FeatureKey, message string) (*types.PlanLimitError, error) {
	ent, err := GetEntitlement(userID)
	if err != nil {
		return nil, err
	}
	if ent.CanUseFeature(feature) {
		return nil, nil
	}
	if message == "" {
		message = "This feature is available with Pro."
	}
	return &types.PlanLimitError{
		Code:    "plan_limit",
		Feature: string(feature),
		Limit:   0,
		Current: 0,
		Message: message,
	}, nil
}
