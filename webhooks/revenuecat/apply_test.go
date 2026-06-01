package revenuecat

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	testUserID      = "11111111-1111-1111-1111-111111111111"
	testOrigTxnID   = "1000000099999999"
	testEventID     = "test-event-id"
	testProductID   = "dr_pro_monthly"
	testSubAppleSrc = "apple"
)

func newDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

// entitlementsRow returns the SELECT * FROM entitlements payload used by the
// pre-flight read in Apply. The applier only inspects updated_at for stale
// detection so the other fields can be zero.
func entitlementsRow(updatedAt time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"user_id", "plan", "source", "status",
		"current_period_start", "current_period_end", "trial_ends_at", "cancelled_at",
		"provider_customer_id", "provider_subscription_id", "product_id",
		"ai_generations_used", "ai_generations_period_start",
		"created_at", "updated_at",
	}).AddRow(
		testUserID, "free", "default", nil,
		nil, nil, nil, nil,
		nil, nil, nil,
		0, time.Now(),
		time.Now(), updatedAt,
	)
}

func expectPreflightRead(mock sqlmock.Sqlmock, updatedAt time.Time) {
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementsRow(updatedAt))
}

// --- ResolveUserID -----------------------------------------------------------

func TestResolveUserID_DirectMatch(t *testing.T) {
	got := ResolveUserID(Event{AppUserID: testUserID})
	assert.Equal(t, testUserID, got)
}

func TestResolveUserID_AnonymousFallsBackToAlias(t *testing.T) {
	got := ResolveUserID(Event{
		AppUserID: "$RCAnonymousID:abc",
		Aliases:   []string{testUserID},
	})
	assert.Equal(t, testUserID, got)
}

func TestResolveUserID_NoneValid(t *testing.T) {
	got := ResolveUserID(Event{
		AppUserID: "anonymous",
		Aliases:   []string{"also-not-a-uuid"},
	})
	assert.Empty(t, got)
}

// --- touchesProEntitlement ---------------------------------------------------

func TestTouchesProEntitlement(t *testing.T) {
	tests := []struct {
		name string
		evt  Event
		want bool
	}{
		{"singular pro", Event{EntitlementID: "pro"}, true},
		{"plural contains pro", Event{EntitlementIDs: []string{"pro", "beta"}}, true},
		{"empty (legacy)", Event{}, true},
		{"non-pro only", Event{EntitlementID: "beta"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, touchesProEntitlement(tt.evt))
		})
	}
}

// --- MapStoreToSource --------------------------------------------------------

func TestMapStoreToSource(t *testing.T) {
	cases := map[string]string{
		StoreAppStore:    "apple",
		StoreMacAppStore: "apple",
		StorePlayStore:   "google",
		StoreAmazon:      "google",
		StoreStripe:      "stripe",
		StorePromotional: "comp",
		"BOGUS":          "",
		"":               "",
	}
	for in, want := range cases {
		assert.Equalf(t, want, MapStoreToSource(in), "MapStoreToSource(%q)", in)
	}
}

// --- Apply: per-event-type ---------------------------------------------------

func TestApply_InitialPurchase_Trial(t *testing.T) {
	db, mock := newDB(t)
	staleTime := time.Now().Add(-1 * time.Hour)
	expectPreflightRead(mock, staleTime)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := Event{
		ID:                    testEventID,
		Type:                  EventTypeInitialPurchase,
		AppUserID:             testUserID,
		Store:                 StoreAppStore,
		ProductID:             testProductID,
		PeriodType:            PeriodTypeTrial,
		EventTimestampMs:      time.Now().UnixMilli(),
		PurchasedAtMs:         time.Now().UnixMilli(),
		ExpirationAtMs:        time.Now().Add(7 * 24 * time.Hour).UnixMilli(),
		EntitlementIDs:        []string{"pro"},
		OriginalTransactionID: testOrigTxnID,
	}

	status, err := Apply(db, evt)
	assert.Equal(t, ApplyStatusApplied, status)
	assert.NoError(t, err)
}

func TestApply_Renewal(t *testing.T) {
	db, mock := newDB(t)
	expectPreflightRead(mock, time.Now().Add(-1*time.Hour))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := Event{
		ID:                    testEventID,
		Type:                  EventTypeRenewal,
		AppUserID:             testUserID,
		Store:                 StorePlayStore,
		ProductID:             testProductID,
		PeriodType:            PeriodTypeNormal,
		EventTimestampMs:      time.Now().UnixMilli(),
		EntitlementIDs:        []string{"pro"},
		OriginalTransactionID: testOrigTxnID,
		IsTrialConversion:     true,
	}

	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusApplied, status)
}

