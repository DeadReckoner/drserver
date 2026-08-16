package handlers

import (
	"context"
	"strings"

	"dr/server/analytics"
	"dr/server/webhooks/revenuecat"
)

// emitSubscriptionEvent records the product-analytics event for a RevenueCat
// webhook, if the event maps to one.
//
// The store webhook is the authoritative source for every revenue number.
// The client knows a purchase sheet was dismissed; it does not know whether
// the charge settled, whether a trial converted, or whether a refund landed
// three weeks later. Sourcing subscription state from the client would make
// the revenue dashboard disagree with the actual deposits.
//
// Called after the event has been applied and recorded, so duplicates (RC
// retries the same event id) have already short-circuited upstream and cannot
// double-count here.
func emitSubscriptionEvent(ctx context.Context, evt revenuecat.Event, userID string) {
	// Sandbox purchases are not revenue. They arrive on the same webhook from
	// TestFlight builds and RC's test button, and would otherwise show up as
	// trials and activations that nobody paid for.
	if evt.IsSandbox() || evt.Type == revenuecat.EventTypeTest {
		return
	}

	name, ok := subscriptionEventName(evt)
	if !ok {
		return
	}

	// The webhook arrives on its own connection with no user session behind
	// it, so the identity has to be attached explicitly rather than inherited
	// from a request.
	if userID != "" {
		ctx = analytics.WithUserID(ctx, userID)
	}

	props := analytics.Props(
		"billing_provider", "revenuecat",
		"store", evt.Store,
		"product_id", evt.ProductID,
		"plan_type", planTypeFor(evt.ProductID),
		"billing_period", billingPeriodFor(evt.ProductID),
		"is_trial", evt.PeriodType == revenuecat.PeriodTypeTrial,
	)

	// Amounts only where RC reported one. Zero-price events (cancellations,
	// expirations) would otherwise contribute a spurious 0 to revenue
	// aggregations and drag averages down.
	if evt.Price > 0 {
		props["price_amount"] = evt.Price
		props["price_currency"] = "USD"
	} else if evt.PriceInPurchasedCurrency > 0 && evt.Currency != "" {
		props["price_amount"] = evt.PriceInPurchasedCurrency
		props["price_currency"] = evt.Currency
	}

	if reason := cancellationReason(evt); reason != "" {
		props["reason"] = reason
	}

	analytics.Emit(ctx, name, props)
}

// subscriptionEventName maps an RC event type onto the product event catalog.
//
// The mapping is not one-to-one, and the places it bends are the places the
// revenue numbers are usually wrong:
//
//   - An INITIAL_PURCHASE that starts a trial is a trial start, not an
//     activation. Counting it as an activation makes trial-to-paid conversion
//     look like 100%.
//   - A RENEWAL flagged as a trial conversion is the activation. That is the
//     moment money first moves, and it is a different event from the routine
//     renewals that follow.
//   - PRODUCT_CHANGE is a plan switch, not a lifecycle transition. It maps to
//     nothing, because counting it as an activation would double-count a
//     subscriber who upgraded.
func subscriptionEventName(evt revenuecat.Event) (string, bool) {
	switch evt.Type {
	case revenuecat.EventTypeInitialPurchase:
		if evt.PeriodType == revenuecat.PeriodTypeTrial {
			return analytics.EventTrialStarted, true
		}
		return analytics.EventSubscriptionActivated, true

	case revenuecat.EventTypeRenewal:
		if evt.IsTrialConversion {
			return analytics.EventSubscriptionActivated, true
		}
		return analytics.EventSubscriptionRenewed, true

	case revenuecat.EventTypeNonRenewingPurchase:
		return analytics.EventSubscriptionActivated, true

	case revenuecat.EventTypeCancellation:
		return analytics.EventSubscriptionCanceled, true

	case revenuecat.EventTypeUncancellation:
		return analytics.EventSubscriptionReactivated, true

	case revenuecat.EventTypeExpiration:
		return analytics.EventSubscriptionExpired, true

	case revenuecat.EventTypeBillingIssue:
		return analytics.EventSubscriptionPaymentFail, true

	case revenuecat.EventTypeRefund:
		return analytics.EventRefundIssued, true
	}

	// SUBSCRIBER_ALIAS, TRANSFER, SUBSCRIPTION_PAUSED, PRODUCT_CHANGE: real
	// events, but not lifecycle transitions. Recorded in entitlement_events
	// for audit; deliberately absent from the funnel.
	return "", false
}

// planTypeFor maps a store product id to the plan tier. Kept separate from
// billing period so "how many Pro subscribers" and "what is the monthly/annual
// mix" are independent questions.
func planTypeFor(productID string) string {
	if productID == "" {
		return "unknown"
	}
	if strings.Contains(productID, "pro") {
		return "pro"
	}
	return "unknown"
}

// billingPeriodFor derives monthly vs annual from the product id. Product ids
// are set in App Store Connect, so this needs updating when they change —
// which is why unrecognized ids report "unknown" rather than guessing.
func billingPeriodFor(productID string) string {
	switch {
	case strings.Contains(productID, "annual"), strings.Contains(productID, "yearly"):
		return "annual"
	case strings.Contains(productID, "monthly"):
		return "monthly"
	}
	return "unknown"
}

// cancellationReason returns RC's reason code for a cancellation or
// expiration. These are a closed vocabulary from RC (UNSUBSCRIBE,
// BILLING_ERROR, ...), so they are safe as a dashboard dimension — a voluntary
// churn and a failed card are the same row in a naive cancellation count, and
// they call for completely different responses.
func cancellationReason(evt revenuecat.Event) string {
	if evt.CancelReason != "" {
		return strings.ToLower(evt.CancelReason)
	}
	if evt.ExpirationReason != "" {
		return strings.ToLower(evt.ExpirationReason)
	}
	return ""
}
