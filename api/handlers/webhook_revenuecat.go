package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"dr/server/webhooks/revenuecat"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// WebhookHandler bundles all external-webhook endpoints. Today only RC; room
// to grow (Stripe, Apple ASSN direct, etc.) without expanding handler count.
type WebhookHandler struct {
	db *gorm.DB
}

func NewWebhookHandler(db *gorm.DB) *WebhookHandler {
	return &WebhookHandler{db: db}
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
		log.Error(ctx, "RC webhook: REVENUECAT_WEBHOOK_SECRET not set", err)
		c.JSON(http.StatusServiceUnavailable,
			gin.H{"error": "webhook secret not configured"})
		return
	case errors.Is(err, revenuecat.ErrAuthMismatch):
		log.Warn(ctx, "RC webhook: auth mismatch")
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
		log.Error(ctx, "RC webhook: idempotency lookup", err)
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

		if uid := revenuecat.ResolveUserID(env.Event); uid != "" {
			userIDPtr = &uid
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
		log.Error(ctx, "RC webhook: tx", txErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist"})
		return
	}

	log.Info(ctx, "RC webhook: handled",
		"event_id="+env.Event.ID, "type="+env.Event.Type, "apply="+applyStatus)
	c.JSON(http.StatusOK, gin.H{"status": applyStatus})
}
