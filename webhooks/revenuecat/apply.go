package revenuecat

import (
	"errors"
	"fmt"
	"time"

	"dr/server/types"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Apply mutates the entitlements row for the user identified in evt to
// reflect the lifecycle change described by the event. Pure function over
// (tx, evt): no side effects outside the supplied *gorm.DB.
//
// Returns an ApplyStatus* string suitable for entitlement_events.apply_status
// and an error. Errors are returned to the caller for logging and DB
// recording but the handler will still return 200 to RC so the event is not
// retried forever - external retry would just re-corrupt state.
//
// Behavior matrix (see also docs/BILLING_API_CONTRACT.md):
//
//	INITIAL_PURCHASE        -> plan=paid, status=trialing|active, set period + product
//	RENEWAL                 -> bump period dates; status may flip trialing->active if IsTrialConversion
//	UNCANCELLATION          -> clear cancelled_at, status=active
//	PRODUCT_CHANGE          -> swap product_id, update period_end
//	CANCELLATION            -> mark cancelled_at, status=cancelled; access stays paid until period_end
//	EXPIRATION              -> plan=free, status=expired, clear paid fields
//	BILLING_ISSUE           -> status=in_grace, plan stays paid
//	REFUND                  -> immediate plan=free, status=expired
//	SUBSCRIBER_ALIAS/TRANSFER -> log only (no-op); cross-account relinking is rare and not auto-resolved
//	TEST                    -> log only
//	(anything else)         -> noop_unknown
func Apply(tx *gorm.DB, evt Event) (string, error) {
	if !touchesProEntitlement(evt) {
		// Event is for an entitlement we don't gate on (e.g. a secondary
		// entitlement we haven't defined). Log and skip.
		return ApplyStatusNoopUnknown, nil
	}

	userID := ResolveUserID(evt)
	if userID == "" {
		return ApplyStatusNoopUnmapped, nil
	}

	source := MapStoreToSource(evt.Store)
	if source == "" {
		return ApplyStatusNoopUnknown, fmt.Errorf("unknown store %q", evt.Store)
	}

	// Pre-check stale ordering against the row's updated_at - if a newer
	// event has already been recorded for this user, skip applying this one
	// to avoid clobbering a more recent state.
	var ent types.Entitlement
	switch err := tx.Where("user_id = ?", userID).First(&ent).Error; {
	case err == nil:
		eventTime := time.UnixMilli(evt.EventTimestampMs)
		if !ent.UpdatedAt.IsZero() && eventTime.Before(ent.UpdatedAt) {
			return ApplyStatusNoopStale, nil
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		// No entitlement row yet - the user was inserted but the auto-create
		// trigger didn't fire (or we're applying for a user that hasn't been
		// created yet). The trigger guarantees this won't happen in normal
		// flow; treat as unmapped.
		return ApplyStatusNoopUnmapped, nil
	default:
		return ApplyStatusError, err
	}

	switch evt.Type {
	case EventTypeInitialPurchase:
		return applyInitialPurchase(tx, userID, source, evt)
	case EventTypeRenewal, EventTypeProductChange:
		return applyRenewalOrProductChange(tx, userID, source, evt)
	case EventTypeUncancellation:
		return applyUncancellation(tx, userID)
	case EventTypeCancellation:
		return applyCancellation(tx, userID, evt)
	case EventTypeExpiration:
		return applyExpiration(tx, userID, evt)
	case EventTypeBillingIssue:
		return applyBillingIssue(tx, userID)
	case EventTypeRefund:
		return applyRefund(tx, userID)
	case EventTypeNonRenewingPurchase:
		// One-time purchase; we don't sell those today. Recorded for audit.
		return ApplyStatusNoopUnknown, nil
	case EventTypeSubscriberAlias, EventTypeTransfer, EventTypeSubscriptionPaused, EventTypeTest:
		// Recorded for audit, no entitlement change.
		return ApplyStatusNoopUnknown, nil
	default:
		return ApplyStatusNoopUnknown, nil
	}
}

// ResolveUserID picks the most authoritative app_user_id from the event.
// RC sends the current ID first, with aliases for historical identifiers. We
// accept any value that parses as a UUID - that's our DR user_id format.
func ResolveUserID(evt Event) string {
	candidates := append([]string{evt.AppUserID}, evt.Aliases...)
	if evt.OriginalAppUserID != "" {
		candidates = append(candidates, evt.OriginalAppUserID)
	}
	for _, c := range candidates {
		if _, err := uuid.Parse(c); err == nil {
			return c
		}
	}
	return ""
}

// touchesProEntitlement returns true when the event's entitlement_id (or any
// of entitlement_ids) is the "pro" entitlement we gate on. Events for other
// entitlements (e.g. one-shot purchases or experiments) are recorded but do
// not mutate our pro plan state.
func touchesProEntitlement(evt Event) bool {
	const pro = "pro"
	if evt.EntitlementID == pro {
		return true
	}
	for _, id := range evt.EntitlementIDs {
		if id == pro {
			return true
		}
	}
	// Some event types (REFUND, EXPIRATION, etc.) omit entitlement_ids;
	// treat absence as "applies to whatever the user had". A user that
	// never had pro can't have an expiration for it, so this is safe.
	return len(evt.EntitlementIDs) == 0 && evt.EntitlementID == ""
}

// --- Per-event-type appliers --------------------------------------------------

func applyInitialPurchase(tx *gorm.DB, userID, source string, evt Event) (string, error) {
	status := "active"
	var trialEnds *time.Time
	if evt.PeriodType == PeriodTypeTrial {
		status = "trialing"
		if evt.ExpirationAtMs > 0 {
			t := time.UnixMilli(evt.ExpirationAtMs)
			trialEnds = &t
		}
	}
	periodStart := timePtr(evt.PurchasedAtMs)
	periodEnd := timePtr(evt.ExpirationAtMs)

	updates := map[string]interface{}{
		"plan":                     types.PlanPaid,
		"source":                   source,
		"status":                   status,
		"product_id":               nullableString(evt.ProductID),
		"current_period_start":     periodStart,
		"current_period_end":       periodEnd,
		"trial_ends_at":            trialEnds,
		"cancelled_at":             nil,
		"provider_customer_id":     nullableString(evt.AppUserID),
		"provider_subscription_id": nullableString(evt.OriginalTransactionID),
	}
	return applyUpdates(tx, userID, updates)
}

func applyRenewalOrProductChange(tx *gorm.DB, userID, source string, evt Event) (string, error) {
	productID := evt.ProductID
	if evt.NewProductID != "" {
		productID = evt.NewProductID
	}

	// Coming out of trial: status flips trialing->active and trial_ends_at clears.
	status := "active"
	updates := map[string]interface{}{
		"plan":                 types.PlanPaid,
		"source":               source,
		"status":               status,
		"product_id":           nullableString(productID),
		"current_period_start": timePtr(evt.PurchasedAtMs),
		"current_period_end":   timePtr(evt.ExpirationAtMs),
	}
	if evt.IsTrialConversion {
		updates["trial_ends_at"] = nil
	}
	if evt.OriginalTransactionID != "" {
		updates["provider_subscription_id"] = evt.OriginalTransactionID
	}
	return applyUpdates(tx, userID, updates)
}

func applyUncancellation(tx *gorm.DB, userID string) (string, error) {
	updates := map[string]interface{}{
		"status":       "active",
		"cancelled_at": nil,
	}
	return applyUpdates(tx, userID, updates)
}

func applyCancellation(tx *gorm.DB, userID string, evt Event) (string, error) {
	cancelledAt := time.UnixMilli(evt.EventTimestampMs)
	updates := map[string]interface{}{
		"status":       "cancelled",
		"cancelled_at": &cancelledAt,
		// Keep plan=paid and current_period_end intact; the user retains
		// access through the existing period. EXPIRATION will flip plan
		// when the period actually ends.
	}
	return applyUpdates(tx, userID, updates)
}

func applyExpiration(tx *gorm.DB, userID string, evt Event) (string, error) {
	updates := map[string]interface{}{
		"plan":                 types.PlanFree,
		"status":               "expired",
		"product_id":           nil,
		"current_period_start": nil,
		"current_period_end":   nil,
		"trial_ends_at":        nil,
		// Keep source so we know which store they came from; useful for
		// "manage subscription" deep-links and analytics. Do NOT clear
		// provider_customer_id / provider_subscription_id so we can re-link
		// if they resubscribe.
	}
	_ = evt // evt.ExpirationReason recorded in payload for audit
	return applyUpdates(tx, userID, updates)
}

func applyBillingIssue(tx *gorm.DB, userID string) (string, error) {
	// Plan stays paid through the grace period. Apple grants ~16 days; Google
	// is similar. EXPIRATION will fire if grace runs out.
	updates := map[string]interface{}{
		"status": "in_grace",
	}
	return applyUpdates(tx, userID, updates)
}

func applyRefund(tx *gorm.DB, userID string) (string, error) {
	// Refunds are retroactive - immediate revocation. Distinct from
	// CANCELLATION which honors the paid period.
	updates := map[string]interface{}{
		"plan":                 types.PlanFree,
		"status":               "expired",
		"product_id":           nil,
		"current_period_start": nil,
		"current_period_end":   nil,
		"trial_ends_at":        nil,
	}
	return applyUpdates(tx, userID, updates)
}

// applyUpdates runs the actual UPDATE and translates row-count to status.
func applyUpdates(tx *gorm.DB, userID string, updates map[string]interface{}) (string, error) {
	res := tx.Model(&types.Entitlement{}).
		Where("user_id = ?", userID).
		Updates(updates)
	if res.Error != nil {
		return ApplyStatusError, res.Error
	}
	if res.RowsAffected == 0 {
		// Pre-flight read already proved the row exists; zero rows here is
		// unexpected but we don't want to error - treat as unmapped.
		return ApplyStatusNoopUnmapped, nil
	}
	return ApplyStatusApplied, nil
}

// timePtr converts an ms-since-epoch to *time.Time, or nil for zero.
func timePtr(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

// nullableString returns nil for empty strings so Gorm writes NULL.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
