package analytics

// The event catalog. Every product event name the system may emit is declared
// here, and anything not declared is rejected — by Emit on the server side and
// by the /api/v1/events ingest endpoint on the client side.
//
// An allowlist rather than free-form strings is deliberate. Dashboards are
// built on exact event-name matches, so a typo at one call site ("goal_created"
// vs "goals_created") produces a metric that silently undercounts forever, and
// the gap is invisible until someone notices the number is wrong. Failing at
// the boundary makes that a test failure instead of a bad quarter of data.
const (
	// Account and onboarding. Signup completion is server-authoritative (it is
	// the row insert); the started/step events can only come from the client.
	EventAppFirstOpen           = "app_first_open"
	EventAppSessionStarted      = "app_session_started"
	EventSignupStarted          = "signup_started"
	EventSignupCompleted        = "signup_completed"
	EventSignupFailed           = "signup_failed"
	EventLoginCompleted         = "login_completed"
	EventLoginFailed            = "login_failed"
	EventOnboardingStarted      = "onboarding_started"
	EventOnboardingStepComplete = "onboarding_step_completed"
	EventOnboardingCompleted    = "onboarding_completed"
	EventAccountDeleted         = "account_deleted"

	// Goals and roadmaps.
	EventGoalCreated                = "goal_created"
	EventGoalActivated              = "goal_activated"
	EventGoalPaused                 = "goal_paused"
	EventGoalCompleted              = "goal_completed"
	EventGoalAbandoned              = "goal_abandoned"
	EventRoadmapGenerationStarted   = "roadmap_generation_started"
	EventRoadmapGenerationSucceeded = "roadmap_generation_succeeded"
	EventRoadmapGenerationFailed    = "roadmap_generation_failed"
	EventRoadmapActivated           = "roadmap_activated"

	// Goal discovery. Entirely client-driven — the server never sees the
	// intermediate steps of the flow.
	EventGoalDiscoveryStarted   = "goal_discovery_started"
	EventGoalDiscoverySubmitted = "goal_discovery_submitted"
	EventGoalOptionsGenerated   = "goal_options_generated"
	EventGoalOptionSelected     = "goal_option_selected"
	EventGoalDiscoveryAbandoned = "goal_discovery_abandoned"

	// Execution. work_item_became_due / _missed require the occurrence model
	// (see occurrence.go) and are emitted by the scheduler, not by a request.
	EventWorkItemCreated         = "work_item_created"
	EventWorkItemBecameDue       = "work_item_became_due"
	EventWorkItemCompleted       = "work_item_completed"
	EventWorkItemMissed          = "work_item_missed"
	EventWorkItemRescheduled     = "work_item_rescheduled"
	EventWorkItemSkipped         = "work_item_skipped"
	EventWorkItemDeleted         = "work_item_deleted"
	EventWorkItemSubstepComplete = "work_item_substep_completed"

	// Accountability.
	EventAnchorInviteSent             = "anchor_invite_sent"
	EventAnchorInviteAccepted         = "anchor_invite_accepted"
	EventAnchorInviteDeclined         = "anchor_invite_declined"
	EventAnchorInviteExpired          = "anchor_invite_expired"
	EventAnchorRelationshipActivated  = "anchor_relationship_activated"
	EventAnchorRelationshipEnded      = "anchor_relationship_ended"
	EventAccountabilityAlertTrigger   = "accountability_alert_triggered"
	EventAccountabilityAlertDelivered = "accountability_alert_delivered"
	EventAccountabilityAlertFailed    = "accountability_alert_failed"
	EventAccountabilityAlertOpened    = "accountability_alert_opened"
	EventAnchorInterventionRecorded   = "anchor_intervention_recorded"

	// Notifications.
	EventPushPermissionPrompted = "push_permission_prompted"
	EventPushPermissionGranted  = "push_permission_granted"
	EventPushPermissionDenied   = "push_permission_denied"
	EventNotificationQueued     = "notification_queued"
	EventNotificationDelivered  = "notification_delivered"
	EventNotificationFailed     = "notification_failed"
	EventNotificationOpened     = "notification_opened"

	// Subscription. Every event below except paywall_viewed and
	// checkout_started is server-authoritative, sourced from the RevenueCat
	// webhook — the client is never trusted for revenue state.
	EventPaywallViewed           = "paywall_viewed"
	EventCheckoutStarted         = "checkout_started"
	EventTrialStarted            = "trial_started"
	EventSubscriptionActivated   = "subscription_activated"
	EventSubscriptionRenewed     = "subscription_renewed"
	EventSubscriptionPaymentFail = "subscription_payment_failed"
	EventSubscriptionCanceled    = "subscription_canceled"
	EventSubscriptionExpired     = "subscription_expired"
	EventSubscriptionReactivated = "subscription_reactivated"
	EventRefundIssued            = "refund_issued"

	// Telemetry heartbeat. Emitted on a fixed cadence so a gap in this one
	// series distinguishes "nothing happened" from "the pipeline broke".
	EventTelemetryHeartbeat = "telemetry_heartbeat"
)

