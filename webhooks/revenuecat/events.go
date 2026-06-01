// Package revenuecat models the RevenueCat webhook payload format and the
// logic to apply each event type to the entitlements table. Only the fields
// we actually consume are modeled; RC's full payload has dozens more that
// flow through unparsed and are preserved verbatim in entitlement_events.payload.
//
// Reference: https://www.revenuecat.com/docs/webhooks/event-types-and-fields
package revenuecat

// Envelope is the top-level RevenueCat webhook payload.
type Envelope struct {
	Event      Event  `json:"event"`
	APIVersion string `json:"api_version"`
}

// Event is the inner event object. Fields are present or absent depending on
// Type; using zero-values for missing fields is fine because the applier
// branches on Type before reading anything specific.
type Event struct {
	// ID is the idempotency key. RC guarantees uniqueness per logical event;
	// retries of the same logical event reuse the same ID.
	ID string `json:"id"`

	// Type is the event class - see the EventType* constants below.
	Type string `json:"type"`

	// EventTimestampMs is when the event occurred per RC's clock, in ms
	// since epoch. Used for stale-event detection.
	EventTimestampMs int64 `json:"event_timestamp_ms"`

	// AppUserID is the identifier we set via Purchases.logIn() on the
	// client. Should match a DR user_id (UUID string). May be a RC-
	// generated anonymous ID if logIn was never called - those events get
	// recorded as unmapped.
	AppUserID string `json:"app_user_id"`

	// Aliases lists alternate app_user_ids that have been linked to this
	// subscriber via Purchases.logIn() called from different sessions. We
	// fall back to scanning these if AppUserID itself is anonymous.
	Aliases []string `json:"aliases,omitempty"`

	// OriginalAppUserID is the very first identifier RC saw for this
	// subscriber - useful as a final fallback.
	OriginalAppUserID string `json:"original_app_user_id,omitempty"`

	// ProductID is the store product identifier (e.g. dr_pro_monthly).
	ProductID string `json:"product_id"`

	// NewProductID appears on PRODUCT_CHANGE events.
	NewProductID string `json:"new_product_id,omitempty"`

	// Store is the source platform: APP_STORE / PLAY_STORE / STRIPE /
	// PROMOTIONAL / MAC_APP_STORE / AMAZON.
	Store string `json:"store"`

	// Environment is SANDBOX or PRODUCTION. We accept both; production
	// monitoring should alert on sandbox events in prod.
	Environment string `json:"environment"`

	// PeriodType is TRIAL / INTRO / NORMAL / PROMOTIONAL. We map TRIAL to
	// status=trialing on initial purchase.
	PeriodType string `json:"period_type"`

	// PurchasedAtMs is when the current period began (start of trial,
	// renewal, or initial purchase).
	PurchasedAtMs int64 `json:"purchased_at_ms"`

	// ExpirationAtMs is when the current paid access ends. For CANCELLATION
	// this is the future date through which access remains valid; for
	// EXPIRATION it's the past date when access lapsed.
	ExpirationAtMs int64 `json:"expiration_at_ms"`

	// EntitlementIDs lists the RC entitlement IDs this event applies to.
	// We only care about events that touch the "pro" entitlement.
	EntitlementIDs []string `json:"entitlement_ids,omitempty"`

	// EntitlementID is the singular form, set on older event versions. We
	// check both.
	EntitlementID string `json:"entitlement_id,omitempty"`

	// CancelReason populated on CANCELLATION. Values: UNSUBSCRIBE,
	// BILLING_ERROR, DEVELOPER_INITIATED, PRICE_INCREASE, CUSTOMER_SUPPORT,
	// UNKNOWN.
	CancelReason string `json:"cancel_reason,omitempty"`

	// ExpirationReason populated on EXPIRATION. Values: UNSUBSCRIBE,
	// BILLING_ERROR, DEVELOPER_INITIATED, PRICE_INCREASE, CUSTOMER_SUPPORT,
	// UNKNOWN.
	ExpirationReason string `json:"expiration_reason,omitempty"`

	// IsTrialConversion set true on RENEWAL events that mark the trial-to-
	// paid transition. Lets us distinguish a trial conversion from a normal
	// renewal of an already-paid sub.
	IsTrialConversion bool `json:"is_trial_conversion,omitempty"`

	// OriginalTransactionID identifies the subscriber's underlying
	// subscription across renewals. Maps to entitlements.provider_subscription_id.
	OriginalTransactionID string `json:"original_transaction_id,omitempty"`

	// TransactionID is the specific transaction (one per renewal).
	TransactionID string `json:"transaction_id,omitempty"`
}

// Event types we handle explicitly. Anything not in this list is recorded
// for audit but applied as a no-op.
const (
	EventTypeInitialPurchase     = "INITIAL_PURCHASE"
	EventTypeNonRenewingPurchase = "NON_RENEWING_PURCHASE"
	EventTypeRenewal             = "RENEWAL"
	EventTypeProductChange       = "PRODUCT_CHANGE"
	EventTypeCancellation        = "CANCELLATION"
	EventTypeUncancellation      = "UNCANCELLATION"
	EventTypeExpiration          = "EXPIRATION"
	EventTypeBillingIssue        = "BILLING_ISSUE"
	EventTypeRefund              = "REFUND"
	EventTypeSubscriberAlias     = "SUBSCRIBER_ALIAS"
	EventTypeTransfer            = "TRANSFER"
	EventTypeSubscriptionPaused  = "SUBSCRIPTION_PAUSED"
	EventTypeTest                = "TEST"
)

// Store values from RC.
const (
	StoreAppStore    = "APP_STORE"
	StoreMacAppStore = "MAC_APP_STORE"
	StorePlayStore   = "PLAY_STORE"
	StoreAmazon      = "AMAZON"
	StoreStripe      = "STRIPE"
	StorePromotional = "PROMOTIONAL"
)

// PeriodType values.
const (
	PeriodTypeTrial       = "TRIAL"
	PeriodTypeIntro       = "INTRO"
	PeriodTypeNormal      = "NORMAL"
	PeriodTypePromotional = "PROMOTIONAL"
)

// Apply status values mirrored in entitlement_events.apply_status.
const (
	ApplyStatusApplied      = "applied"
	ApplyStatusNoopStale    = "noop_stale"
	ApplyStatusNoopUnknown  = "noop_unknown"
	ApplyStatusNoopUnmapped = "noop_unmapped"
	ApplyStatusError        = "error"
)

// MapStoreToSource translates RC's Store value to the internal entitlements.source
// value. Returns "" for unknown stores; the applier treats that as a soft error
// and refuses to update.
func MapStoreToSource(store string) string {
	switch store {
	case StoreAppStore, StoreMacAppStore:
		return "apple"
	case StorePlayStore, StoreAmazon:
		// Amazon Appstore subscriptions are functionally equivalent to Play
		// for our gating purposes; collapse for simplicity until/unless we
		// need to distinguish.
		return "google"
	case StoreStripe:
		return "stripe"
	case StorePromotional:
		// PROMOTIONAL on RC corresponds to admin-granted entitlements (RC's
		// dashboard "Promotional entitlements" feature). Treat like our
		// internal comp source.
		return "comp"
	}
	return ""
}
