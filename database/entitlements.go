package database

import (
	"errors"
	"time"

	"dr/server/types"

	"gorm.io/gorm"
)

// UserPlanContext bundles a user's entitlement with the live usage counts that
// Step 2 enforcement (and the /billing/status endpoint today) need to make
// gating decisions in a single read pass.
type UserPlanContext struct {
	Entitlement        *types.Entitlement
	ActiveRoadmapCount int
	ActiveHabitCount   int
}

// GetEntitlement returns the entitlement row for a user, lazily creating one if
// it is missing. The DB trigger handles new users, but this guard keeps the API
// safe if the trigger is ever disabled or a backfill is skipped.
//
// Before returning, it applies the Pro email whitelist overlay (see
// applyProWhitelist): users on a whitelisted domain (e.g. @deadreckoner.io)
// are reported as paid without a real subscription.
func GetEntitlement(userID string) (*types.Entitlement, error) {
	if userID == "" {
		return nil, errors.New("missing user_id")
	}

	var ent types.Entitlement
	err := db.Where("user_id = ?", userID).First(&ent).Error
	if err == nil {
		applyProWhitelist(&ent)
		return &ent, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Missing row - create a default free entitlement. ON CONFLICT DO NOTHING
	// equivalent: tolerate races with the user-insert trigger.
	ent = types.Entitlement{
		UserID:                   userID,
		Plan:                     types.PlanFree,
		Source:                   types.EntitlementSourceDefault,
		AIGenerationsPeriodStart: time.Now().UTC(),
	}
	if err := db.Create(&ent).Error; err != nil {
		// Another writer may have created it between First and Create; re-read.
		if err2 := db.Where("user_id = ?", userID).First(&ent).Error; err2 == nil {
			applyProWhitelist(&ent)
			return &ent, nil
		}
		return nil, err
	}
	applyProWhitelist(&ent)
	return &ent, nil
}

// applyProWhitelist is a READ-TIME OVERLAY: if the entitlement's user is on a
// whitelisted email domain, it reports them as paid (source=comp) in-memory
// WITHOUT writing to the DB. This keeps the real subscription row untouched -
// an employee can still test real sandbox purchases and the stored row
// reflects reality, while they always retain Pro access via this overlay.
//
// Skipped when the user is already paid (real subscribers don't need the
// overlay, and we avoid the extra user-email lookup on that path). The email
// lookup is a single indexed PK query; for non-paid users it runs once per
// entitlement resolution, which is acceptable at this app's scale. If it ever
// shows up in profiling, thread the auth-context email through instead.
func applyProWhitelist(ent *types.Entitlement) {
	if ent == nil || ent.IsPaid() {
		return
	}
	var email string
	if err := db.Model(&types.User{}).
		Select("email").
		Where("user_id = ?", ent.UserID).
		Scan(&email).Error; err != nil {
		// Soft-fail: a lookup error must not break entitlement resolution.
		// Worst case the user is treated as free until the next read.
		return
	}
	if types.IsProWhitelistedEmail(email) {
		ent.Plan = types.PlanPaid
		ent.Source = types.EntitlementSourceComp
	}
}

// GetUserPlanContext returns an entitlement plus active counts in a single call.
// The AI generation counter is reset lazily here when the calendar month has
// rolled over since AIGenerationsPeriodStart.
func GetUserPlanContext(userID string) (*UserPlanContext, error) {
	ent, err := GetEntitlement(userID)
	if err != nil {
		return nil, err
	}

	if shouldResetAIPeriod(ent.AIGenerationsPeriodStart, time.Now().UTC()) {
		now := time.Now().UTC()
		updates := map[string]interface{}{
			"ai_generations_used":         0,
			"ai_generations_period_start": now,
		}
		if err := db.Model(&types.Entitlement{}).
			Where("user_id = ?", userID).
			Updates(updates).Error; err != nil {
			return nil, err
		}
		ent.AIGenerationsUsed = 0
		ent.AIGenerationsPeriodStart = now
	}

	var roadmapCount, habitCount int64
	if err := db.Model(&types.Roadmap{}).
		Where("user_id = ? AND archived = ?", userID, false).
		Count(&roadmapCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&types.Habit{}).
		Where("user_id = ? AND archived = ?", userID, false).
		Count(&habitCount).Error; err != nil {
		return nil, err
	}

	return &UserPlanContext{
		Entitlement:        ent,
		ActiveRoadmapCount: int(roadmapCount),
		ActiveHabitCount:   int(habitCount),
	}, nil
}

// IncrementAIGenerations atomically bumps the user's AI generation counter.
// Callers should invoke this after a successful AI generation completes.
// The counter is reset lazily by GetUserPlanContext when the calendar month rolls.
func IncrementAIGenerations(userID string) error {
	if userID == "" {
		return errors.New("missing user_id")
	}
	// Ensure a row exists first - safer than UPDATE ... WHERE which silently no-ops.
	if _, err := GetEntitlement(userID); err != nil {
		return err
	}
	return db.Model(&types.Entitlement{}).
		Where("user_id = ?", userID).
		UpdateColumn("ai_generations_used", gorm.Expr("ai_generations_used + 1")).
		Error
}

// shouldResetAIPeriod returns true when `now` is in a different calendar month
// (in UTC) than `periodStart`. Exposed for testing.
func shouldResetAIPeriod(periodStart, now time.Time) bool {
	ps := periodStart.UTC()
	n := now.UTC()
	return ps.Year() != n.Year() || ps.Month() != n.Month()
}
