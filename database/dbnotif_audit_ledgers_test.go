package database

// Tests for the thin append-only ledgers: email_log.go, entitlement_events.go,
// and reports.go. These are simple, but each carries a contract worth pinning:
// the nil-tx fallback to the package global, dedupe predicates, the ReceivedAt
// backfill, and raw duplicate-key error passthrough.

import (
	"context"
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// InsertEmailLog
// =============================================================================

func TestInsertEmailLog_NilTx_UsesGlobalDB(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "email_log"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := InsertEmailLog(nil, &types.EmailLog{
		ID:      dbnotifRowID,
		UserID:  testUserID,
		Kind:    "welcome",
		ToEmail: "user@example.com",
		Status:  "sent",
	})
	require.NoError(t, err)
}

func TestInsertEmailLog_ExplicitTx_GlobalDBUntouched(t *testing.T) {
	// The global mock gets ZERO expectations; if InsertEmailLog wrongly fell
	// through to the package global, the cleanup expectation check (and the
	// unexpected-call error) would fail the test.
	withMockDB(t)

	txDB, txMock := newSqlmockDB(t)
	txMock.ExpectBegin()
	txMock.ExpectExec(`INSERT INTO "email_log"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	txMock.ExpectCommit()

	err := InsertEmailLog(txDB, &types.EmailLog{
		ID:      dbnotifRowID,
		UserID:  testUserID,
		Kind:    "digest",
		ToEmail: "user@example.com",
		Status:  "sent",
	})
	require.NoError(t, err)
}

func TestInsertEmailLog_CreateError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "email_log"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := InsertEmailLog(nil, &types.EmailLog{ID: dbnotifRowID, UserID: testUserID, Kind: "welcome", ToEmail: "u@e.co", Status: "sent"})
	assert.ErrorContains(t, err, "insert failed")
}

// =============================================================================
// EmailAlreadySent
// =============================================================================

func TestEmailAlreadySent_MissingUserOrKind_ErrorsWithoutSQL(t *testing.T) {
	withMockDB(t) // zero expectations: validation must short-circuit

	for _, tc := range []struct {
		name         string
		userID, kind string
	}{
		{"empty user", "", "welcome"},
		{"empty kind", testUserID, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent, err := EmailAlreadySent(nil, tc.userID, tc.kind, "")
			assert.Error(t, err)
			assert.False(t, sent)
		})
	}
}

func TestEmailAlreadySent_EmptyDedupeKey_OmitsPredicate(t *testing.T) {
	mock := withMockDB(t)
	// "any send of this kind": no dedupe_key clause between status and count.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "email_log" WHERE user_id = \$1 AND kind = \$2 AND status = \$3$`).
		WithArgs(testUserID, "welcome", "sent").
		WillReturnRows(dbnotifCountRow(1))

	sent, err := EmailAlreadySent(nil, testUserID, "welcome", "")
	require.NoError(t, err)
	assert.True(t, sent)
}

func TestEmailAlreadySent_WithDedupeKey_AddsPredicate(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "email_log" WHERE \(user_id = \$1 AND kind = \$2 AND status = \$3\) AND dedupe_key = \$4`).
		WithArgs(testUserID, "digest", "sent", "2026-07-26").
		WillReturnRows(dbnotifCountRow(0))

	sent, err := EmailAlreadySent(nil, testUserID, "digest", "2026-07-26")
	require.NoError(t, err)
	assert.False(t, sent)
}

func TestEmailAlreadySent_QueryError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "email_log"`).
		WillReturnError(errors.New("count failed"))

	sent, err := EmailAlreadySent(nil, testUserID, "welcome", "")
	assert.ErrorContains(t, err, "count failed")
	assert.False(t, sent)
}

// =============================================================================
// InsertEntitlementEvent
// =============================================================================

