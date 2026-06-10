package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dr/server/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newRCDB returns a sqlmock-backed gorm.DB + the mock. Inlined here (not
// reusing internal/testutil) to avoid an import cycle.
func newRCDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = sqlDB.Close()
	})
	return gormDB, mock
}

// rcRouter spins a one-route gin engine with the webhook handler attached.
func rcRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// nil mailer skips email dispatch - acceptable for these tests since
	// we're verifying the persist/HTTP path, not the email side. Step 6.5's
	// own tests exercise the mailer in isolation.
	h := NewWebhookHandler(db, nil)
	r.POST("/webhooks/revenuecat", h.RevenueCatWebhook)
	return r
}

func postRC(t *testing.T, router *gin.Engine, authHeader string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/revenuecat", bytes.NewReader(buf))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRCWebhook_MissingSecretReturns503(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "")
	db, _ := newRCDB(t)
	w := postRC(t, rcRouter(db), "anything", map[string]any{"event": map[string]any{"id": "x"}})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRCWebhook_BadAuthReturns401(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "real-secret")
	db, _ := newRCDB(t)
	w := postRC(t, rcRouter(db), "wrong-secret", map[string]any{"event": map[string]any{"id": "x"}})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRCWebhook_DuplicateEventReturns200(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "real-secret")
	db, mock := newRCDB(t)

	// Idempotency lookup: count > 0 means duplicate, short-circuit.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "entitlement_events" WHERE event_id = \$1`).
		WithArgs("dup-evt").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	body := map[string]any{
		"event": map[string]any{
			"id":                 "dup-evt",
			"type":               "INITIAL_PURCHASE",
			"app_user_id":        "11111111-1111-1111-1111-111111111111",
			"event_timestamp_ms": time.Now().UnixMilli(),
		},
	}
	w := postRC(t, rcRouter(db), "real-secret", body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "duplicate", resp["status"])
}

func TestRCWebhook_HappyPath_InitialPurchase(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "real-secret")
	db, mock := newRCDB(t)

	const userID = "11111111-1111-1111-1111-111111111111"
	const eventID = "evt-happy-1"

	// 1. Idempotency: not seen yet.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "entitlement_events" WHERE event_id = \$1`).
		WithArgs(eventID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// 2. Transaction begins.
	mock.ExpectBegin()

	// 3. Apply preflight SELECT.
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "plan", "source", "status",
			"current_period_start", "current_period_end", "trial_ends_at", "cancelled_at",
			"provider_customer_id", "provider_subscription_id", "product_id",
			"ai_generations_used", "ai_generations_period_start",
			"created_at", "updated_at",
		}).AddRow(
			userID, "free", "default", nil, nil, nil, nil, nil, nil, nil, nil, 0, time.Now(),
			time.Now(), time.Now().Add(-1*time.Hour),
		))

	// 4. UPDATE entitlements.
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 5. userExists check before FK-linking the audit row.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "users" WHERE user_id = \$1`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// 6. INSERT into entitlement_events.
	mock.ExpectExec(`INSERT INTO "entitlement_events"`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 7. Commit.
	mock.ExpectCommit()

	body := map[string]any{
		"event": map[string]any{
			"id":                 eventID,
			"type":               "INITIAL_PURCHASE",
			"app_user_id":        userID,
			"store":              "APP_STORE",
			"product_id":         "dr_pro_monthly",
			"period_type":        "TRIAL",
			"event_timestamp_ms": time.Now().UnixMilli(),
			"purchased_at_ms":    time.Now().UnixMilli(),
			"expiration_at_ms":   time.Now().Add(7 * 24 * time.Hour).UnixMilli(),
			"entitlement_ids":    []string{"pro"},
		},
	}
	w := postRC(t, rcRouter(db), "real-secret", body)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	// Suppress unused-var warning for the database import.
	_ = database.EntitlementEventExists
}

// Regression test for the FK-violation that took down the first prod sandbox
// test event: RC's "Send Test Event" button posts a canned event whose
// app_user_id is a well-formed UUID that does NOT exist in our users table.
// The handler must still 200 (event recorded for audit) and leave user_id
// NULL in the audit row to avoid the FK constraint.
func TestRCWebhook_UnmappedButValidUUID_RecordsWithNullUser(t *testing.T) {
	t.Setenv("REVENUECAT_WEBHOOK_SECRET", "real-secret")
	db, mock := newRCDB(t)

	const phantomUUID = "bb3a1105-6e89-4669-9e72-fb73f99c5a2d"
	const eventID = "evt-phantom-1"

	// 1. Idempotency: not seen.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "entitlement_events" WHERE event_id = \$1`).
		WithArgs(eventID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// 2. Tx begins.
	mock.ExpectBegin()

	// 3. Apply's preflight read - no entitlement row for the phantom user,
	// so Apply will short-circuit with ApplyStatusNoopUnmapped and NOT
	// emit the UPDATE.
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(phantomUUID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	// 4. userExists check returns 0 - phantom user doesn't exist in
	// public.users either, so we record the audit row with user_id NULL.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "users" WHERE user_id = \$1`).
		WithArgs(phantomUUID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// 5. INSERT proceeds with user_id NULL (no FK violation).
	mock.ExpectExec(`INSERT INTO "entitlement_events"`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 6. Commit.
	mock.ExpectCommit()

	body := map[string]any{
		"event": map[string]any{
			"id":                 eventID,
			"type":               "TEST",
			"app_user_id":        phantomUUID,
			"store":              "APP_STORE",
			"event_timestamp_ms": time.Now().UnixMilli(),
		},
	}
	w := postRC(t, rcRouter(db), "real-secret", body)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "noop_unmapped", resp["status"])
}
