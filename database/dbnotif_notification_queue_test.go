package database

// Tests for pending_notifications.go and digest_items.go — the deferral and
// digest queues behind notification Dispatch. Contracts pinned here:
//   - nil Data normalizes to jsonb '{}' (not 'null') on both queues
//   - the pending upsert bumps attempts and refreshes scheduled_for but NEVER
//     reassigns title/body (trigger spam can't rewrite a queued payload)
//   - the digest upsert is DO NOTHING (no assignment clause at all)
// The gormExpr helper is covered transitively by the attempts-increment SQL
// assertion.

import (
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// UpsertPendingNotification
// =============================================================================

func TestUpsertPendingNotification_NilDataNormalizedAndAttemptsBumped(t *testing.T) {
	scheduledFor := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)

	mock := withMockDB(t)
	mock.ExpectBegin()
	// The DO UPDATE SET list is pinned end-to-end: attempts increments via a
	// literal SQL expression, scheduled_for/updated_at refresh, and title/body
	// are absent (the original captured payload must survive trigger spam).
	mock.ExpectQuery(`INSERT INTO "pending_notifications" .* ON CONFLICT \("user_id","notification_type","dedup_key","channel"\) DO UPDATE SET "attempts"=pending_notifications\.attempts \+ 1,"scheduled_for"=\$\d+,"updated_at"=\$\d+ RETURNING`).
		WithArgs(
			// Insert args in struct order (id/created_at/updated_at omitted —
			// db-side defaults fetched via RETURNING).
			testUserID, "missed_progress", "dk-1", "push", "f_and_a",
			nil, nil, "Title", "Body",
			dbnotifJSONArg(`{}`), // nil Data must serialize as '{}', not 'null'
			scheduledFor, 0,
			// DO UPDATE assignment args.
			scheduledFor, dbnotifNonZeroTime{},
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(dbnotifRowID))
	mock.ExpectCommit()

	err := UpsertPendingNotification(types.PendingNotification{
		UserID:           testUserID,
		NotificationType: "missed_progress",
		DedupKey:         "dk-1",
		Channel:          "push",
		Category:         "f_and_a",
		Title:            "Title",
		Body:             "Body",
		Data:             nil, // exercises the normalization branch
		ScheduledFor:     scheduledFor,
	})
	require.NoError(t, err)
}

func TestUpsertPendingNotification_CreateError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "pending_notifications"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := UpsertPendingNotification(types.PendingNotification{
		UserID:           testUserID,
		NotificationType: "missed_progress",
		DedupKey:         "dk-1",
		Channel:          "push",
		ScheduledFor:     time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC),
	})
	assert.ErrorContains(t, err, "insert failed")
}

// =============================================================================
// ListDuePendingNotifications
// =============================================================================

func TestListDuePendingNotifications_DueWindowAndAttemptsCap(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "pending_notifications" WHERE scheduled_for <= \$1 AND attempts < \$2 ORDER BY scheduled_for ASC`).
		WithArgs(dbnotifNonZeroTime{}, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "notification_type", "dedup_key", "channel"}).
			AddRow("p-1", testUserID, "missed_progress", "dk-1", "push").
			AddRow("p-2", testUserID, "streak_at_risk", "dk-2", "push"))

	out, err := ListDuePendingNotifications(100)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "p-1", out[0].ID, "oldest-first ordering preserved")
}

func TestListDuePendingNotifications_NoRows_EmptySlice(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "pending_notifications"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	out, err := ListDuePendingNotifications(100)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestListDuePendingNotifications_QueryError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "pending_notifications"`).
		WillReturnError(errors.New("select failed"))

	_, err := ListDuePendingNotifications(100)
	assert.ErrorContains(t, err, "select failed")
}

// =============================================================================
// DeletePendingNotification / CleanupExhaustedPendingNotifications
// =============================================================================

func TestDeletePendingNotification_ByID(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "pending_notifications" WHERE id = \$1`).
		WithArgs("p-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeletePendingNotification("p-1"))
}

func TestDeletePendingNotification_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "pending_notifications"`).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	assert.ErrorContains(t, DeletePendingNotification("p-1"), "delete failed")
}

func TestCleanupExhaustedPendingNotifications_ReturnsRowsAffected(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "pending_notifications" WHERE attempts >= \$1`).
		WithArgs(100).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	n, err := CleanupExhaustedPendingNotifications(100)
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)
}

func TestCleanupExhaustedPendingNotifications_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "pending_notifications"`).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	n, err := CleanupExhaustedPendingNotifications(100)
	assert.ErrorContains(t, err, "delete failed")
	assert.Zero(t, n)
}

// =============================================================================
// UpsertDigestItem
// =============================================================================

func TestUpsertDigestItem_NilDataNormalized_ConflictDoesNothing(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// DO NOTHING — the emitted SQL must carry no assignment clause: the item
	// content is captured-at-trigger and must not be overwritten.
	mock.ExpectQuery(`INSERT INTO "digest_items" .* ON CONFLICT \("user_id","notification_type","dedup_key"\) DO NOTHING RETURNING`).
		WithArgs(testUserID, "timeline_updated", "dk-9", "Title", "Body", dbnotifJSONArg(`{}`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(dbnotifRowID))
	mock.ExpectCommit()

	err := UpsertDigestItem(types.DigestItem{
		UserID:           testUserID,
		NotificationType: "timeline_updated",
		DedupKey:         "dk-9",
		Title:            "Title",
		Body:             "Body",
		Data:             nil, // exercises the '{}' normalization branch
	})
	require.NoError(t, err)
}

func TestUpsertDigestItem_CreateError_Propagates(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "digest_items"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := UpsertDigestItem(types.DigestItem{UserID: testUserID, NotificationType: "timeline_updated", DedupKey: "dk-9"})
	assert.ErrorContains(t, err, "insert failed")
}

// =============================================================================
// ListDigestItemsForUser / DeleteDigestItemsForUser / CleanupStaleDigestItems
// =============================================================================

func TestListDigestItemsForUser_UserScopedOldestFirst(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "digest_items" WHERE user_id = \$1 ORDER BY created_at ASC`).
		WithArgs(testUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "notification_type"}).
			AddRow("d-1", testUserID, "timeline_updated").
			AddRow("d-2", testUserID, "milestone_completed"))

	out, err := ListDigestItemsForUser(testUserID)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "d-1", out[0].ID)
}

func TestListDigestItemsForUser_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "digest_items"`).
		WillReturnError(errors.New("select failed"))

	_, err := ListDigestItemsForUser(testUserID)
	assert.ErrorContains(t, err, "select failed")
}

func TestDeleteDigestItemsForUser_ReturnsRowsAffected(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "digest_items" WHERE user_id = \$1`).
		WithArgs(testUserID).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	n, err := DeleteDigestItemsForUser(testUserID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

func TestDeleteDigestItemsForUser_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "digest_items"`).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	n, err := DeleteDigestItemsForUser(testUserID)
	assert.ErrorContains(t, err, "delete failed")
	assert.Zero(t, n)
}

func TestCleanupStaleDigestItems_ReturnsRowsAffected(t *testing.T) {
	cutoff := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "digest_items" WHERE created_at < \$1`).
		WithArgs(cutoff).
		WillReturnResult(sqlmock.NewResult(0, 7))
	mock.ExpectCommit()

	n, err := CleanupStaleDigestItems(cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(7), n)
}

func TestCleanupStaleDigestItems_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "digest_items"`).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	n, err := CleanupStaleDigestItems(time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC))
	assert.ErrorContains(t, err, "delete failed")
	assert.Zero(t, n)
}
