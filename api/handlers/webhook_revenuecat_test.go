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
	h := NewWebhookHandler(db)
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

	// 5. INSERT into entitlement_events.
	mock.ExpectExec(`INSERT INTO "entitlement_events"`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 6. Commit.
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