func TestApply_Cancellation_KeepsAccessUntilPeriodEnd(t *testing.T) {
	db, mock := newDB(t)
	expectPreflightRead(mock, time.Now().Add(-1*time.Hour))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := Event{
		ID:               testEventID,
		Type:             EventTypeCancellation,
		AppUserID:        testUserID,
		Store:            StoreAppStore,
		EventTimestampMs: time.Now().UnixMilli(),
		EntitlementIDs:   []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusApplied, status)
}

func TestApply_Expiration_DowngradesToFree(t *testing.T) {
	db, mock := newDB(t)
	expectPreflightRead(mock, time.Now().Add(-1*time.Hour))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := Event{
		ID:               testEventID,
		Type:             EventTypeExpiration,
		AppUserID:        testUserID,
		Store:            StoreAppStore,
		EventTimestampMs: time.Now().UnixMilli(),
		EntitlementIDs:   []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusApplied, status)
}

func TestApply_Refund_ImmediateDowngrade(t *testing.T) {
	db, mock := newDB(t)
	expectPreflightRead(mock, time.Now().Add(-1*time.Hour))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := Event{
		ID:               testEventID,
		Type:             EventTypeRefund,
		AppUserID:        testUserID,
		Store:            StoreAppStore,
		EventTimestampMs: time.Now().UnixMilli(),
		EntitlementIDs:   []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusApplied, status)
}

func TestApply_BillingIssue_StaysPaid_Grace(t *testing.T) {
	db, mock := newDB(t)
	expectPreflightRead(mock, time.Now().Add(-1*time.Hour))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "entitlements" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	evt := Event{
		ID:               testEventID,
		Type:             EventTypeBillingIssue,
		AppUserID:        testUserID,
		Store:            StoreAppStore,
		EventTimestampMs: time.Now().UnixMilli(),
		EntitlementIDs:   []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusApplied, status)
}

func TestApply_StaleEvent_Skipped(t *testing.T) {
	db, mock := newDB(t)
	// updated_at is in the future relative to the event.
	expectPreflightRead(mock, time.Now().Add(1*time.Hour))

	evt := Event{
		ID:               testEventID,
		Type:             EventTypeExpiration,
		AppUserID:        testUserID,
		Store:            StoreAppStore,
		EventTimestampMs: time.Now().UnixMilli(), // older than the row
		EntitlementIDs:   []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusNoopStale, status)
}

func TestApply_UnmappedUser(t *testing.T) {
	db, _ := newDB(t)
	evt := Event{
		ID:             testEventID,
		Type:           EventTypeInitialPurchase,
		AppUserID:      "$RCAnonymousID:abc",
		Store:          StoreAppStore,
		EntitlementIDs: []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusNoopUnmapped, status)
}

func TestApply_UnknownEventType_Noop(t *testing.T) {
	db, mock := newDB(t)
	expectPreflightRead(mock, time.Now().Add(-1*time.Hour))

	evt := Event{
		ID:               testEventID,
		Type:             "SOME_BRAND_NEW_TYPE",
		AppUserID:        testUserID,
		Store:            StoreAppStore,
		EventTimestampMs: time.Now().UnixMilli(),
		EntitlementIDs:   []string{"pro"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusNoopUnknown, status)
}

func TestApply_NonProEntitlement_Noop(t *testing.T) {
	db, _ := newDB(t)

	evt := Event{
		ID:             testEventID,
		Type:           EventTypeInitialPurchase,
		AppUserID:      testUserID,
		Store:          StoreAppStore,
		EntitlementIDs: []string{"beta_program"},
	}
	status, _ := Apply(db, evt)
	assert.Equal(t, ApplyStatusNoopUnknown, status)
}

func TestApply_UnknownStore_Error(t *testing.T) {
	db, _ := newDB(t)

	evt := Event{
		ID:             testEventID,
		Type:           EventTypeInitialPurchase,
		AppUserID:      testUserID,
		Store:          "MYSTERY_STORE",
		EntitlementIDs: []string{"pro"},
	}
	status, applyErr := Apply(db, evt)
	assert.Equal(t, ApplyStatusNoopUnknown, status)
	assert.Error(t, applyErr, "unknown store should bubble an error for logging")
}