// knownEvents is the set form of the constants above, used for validation.
var knownEvents = func() map[string]struct{} {
	names := []string{
		EventAppFirstOpen, EventAppSessionStarted, EventSignupStarted,
		EventSignupCompleted, EventSignupFailed, EventLoginCompleted,
		EventLoginFailed, EventOnboardingStarted, EventOnboardingStepComplete,
		EventOnboardingCompleted, EventAccountDeleted,

		EventGoalCreated, EventGoalActivated, EventGoalPaused,
		EventGoalCompleted, EventGoalAbandoned, EventRoadmapGenerationStarted,
		EventRoadmapGenerationSucceeded, EventRoadmapGenerationFailed,
		EventRoadmapActivated,

		EventGoalDiscoveryStarted, EventGoalDiscoverySubmitted,
		EventGoalOptionsGenerated, EventGoalOptionSelected,
		EventGoalDiscoveryAbandoned,

		EventWorkItemCreated, EventWorkItemBecameDue, EventWorkItemCompleted,
		EventWorkItemMissed, EventWorkItemRescheduled, EventWorkItemSkipped,
		EventWorkItemDeleted, EventWorkItemSubstepComplete,

		EventAnchorInviteSent, EventAnchorInviteAccepted,
		EventAnchorInviteDeclined, EventAnchorInviteExpired,
		EventAnchorRelationshipActivated, EventAnchorRelationshipEnded,
		EventAccountabilityAlertTrigger, EventAccountabilityAlertDelivered,
		EventAccountabilityAlertFailed, EventAccountabilityAlertOpened,
		EventAnchorInterventionRecorded,

		EventPushPermissionPrompted, EventPushPermissionGranted,
		EventPushPermissionDenied, EventNotificationQueued,
		EventNotificationDelivered, EventNotificationFailed,
		EventNotificationOpened,

		EventPaywallViewed, EventCheckoutStarted, EventTrialStarted,
		EventSubscriptionActivated, EventSubscriptionRenewed,
		EventSubscriptionPaymentFail, EventSubscriptionCanceled,
		EventSubscriptionExpired, EventSubscriptionReactivated,
		EventRefundIssued,

		EventTelemetryHeartbeat,
	}
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}()

// IsKnownEvent reports whether name is in the catalog.
func IsKnownEvent(name string) bool {
	_, ok := knownEvents[name]
	return ok
}

// clientEmittable is the subset of events the mobile app is allowed to report.
// Everything else is server-authoritative: the client may not claim a signup
// completed, an action was finished, or a subscription activated, because
// those numbers decide whether the business is working. A client that could
// assert them could also be wrong about them — through a retry, a stale queue
// flushing days later, or a tampered build.
var clientEmittable = func() map[string]struct{} {
	names := []string{
		EventAppFirstOpen, EventAppSessionStarted, EventSignupStarted,
		EventSignupFailed, EventLoginFailed, EventOnboardingStarted,
		EventOnboardingStepComplete,

		EventGoalDiscoveryStarted, EventGoalDiscoverySubmitted,
		EventGoalOptionsGenerated, EventGoalOptionSelected,
		EventGoalDiscoveryAbandoned,

		EventPushPermissionPrompted, EventPushPermissionGranted,
		EventPushPermissionDenied, EventNotificationOpened,
		EventAccountabilityAlertOpened,

		EventPaywallViewed, EventCheckoutStarted,
	}
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}()

// IsClientEmittable reports whether the mobile client may report this event.
func IsClientEmittable(name string) bool {
	_, ok := clientEmittable[name]
	return ok
}

// allowedProperties is the allowlist for the `properties` bag. Keys outside it
// are dropped before the event is written.
//
// This is the enforcement point for the privacy rules, and it is an allowlist
// rather than a denylist on purpose: a denylist of "goal_title, notes,
// prompt, ..." only blocks the leaks someone thought of in advance, and the
// cost of being wrong is PII sitting in a 24-month retention store. Anything
// genuinely needed gets added here in a reviewed diff.
//
// Deliberately absent, and not to be added: any free text a user or the model
// wrote — goal titles and descriptions, notes, anchor messages, AI prompts,
// generated roadmap content, notification bodies — plus names, emails, phone
// numbers, tokens, and raw request or response bodies. Identifiers correlate
// back to the primary database when a human legitimately needs the content.
var allowedProperties = map[string]struct{}{
	// Entity identifiers. Opaque ids only, never the content they point at.
	"goal_id":                {},
	"roadmap_id":             {},
	"milestone_id":           {},
	"task_id":                {},
	"work_item_id":           {},
	"occurrence_id":          {},
	"anchor_relationship_id": {},
	"job_id":                 {},

	// Classification and outcome.
	"item_type":     {},
	"outcome":       {},
	"error_code":    {},
	"status":        {},
	"reason":        {},
	"anchor_active": {},
	"source_screen": {},
	"step":          {},
	"step_index":    {},

	// Timing and counts. Numbers, never payloads.
	"duration_ms":    {},
	"generation_ms":  {},
	"retry_count":    {},
	"count":          {},
	"days_since_due": {},
	"recovery_hours": {},
	"scheduled_date": {},
	"completed_date": {},

	// AI operational metadata.
	"provider": {},
	"model":    {},

	// Notifications.
	"notification_type":    {},
	"notification_channel": {},

	// Subscription. Amounts and plan shape, never payment instruments.
	"plan_type":        {},
	"billing_period":   {},
	"billing_provider": {},
	"store":            {},
	"product_id":       {},
	"price_amount":     {},
	"price_currency":   {},
	"is_trial":         {},

	// Acquisition and experiments. Reserved now so the schema does not change
	// when attribution lands; nothing populates them yet.
	"acquisition_source":   {},
	"acquisition_campaign": {},
	"experiment_id":        {},
	"experiment_variant":   {},
}

// IsAllowedProperty reports whether key may appear in an event's properties.
func IsAllowedProperty(key string) bool {
	_, ok := allowedProperties[key]
	return ok
}

// Item types for work_item_* events.
const (
	ItemTypeRoadmapStep   = "roadmap_step"
	ItemTypeHabit         = "habit"
	ItemTypeRecurringTask = "recurring_task"
	ItemTypeOneTimeTask   = "one_time_task"
)
