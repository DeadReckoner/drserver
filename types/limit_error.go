package types

import "fmt"

// PlanLimitError describes a plan-gated rejection. Returned by the
// database/plan_check helpers when a user is blocked by their plan, and
// rendered to clients by api/utils.PlanLimitResponse as a structured 402
// payload.
//
// The mobile client looks at Code to decide which paywall variant to render
// and uses Feature to format the upgrade prompt.
type PlanLimitError struct {
	// Code identifies the rejection class. Currently always "plan_limit" but
	// reserved for future codes like "trial_required", "fair_use", etc.
	Code string `json:"code"`

	// Feature names the gated thing in machine form (e.g. "active_roadmaps",
	// "active_habits", "custom_reminders"). Stable; mobile keys off it.
	Feature string `json:"feature"`

	// Limit is the cap that was hit. Zero for paid-only features that have no
	// numeric limit; -1 is never returned (only used internally).
	Limit int `json:"limit"`

	// Current is the user's current value at the time of the check (e.g. 1
	// active roadmap when the limit is 1). Helps the client message
	// "you have 3/3 habits" without an extra round trip.
	Current int `json:"current"`

	// Message is a default human-readable string. The client is free to
	// override with localized copy keyed off Feature.
	Message string `json:"message"`
}

// Error implements the error interface so plan checks can be returned and
// compared like any other error.
func (e *PlanLimitError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("plan limit hit: %s (limit=%d, current=%d)", e.Feature, e.Limit, e.Current)
}
