package database

// Tests for ai_acknowledgment.go. The contract that matters: acceptance is
// per-version (an old acceptance must not satisfy a bumped version), and
// RecordAIAck is idempotent — a repeat acceptance returns the ORIGINAL stored
// row rather than a zero-value struct from the DO NOTHING insert.

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dbnotifAIAckRow(version int, acceptedAt time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"user_id", "version", "app_version", "accepted_at"}).
		AddRow(testUserID, version, "1.2.3", acceptedAt)
}

// =============================================================================
// GetLatestAIAck
// =============================================================================

func TestGetLatestAIAck_NeverAccepted_NilNil(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments" WHERE user_id = \$1 ORDER BY version DESC`).
		WithArgs(testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))

	ack, err := GetLatestAIAck(testUserID)
	require.NoError(t, err, "not-found is not an error for this API")
	assert.Nil(t, ack)
}

func TestGetLatestAIAck_Found_ReturnsNewestVersion(t *testing.T) {
	accepted := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments" WHERE user_id = \$1 ORDER BY version DESC`).
		WithArgs(testUserID, 1).
		WillReturnRows(dbnotifAIAckRow(2, accepted))

	ack, err := GetLatestAIAck(testUserID)
	require.NoError(t, err)
	require.NotNil(t, ack)
	assert.Equal(t, 2, ack.Version)
	assert.Equal(t, accepted, ack.AcceptedAt)
}

func TestGetLatestAIAck_DBError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments"`).
		WillReturnError(errors.New("select failed"))

	ack, err := GetLatestAIAck(testUserID)
	assert.ErrorContains(t, err, "select failed")
	assert.Nil(t, ack)
}

// =============================================================================
// HasAcceptedAIAck
// =============================================================================

func TestHasAcceptedAIAck_PinsExactVersion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		want  bool
	}{
		{"accepted", 1, true},
		{"not accepted", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := withMockDB(t)
			// The version arg must be pinned: accepting v1 must not satisfy
			// a v2 check (version bumps require re-acceptance).
			mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
				WithArgs(testUserID, 2).
				WillReturnRows(dbnotifCountRow(tc.count))

			ok, err := HasAcceptedAIAck(testUserID, 2)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ok)
		})
	}
}

func TestHasAcceptedAIAck_CountError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_acknowledgments"`).
		WillReturnError(errors.New("count failed"))

	ok, err := HasAcceptedAIAck(testUserID, 1)
	assert.ErrorContains(t, err, "count failed")
	assert.False(t, ok)
}

// =============================================================================
// RecordAIAck
// =============================================================================

func TestRecordAIAck_RepeatAcceptance_ReturnsOriginalRow(t *testing.T) {
	originalAcceptedAt := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "ai_acknowledgments" .* ON CONFLICT \("user_id","version"\) DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 0)) // conflict: no row inserted
	mock.ExpectCommit()
	// Re-read returns the canonical stored row with the ORIGINAL accepted_at,
	// not the zero value the DoNothing insert would have left behind.
	mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments" WHERE user_id = \$1 AND version = \$2`).
		WithArgs(testUserID, 1, 1).
		WillReturnRows(dbnotifAIAckRow(1, originalAcceptedAt))

	ack, err := RecordAIAck(testUserID, 1, "1.2.3")
	require.NoError(t, err)
	assert.Equal(t, originalAcceptedAt, ack.AcceptedAt)
	assert.Equal(t, 1, ack.Version)
}

func TestRecordAIAck_InsertError_ZeroStruct(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "ai_acknowledgments"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	ack, err := RecordAIAck(testUserID, 1, "1.2.3")
	assert.ErrorContains(t, err, "insert failed")
	assert.Zero(t, ack)
}

func TestRecordAIAck_ReReadError_ZeroStruct(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "ai_acknowledgments"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT \* FROM "ai_acknowledgments"`).
		WillReturnError(errors.New("read-back failed"))

	ack, err := RecordAIAck(testUserID, 1, "1.2.3")
	assert.ErrorContains(t, err, "read-back failed")
	assert.Zero(t, ack)
}
