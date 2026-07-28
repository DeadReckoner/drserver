package email

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// tleSqlmockDB returns a *gorm.DB backed by go-sqlmock using regex query
// matching. Inlined here (instead of using internal/testutil) because testutil
// transitively imports this package via api/handlers - importing it from an
// in-package test would create an import cycle.
func tleSqlmockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err, "sqlmock.New")
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = sqlDB.Close()
	})
	return gormDB, mock
}

const tleUserID = "aaaaaaaa-1111-2222-3333-444444444444"

// tleHasPrefix is a sqlmock argument matcher for string args asserted by
// prefix (e.g. error_text values like "render: ..." whose tail varies).
type tleHasPrefix string

func (p tleHasPrefix) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, string(p))
}

// tleExpectUserLookup stubs the Send user lookup with one row.
func tleExpectUserLookup(mock sqlmock.Sqlmock, firstName, email string) {
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(tleUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "first_name", "last_name", "email", "phone_number",
			"created_at", "profile_image_url", "timezone", "onboarding_completed",
		}).AddRow(tleUserID, firstName, "", email, "", nil, "", "UTC", true))
}

// tleExpectDedupeCount stubs the EmailAlreadySent count query.
func tleExpectDedupeCount(mock sqlmock.Sqlmock, userID, kind, dedupeKey string, count int) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "email_log"`).
		WithArgs(userID, kind, "sent", dedupeKey).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

// tleExpectEmailLogInsert stubs the transactional email_log INSERT. GORM's
// column order follows the EmailLog struct: id, user_id, kind, to_email,
// status, provider_message_id, error_text, dedupe_key, created_at.
func tleExpectEmailLogInsert(mock sqlmock.Sqlmock, args ...driver.Value) {
	mock.ExpectBegin()
	e := mock.ExpectExec(`INSERT INTO "email_log"`)
	if len(args) > 0 {
		e.WithArgs(args...)
	}
	e.WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

// tleDispatcher wires a Dispatcher whose Resend leg points at an httptest
// server (counting calls) and whose DB is sqlmock-backed.
func tleDispatcher(t *testing.T, handler http.HandlerFunc) (*Dispatcher, sqlmock.Sqlmock, *int32) {
	t.Helper()
	db, mock := tleSqlmockDB(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Dispatcher{DB: db, Client: newClient(srv.URL, "test-key")}, mock, &calls
}

// tleOKHandler is a Resend success response with a fixed message id.
func tleOKHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"id":"prov-123"}`))
}

func TestDispatcherSend_HappyPath_SendsAndLogsSent(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	// dedupeKey empty -> no count query.
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "welcome", "ada@x.com",
		"sent", "prov-123", "", "", sqlmock.AnyArg())

	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))
}

func TestDispatcherSend_UserNotFound_NoLogNoHTTP(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(tleUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"})) // zero rows -> ErrRecordNotFound

	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestDispatcherSend_EmptyEmail_RecordsSkip(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectUserLookup(mock, "Ada", "") // user exists but has no address
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "welcome", "",
		"skipped", "", "no email on user record", "", sqlmock.AnyArg())

	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no email address")
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestDispatcherSend_DedupeHit_SkipsWithoutSending(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	tleExpectDedupeCount(mock, tleUserID, "welcome", "dk-1", 1)
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "welcome", "ada@x.com",
		"skipped", "", "already sent with same dedupe_key", "dk-1", sqlmock.AnyArg())

	// Duplicate send is a success from the caller's perspective, not an error.
	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "dk-1")
	require.NoError(t, err)
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestDispatcherSend_DedupeCheckDBError_StillSends(t *testing.T) {
	// The "rather duplicate than drop" contract: a failing dedupe query must
	// not block a transactional email.
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	mock.ExpectQuery(`SELECT count\(\*\) FROM "email_log"`).
		WithArgs(tleUserID, "welcome", "sent", "dk-1").
		WillReturnError(errors.New("db down"))
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "welcome", "ada@x.com",
		"sent", "prov-123", "", "dk-1", sqlmock.AnyArg())

	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "dk-1")
	require.NoError(t, err)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))
}

func TestDispatcherSend_RenderError_RecordsError(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "nope", "ada@x.com",
		"error", "", tleHasPrefix("render:"), "", sqlmock.AnyArg())

	err := d.Send(context.Background(), tleUserID, EmailKind("nope"), Vars{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render")
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestDispatcherSend_ProviderPermanentFailure_LogsError(t *testing.T) {
	d, mock, calls := tleDispatcher(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad email","name":"validation_error"}`))
	})

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "welcome", "ada@x.com",
		"error", "", tleHasPrefix("resend: 400"), "", sqlmock.AnyArg())

	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation_error")
	assert.EqualValues(t, 1, atomic.LoadInt32(calls), "400 is permanent - no retries")
}