func TestInsertEntitlementEvent_ZeroReceivedAt_BackfilledToNow(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// Arg order follows struct declaration: event_id, user_id, provider,
	// store, event_type, product_id, event_timestamp_ms, received_at,
	// payload, apply_status, apply_error. received_at must be a real
	// timestamp, not the zero value.
	mock.ExpectExec(`INSERT INTO "entitlement_events"`).
		WithArgs("evt-1", nil, "revenuecat", "app_store", "INITIAL_PURCHASE", "pro_monthly",
			int64(1750000000000), dbnotifNonZeroTime{}, dbnotifJSONArg(`{"raw":true}`), "applied", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := &types.EntitlementEvent{
		EventID:          "evt-1",
		Provider:         "revenuecat",
		Store:            "app_store",
		EventType:        "INITIAL_PURCHASE",
		ProductID:        "pro_monthly",
		EventTimestampMs: 1750000000000,
		Payload:          types.JSONRaw(`{"raw":true}`),
		ApplyStatus:      "applied",
	}
	require.NoError(t, InsertEntitlementEvent(nil, evt))
	assert.False(t, evt.ReceivedAt.IsZero(), "backfill must also be visible on the struct")
	assert.Equal(t, time.UTC, evt.ReceivedAt.Location())
}

func TestInsertEntitlementEvent_NonZeroReceivedAt_Preserved(t *testing.T) {
	received := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "entitlement_events"`).
		WithArgs("evt-2", nil, "revenuecat", "", "RENEWAL", "",
			int64(1), received, dbnotifJSONArg(`{}`), "applied", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := &types.EntitlementEvent{
		EventID:          "evt-2",
		Provider:         "revenuecat",
		EventType:        "RENEWAL",
		EventTimestampMs: 1,
		ReceivedAt:       received,
		Payload:          types.JSONRaw(`{}`),
		ApplyStatus:      "applied",
	}
	require.NoError(t, InsertEntitlementEvent(nil, evt))
	assert.Equal(t, received, evt.ReceivedAt)
}

func TestInsertEntitlementEvent_DuplicateKeyError_PassedThroughRaw(t *testing.T) {
	dupErr := errors.New(`ERROR: duplicate key value violates unique constraint "entitlement_events_pkey" (SQLSTATE 23505)`)
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "entitlement_events"`).WillReturnError(dupErr)
	mock.ExpectRollback()

	err := InsertEntitlementEvent(nil, &types.EntitlementEvent{
		EventID:          "evt-dup",
		Provider:         "revenuecat",
		EventType:        "RENEWAL",
		EventTimestampMs: 1,
		Payload:          types.JSONRaw(`{}`),
		ApplyStatus:      "applied",
	})
	// The raw driver error must survive so the caller's
	// IsEntitlementEventDuplicate string-matching still works.
	require.Error(t, err)
	assert.ErrorContains(t, err, "23505")
}

// =============================================================================
// EntitlementEventExists
// =============================================================================

func TestEntitlementEventExists_EmptyEventID_ErrorsWithoutSQL(t *testing.T) {
	withMockDB(t) // zero expectations

	exists, err := EntitlementEventExists(nil, "")
	assert.Error(t, err)
	assert.False(t, exists)
}

func TestEntitlementEventExists_TrueAndFalse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		want  bool
	}{
		{"exists", 1, true},
		{"absent", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := withMockDB(t)
			mock.ExpectQuery(`SELECT count\(\*\) FROM "entitlement_events" WHERE event_id = \$1`).
				WithArgs("evt-9").
				WillReturnRows(dbnotifCountRow(tc.count))

			exists, err := EntitlementEventExists(nil, "evt-9")
			require.NoError(t, err)
			assert.Equal(t, tc.want, exists)
		})
	}
}

func TestEntitlementEventExists_CountError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "entitlement_events"`).
		WillReturnError(errors.New("count failed"))

	exists, err := EntitlementEventExists(nil, "evt-9")
	assert.ErrorContains(t, err, "count failed")
	assert.False(t, exists)
}

// =============================================================================
// Reports
// =============================================================================

func TestCreateReport_HappyPath(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "reports"`).
		WillReturnRows(sqlmock.NewRows([]string{"report_id"}).AddRow(dbnotifRowID))
	mock.ExpectCommit()

	report := &types.Report{
		ReporterID: testUserID,
		Type:       types.ReportTypeUser,
		Reason:     "harassment",
	}
	require.NoError(t, CreateReport(context.Background(), report))
	assert.Equal(t, dbnotifRowID, report.ReportID, "generated id should be scanned back")
}

func TestCreateReport_InsertError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "reports"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := CreateReport(context.Background(), &types.Report{ReporterID: testUserID, Type: types.ReportTypeAI, Reason: "harmful"})
	assert.ErrorContains(t, err, "insert failed")
}

func TestCountReportsByReporterSince(t *testing.T) {
	since := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "reports" WHERE reporter_id = \$1 AND created_at >= \$2`).
		WithArgs(testUserID, since).
		WillReturnRows(dbnotifCountRow(4))

	n, err := CountReportsByReporterSince(testUserID, since)
	require.NoError(t, err)
	assert.Equal(t, int64(4), n)
}

func TestCountReportsByReporterSince_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "reports"`).
		WillReturnError(errors.New("count failed"))

	n, err := CountReportsByReporterSince(testUserID, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC))
	assert.ErrorContains(t, err, "count failed")
	assert.Zero(t, n)
}
