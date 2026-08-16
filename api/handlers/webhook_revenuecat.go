package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"dr/server/database"
	"dr/server/email"
	"dr/server/log"
	"dr/server/types"
	"dr/server/webhooks/revenuecat"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// WebhookHandler bundles all external-webhook endpoints. Today only RC; room
// to grow (Stripe, Apple ASSN direct, etc.) without expanding handler count.
type WebhookHandler struct {
	db     *gorm.DB
	mailer *email.Dispatcher
}

func NewWebhookHandler(db *gorm.DB, mailer *email.Dispatcher) *WebhookHandler {
	return &WebhookHandler{db: db, mailer: mailer}
}

// RevenueCatWebhook handles POST /webhooks/revenuecat.
//
// Flow:
//  1. Read raw body (for parsing AND for storage in the events log).
//  2. Verify the shared-secret Authorization header.
//  3. Parse the envelope.
//  4. Short-circuit if event_id has already been recorded (RC retries).
//  5. Inside a single tx: call Apply, then INSERT into entitlement_events.
//  6. Return 200 unless the auth/setup is wrong - we deliberately never 4xx
//     on application errors so RC doesn't enter an infinite retry loop.
//
// Status code matrix:
//
//	200 - applied / no-op / unknown-event / unmapped-user (recorded for audit)
//	401 - bad auth header (do NOT 5xx; RC will retry with the same bad header)
//	503 - REVENUECAT_WEBHOOK_SECRET not configured
//	500 - DB error reading the body or persisting the event
func (h *WebhookHandler) RevenueCatWebhook(c *gin.Context) {
	ctx := c.Request.Context()

	// 1. Read raw body.
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Error(ctx, "RC webhook: read body", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read body"})
		return
	}

	// 2. Verify auth.
	switch err := revenuecat.VerifyAuth(c.GetHeader("Authorization")); {
	case errors.Is(err, revenuecat.ErrSecretNotConfigured):
		log.Failure(ctx, log.FailureBilling, "secret_not_configured", err,
			slog.String("provider", "revenuecat"))
		c.JSON(http.StatusServiceUnavailable,
			gin.H{"error": "webhook secret not configured"})
		return
	case errors.Is(err, revenuecat.ErrAuthMismatch):
		log.Failure(ctx, log.FailureBilling, log.CodeUnauthorized, nil,
			slog.String("provider", "revenuecat"))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	case err != nil:
		log.Error(ctx, "RC webhook: verify", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 3. Parse envelope.
	var env revenuecat.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		log.Warn(ctx, "RC webhook: invalid JSON", err.Error())
		// RC will retry on 4xx, but a malformed payload won't fix itself.
		// 200 + log is the right behavior - we have the raw body in logs.
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "invalid json"})
		return
	}
	if env.Event.ID == "" {
		log.Warn(ctx, "RC webhook: missing event.id; ignoring")
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "missing event id"})
		return
	}

	// 4. Idempotency check (outside tx - cheaper).
	exists, err := database.EntitlementEventExists(h.db, env.Event.ID)
	if err != nil {
		log.Failure(ctx, log.FailureDatabase, log.ErrorCode(err), err,
			slog.String("stage", "rc_idempotency_lookup"))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "idempotency check"})
		return
	}
	if exists {
		log.Info(ctx, "RC webhook: duplicate event ignored", env.Event.ID)
		c.JSON(http.StatusOK, gin.H{"status": "duplicate"})
		return
	}

	// 5. Apply + persist atomically.
	var (
		applyStatus string
		applyErrMsg string
		userIDPtr   *string
	)
	txErr := h.db.Transaction(func(tx *gorm.DB) error {
		s, applyErr := revenuecat.Apply(tx, env.Event)
		applyStatus = s
		if applyErr != nil {
			applyErrMsg = applyErr.Error()
			// Don't roll back the event log just because apply failed - we
			// want the record so we can debug. Mark status=error in the row
			// and proceed to insert.
			applyStatus = revenuecat.ApplyStatusError
		}

		// Only FK-link the audit row to user_id when the user actually
		// exists. Three real cases land us here with a well-formed UUID
		// that's NOT in our users table:
		//   1. RC dashboard "Send Test Event" (uses a canned demo user_id)
		//   2. A real event for a user we've deleted between purchase and
		//      delivery
		//   3. RC anonymous fallbacks that happen to parse as UUID
		// In all three we still want to record the event for audit, but
		// without violating the entitlement_events.user_id FK constraint.
		if uid := revenuecat.ResolveUserID(env.Event); uid != "" {
			exists, err := userExists(tx, uid)
			if err != nil {
				return err
			}
			if exists {
				userIDPtr = &uid
			}
		}

		evtRow := types.EntitlementEvent{
			EventID:          env.Event.ID,
			UserID:           userIDPtr,
			Provider:         "revenuecat",
			Store:            env.Event.Store,
			EventType:        env.Event.Type,
			ProductID:        env.Event.ProductID,
			EventTimestampMs: env.Event.EventTimestampMs,
			Payload:          types.JSONRaw(body),
			ApplyStatus:      applyStatus,
			ApplyError:       applyErrMsg,
		}
		return database.InsertEntitlementEvent(tx, &evtRow)
	})
	if txErr != nil {
		log.Failure(ctx, log.FailureBilling, log.ErrorCode(txErr), txErr,
			slog.String("provider", "revenuecat"),
			slog.String("stage", "apply_and_persist"),
			slog.String("event_type", env.Event.Type))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist"})
		return
	}

	log.Info(ctx, "RC webhook: handled",
		"event_id="+env.Event.ID, "type="+env.Event.Type, "apply="+applyStatus)

	// Emitted after the transaction commits, so the product event and the
	// entitlement state can never disagree — a rolled-back apply produces no
	// event. userIDPtr is nil for events we could not map to a DR user; those
	// still emit, without a user id, so revenue totals stay complete even
	// though those rows drop out of per-user cohort analysis.
	var analyticsUserID string
	if userIDPtr != nil {
		analyticsUserID = *userIDPtr
	}
	emitSubscriptionEvent(ctx, env.Event, analyticsUserID)

	// 6. Dispatch lifecycle email if applicable. Fire-and-forget goroutine -
	// email failures must not affect the webhook response (RC would retry
	// the whole event and we'd re-apply).
	if applyStatus == revenuecat.ApplyStatusApplied && userIDPtr != nil {
		h.dispatchEventEmail(*userIDPtr, env.Event)
	}
	c.JSON(http.StatusOK, gin.H{"status": applyStatus})
}