func TestDispatcherSend_VarsDefaultFromUserRecord(t *testing.T) {
	var gotBody string
	d, mock, _ := tleDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		tleOKHandler(w, r)
	})

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	tleExpectEmailLogInsert(mock)

	// Empty Vars: recipient (and FirstName) must be filled from the user row.
	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "")
	require.NoError(t, err)
	assert.Contains(t, gotBody, `"to":["ada@x.com"]`)
}

func TestDispatcherSend_LogInsertFailure_DoesNotMaskSuccess(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectUserLookup(mock, "Ada", "ada@x.com")
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "email_log"`).WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	// The email went out; a failed audit write is log-only, never an error.
	err := d.Send(context.Background(), tleUserID, KindWelcome, Vars{}, "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))
}

func TestDispatcherSendToAddress_EmptyRecipient_FailsFast(t *testing.T) {
	d, _, calls := tleDispatcher(t, tleOKHandler) // no SQL expectations registered

	err := d.SendToAddress(context.Background(), "", tleUserID, KindReportReceived, Vars{}, "dk")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recipient address required")
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestDispatcherSendToAddress_HappyPath_AttributesUser(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	// No users lookup at all - recipient is explicit.
	tleExpectDedupeCount(mock, tleUserID, "report_received", "dk-2", 0)
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "report_received", "support@deadreckoner.io",
		"sent", "prov-123", "", "dk-2", sqlmock.AnyArg())

	err := d.SendToAddress(context.Background(), "support@deadreckoner.io", tleUserID,
		KindReportReceived, Vars{ReportID: "r1"}, "dk-2")
	require.NoError(t, err)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))
}

func TestDispatcherSendToAddress_NoAttributedUser_SkipsDedupeAndSends(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	// dedupeKey set but attributedUserID empty -> dedupe can't key on a user,
	// so no count query; the send still goes out.
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), "", "report_received", "support@deadreckoner.io",
		"sent", "prov-123", "", "dk-3", sqlmock.AnyArg())

	err := d.SendToAddress(context.Background(), "support@deadreckoner.io", "",
		KindReportReceived, Vars{}, "dk-3")
	require.NoError(t, err)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))
}

func TestDispatcherSendToAddress_DedupeHit_SkipsWithoutSending(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectDedupeCount(mock, tleUserID, "report_received", "dk-4", 1)
	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "report_received", "support@deadreckoner.io",
		"skipped", "", "already sent with same dedupe_key", "dk-4", sqlmock.AnyArg())

	err := d.SendToAddress(context.Background(), "support@deadreckoner.io", tleUserID,
		KindReportReceived, Vars{}, "dk-4")
	require.NoError(t, err)
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestDispatcherSendToAddress_RenderError_RecordsError(t *testing.T) {
	d, mock, calls := tleDispatcher(t, tleOKHandler)

	tleExpectEmailLogInsert(mock,
		sqlmock.AnyArg(), tleUserID, "nope", "support@deadreckoner.io",
		"error", "", tleHasPrefix("render:"), "", sqlmock.AnyArg())

	err := d.SendToAddress(context.Background(), "support@deadreckoner.io", tleUserID,
		EmailKind("nope"), Vars{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render")
	assert.EqualValues(t, 0, atomic.LoadInt32(calls))
}

func TestNewDispatcher_WiresDBAndProductionClient(t *testing.T) {
	db, _ := tleSqlmockDB(t)
	d := NewDispatcher(db)

	require.NotNil(t, d)
	assert.Same(t, db, d.DB, "dispatcher must use the passed *gorm.DB")
	require.NotNil(t, d.Client)
	// Structural, env-independent assertions only: the client is production
	// shaped regardless of RESEND_API_KEY / FROM_EMAIL being set.
	assert.Equal(t, resendEndpoint, d.Client.Endpoint)
	assert.Equal(t, defaultMaxAttempts, d.Client.MaxAttempts)
	assert.Equal(t, defaultInitialBackoff, d.Client.InitialBackoff)
	assert.Equal(t, defaultMaxBackoff, d.Client.MaxBackoff)
	assert.NotEmpty(t, d.Client.From)
	assert.NotNil(t, d.Client.HTTPClient)
	assert.NotNil(t, d.Client.Sleep)
}
