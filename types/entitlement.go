package types

import "time"

// Entitlement holds plan/subscription state and usage counters for a single user.
// One row per user, primary key is user_id. Created automatically by a DB trigger
// on user insert and backfilled for existing users by the initial migration.
//
// All billing-related fields live here rather than on User to keep the user model
// clean, support narrow RLS policies, and isolate the webhook write path.
type Entitlement struct {
	UserID string `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`

	Plan   string `gorm:"column:plan;default:free;not null" json:"plan"`
	Source string `gorm:"column:source;default:default;not null" json:"source"`

	// Subscription state. Null for default/free users; populated by webhook
	// handlers once a paid subscription exists.
	Status             *string    `gorm:"column:status" json:"status,omitempty"`
	CurrentPeriodStart *time.Time `gorm:"column:current_period_start" json:"current_period_start,omitempty"`
	CurrentPeriodEnd   *time.Time `gorm:"column:current_period_end" json:"current_period_end,omitempty"`
	TrialEndsAt        *time.Time `gorm:"column:trial_ends_at" json:"trial_ends_at,omitempty"`
	CancelledAt        *time.Time `gorm:"column:cancelled_at" json:"cancelled_at,omitempty"`

	// Provider linkage. Internal only - never returned in API responses.
	ProviderCustomerID     *string `gorm:"column:provider_customer_id" json:"-"`
	ProviderSubscriptionID *string `gorm:"column:provider_subscription_id" json:"-"`
	ProductID              *string `gorm:"column:product_id" json:"product_id,omitempty"`

	// Usage counters. Reset by app logic on calendar month boundary.
	AIGenerationsUsed        int       `gorm:"column:ai_generations_used;default:0;not null" json:"-"`
	AIGenerationsPeriodStart time.Time `gorm:"column:ai_generations_period_start;not null" json:"-"`

	CreatedAt time.Time `gorm:"column:created_at" json:"-"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"-"`
}

// TableName overrides the GORM default of "entitlement" (singular pluralisation).
func (Entitlement) TableName() string {
	return "entitlements"
}

// IsPaid returns true if the entitlement grants paid-plan access.
// Trial users on a paid product are still considered paid for gating purposes;
// the distinction matters only for analytics and is captured in Status.
func (e *Entitlement) IsPaid() bool {
	return e.Plan == PlanPaid
}

// CanUseFeature returns whether the entitlement grants access to a boolean feature.
// Today every paid-only feature simply requires PlanPaid; this signature gives us
// room to add per-feature overrides (beta access, comps, A/B tests) later without
// changing call sites.
func (e *Entitlement) CanUseFeature(_ FeatureKey) bool {
	return e.IsPaid()
}

// Limits returns the numeric caps applied to this entitlement's plan.
func (e *Entitlement) Limits() PlanLimits {
	return LimitsForPlan(e.Plan)
}