// dispatchEventEmail picks the right email kind for the event and sends it
// in a background goroutine. Silent for event types that should not produce
// a user-facing email (regular renewals, product changes, etc.).
func (h *WebhookHandler) dispatchEventEmail(userID string, evt revenuecat.Event) {
	if h.mailer == nil {
		return
	}
	kind, vars := emailForEvent(evt)
	if kind == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.mailer.Send(ctx, userID, kind, vars, "rc:"+evt.ID); err != nil {
			log.Warn(ctx, "RC lifecycle email failed",
				"event_id="+evt.ID, "kind="+string(kind), err.Error())
		}
	}()
}

// emailForEvent maps an RC event to the email kind to send (or "" for
// silent). Variable values are derived from the event payload.
//
// Silent on: regular RENEWAL, PRODUCT_CHANGE, UNCANCELLATION, REFUND. Those
// have no useful user-facing message we'd want to send unsolicited.
func emailForEvent(evt revenuecat.Event) (email.EmailKind, email.Vars) {
	productName := productNameFor(evt.ProductID)
	resubscribeURL := "https://deadreckoner.io/app"
	manageURL := "https://apps.apple.com/account/subscriptions"

	switch evt.Type {
	case revenuecat.EventTypeInitialPurchase:
		if evt.PeriodType == revenuecat.PeriodTypeTrial {
			return email.KindTrialStarted, email.Vars{
				TrialEndsAt: formatMs(evt.ExpirationAtMs),
				ProductName: productName,
			}
		}
		// Non-trial direct subscription: treat like trial conversion - "you
		// are now Pro" copy fits either path.
		return email.KindTrialConverted, email.Vars{
			NextRenewalDate: formatMs(evt.ExpirationAtMs),
			ProductName:     productName,
		}

	case revenuecat.EventTypeRenewal:
		if evt.IsTrialConversion {
			return email.KindTrialConverted, email.Vars{
				NextRenewalDate: formatMs(evt.ExpirationAtMs),
				ProductName:     productName,
			}
		}
		// Silent on regular renewals.
		return "", email.Vars{}

	case revenuecat.EventTypeCancellation:
		return email.KindCancelled, email.Vars{
			AccessUntil:    formatMs(evt.ExpirationAtMs),
			ProductName:    productName,
			ResubscribeURL: resubscribeURL,
		}

	case revenuecat.EventTypeExpiration:
		return email.KindExpired, email.Vars{
			ResubscribeURL: resubscribeURL,
		}

	case revenuecat.EventTypeBillingIssue:
		return email.KindBillingIssue, email.Vars{
			ManageSubscriptionURL: manageURL,
		}
	}
	return "", email.Vars{}
}

// productNameFor maps a store product ID to a human-readable name for use
// in email copy. Adjust when product IDs change in App Store Connect.
func productNameFor(productID string) string {
	switch productID {
	case "dr_pro_monthly":
		return "monthly subscription"
	case "dr_pro_annual":
		return "annual subscription"
	}
	return "subscription"
}

// formatMs renders an ms-since-epoch timestamp as YYYY-MM-DD UTC, suitable
// for the user-facing email body. Empty for zero values so templates skip
// the field gracefully.
func formatMs(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("January 2, 2006")
}

// userExists returns true if a row with the given user_id is present in
// public.users. Used by the webhook handler to gate the FK reference on
// entitlement_events.user_id, since RC can legitimately deliver events for
// IDs that don't correspond to real DR users (test events, deleted users).
func userExists(tx *gorm.DB, userID string) (bool, error) {
	var count int64
	if err := tx.Model(&types.User{}).
		Where("user_id = ?", userID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
