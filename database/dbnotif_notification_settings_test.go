package database

// Tests for notification_settings.go: the settings row + ensure-defaults path,
// per-target overrides (friend / anchored-goal), and the delivery-log
// idempotent-send primitives (RecordDelivery / RecordDeliveryAndSend) that the
// whole notification scheduler leans on.

import (
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dbnotifSettingsRow is a partial notification_settings result set — gorm only
// scans the columns present, so unlisted columns stay zero-valued.
func dbnotifSettingsRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"user_id", "push_enabled", "email_enabled",
		"quiet_hours_start", "quiet_hours_end", "digest_frequency", "digest_delivery_hour",
	}).AddRow(testUserID, true, false, "21:00", "07:00", "daily", 9)
}

// dbnotifOverrideRow is a partial notification_overrides result set.
func dbnotifOverrideRow(friendID, anchoredRoadmapID any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "friend_id", "anchored_roadmap_id"}).
		AddRow(dbnotifRowID, testUserID, friendID, anchoredRoadmapID)
}

// =============================================================================
// GetNotificationSettings / EnsureNotificationSettings
// =============================================================================

func TestGetNotificationSettings_Found(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_settings" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(dbnotifSettingsRow())

	settings, err := GetNotificationSettings(testUserID)
	require.NoError(t, err)
	assert.Equal(t, testUserID, settings.UserID)
	assert.Equal(t, types.DigestFrequency("daily"), settings.DigestFrequency)
}

func TestEnsureNotificationSettings_ExistingRow_NoInsert(t *testing.T) {
	mock := withMockDB(t)
	// Only the SELECT — an INSERT would trip the unmet-expectations check.
	mock.ExpectQuery(`SELECT \* FROM "notification_settings" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(dbnotifSettingsRow())

	settings, err := EnsureNotificationSettings(testUserID)
	require.NoError(t, err)
	assert.Equal(t, "21:00", settings.QuietHoursStart, "stored row wins over in-memory defaults")
}

func TestEnsureNotificationSettings_Missing_SeedsDefaults(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_settings" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"})) // not found
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "notification_settings"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	settings, err := EnsureNotificationSettings(testUserID)
	require.NoError(t, err)
	// The seeded defaults returned to the caller without a follow-up read.
	assert.Equal(t, testUserID, settings.UserID)
	assert.True(t, settings.PushEnabled)
	assert.True(t, settings.EmailEnabled)
	assert.False(t, settings.QuietHoursEnabled)
	assert.Equal(t, "22:00", settings.QuietHoursStart)
	assert.Equal(t, "08:00", settings.QuietHoursEnd)
	assert.Equal(t, types.DigestFrequencyNone, settings.DigestFrequency)
	assert.Equal(t, 8, settings.DigestDeliveryHour)
	assert.Equal(t, 1, settings.DigestDeliveryDayOfWeek)
	assert.True(t, settings.FAndAEnabled)
	assert.True(t, settings.ApplyToAllFriends)
}

func TestEnsureNotificationSettings_SelectError_PropagatesWithoutInsert(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_settings"`).
		WillReturnError(errors.New("select failed"))

	_, err := EnsureNotificationSettings(testUserID)
	assert.ErrorContains(t, err, "select failed")
}

func TestEnsureNotificationSettings_InsertError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_settings"`).
		WithArgs(testUserID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "notification_settings"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	_, err := EnsureNotificationSettings(testUserID)
	assert.ErrorContains(t, err, "insert failed")
}

// =============================================================================
// UpdateNotificationSettings / UpdateNotificationSettingsMap
// =============================================================================

func TestUpdateNotificationSettings_StructPatch(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// Struct-based Updates: only non-zero fields plus auto updated_at.
	mock.ExpectExec(`UPDATE "notification_settings" SET "digest_delivery_hour"=\$1,"updated_at"=\$2 WHERE user_id = \$3`).
		WithArgs(9, dbnotifNonZeroTime{}, testUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := UpdateNotificationSettings(testUserID, types.NotificationSettings{DigestDeliveryHour: 9})
	require.NoError(t, err)
}

func TestUpdateNotificationSettingsMap_EmptyPatch_NoSQL(t *testing.T) {
	withMockDB(t) // zero expectations

	require.NoError(t, UpdateNotificationSettingsMap(testUserID, map[string]any{}))
}

func TestUpdateNotificationSettingsMap_SetsFalseAndInjectsUpdatedAt(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// Map path exists exactly so booleans can be set to false; the function
	// injects updated_at itself. Map keys are sorted by GORM.
	mock.ExpectExec(`UPDATE "notification_settings" SET "push_enabled"=\$1,"updated_at"=\$2 WHERE user_id = \$3`).
		WithArgs(false, dbnotifNonZeroTime{}, testUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := UpdateNotificationSettingsMap(testUserID, map[string]any{"push_enabled": false})
	require.NoError(t, err)
}

func TestUpdateNotificationSettingsMap_DBError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_settings"`).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	err := UpdateNotificationSettingsMap(testUserID, map[string]any{"email_enabled": true})
	assert.ErrorContains(t, err, "update failed")
}

// =============================================================================
// Overrides: list / get
// =============================================================================

func TestListNotificationOverrides(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_overrides" WHERE user_id = \$1`).
		WithArgs(testUserID).
		WillReturnRows(dbnotifOverrideRow(dbnotifFriendID, nil))

	overrides, err := ListNotificationOverrides(testUserID)
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	assert.Equal(t, dbnotifFriendID, *overrides[0].FriendID)
}

func TestGetFriendOverride_FoundNotFoundAndError(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "notification_overrides" WHERE user_id = \$1 AND friend_id = \$2`).
			WithArgs(testUserID, dbnotifFriendID, 1).
			WillReturnRows(dbnotifOverrideRow(dbnotifFriendID, nil))

		o, err := GetFriendOverride(testUserID, dbnotifFriendID)
		require.NoError(t, err)
		require.NotNil(t, o)
		assert.Equal(t, dbnotifRowID, o.ID)
	})

	t.Run("not found is nil,nil", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "notification_overrides" WHERE user_id = \$1 AND friend_id = \$2`).
			WithArgs(testUserID, dbnotifFriendID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		o, err := GetFriendOverride(testUserID, dbnotifFriendID)
		require.NoError(t, err)
		assert.Nil(t, o)
	})

	t.Run("db error", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "notification_overrides"`).
			WillReturnError(errors.New("select failed"))

		o, err := GetFriendOverride(testUserID, dbnotifFriendID)
		assert.ErrorContains(t, err, "select failed")
		assert.Nil(t, o)
	})
}

func TestGetAnchoredGoalOverride_FoundNotFoundAndError(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "notification_overrides" WHERE user_id = \$1 AND anchored_roadmap_id = \$2`).
			WithArgs(testUserID, dbnotifRoadmapID, 1).
			WillReturnRows(dbnotifOverrideRow(nil, dbnotifRoadmapID))

		o, err := GetAnchoredGoalOverride(testUserID, dbnotifRoadmapID)
		require.NoError(t, err)
		require.NotNil(t, o)
		assert.Equal(t, dbnotifRoadmapID, *o.AnchoredRoadmapID)
	})

	t.Run("not found is nil,nil", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "notification_overrides" WHERE user_id = \$1 AND anchored_roadmap_id = \$2`).
			WithArgs(testUserID, dbnotifRoadmapID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		o, err := GetAnchoredGoalOverride(testUserID, dbnotifRoadmapID)
		require.NoError(t, err)
		assert.Nil(t, o)
	})

	t.Run("db error", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectQuery(`SELECT \* FROM "notification_overrides"`).
			WillReturnError(errors.New("select failed"))

		o, err := GetAnchoredGoalOverride(testUserID, dbnotifRoadmapID)
		assert.ErrorContains(t, err, "select failed")
		assert.Nil(t, o)
	})
}

// =============================================================================
// Overrides: upsert / delete
// =============================================================================

func TestUpsertFriendOverride_ShapeValidation_NoSQL(t *testing.T) {
	withMockDB(t) // zero expectations

	friendID, roadmapID := dbnotifFriendID, dbnotifRoadmapID
	for _, tc := range []struct {
		name string
		o    types.NotificationOverride
	}{
		{"nil FriendID", types.NotificationOverride{UserID: testUserID}},
		{"both targets set", types.NotificationOverride{UserID: testUserID, FriendID: &friendID, AnchoredRoadmapID: &roadmapID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, UpsertFriendOverride(tc.o))
		})
	}
}

func TestUpsertFriendOverride_ValidInput_ConflictAssignsPrefsOnly(t *testing.T) {
	friendID := dbnotifFriendID
	mock := withMockDB(t)
	mock.ExpectBegin()
	// The DO UPDATE list assigns exactly prefs/mute_until/updated_at from the
	// excluded row — created_at and the target columns are never rewritten.
	mock.ExpectQuery(`INSERT INTO "notification_overrides" .* ON CONFLICT \("user_id","friend_id"\) DO UPDATE SET "prefs"="excluded"\."prefs","mute_until"="excluded"\."mute_until","updated_at"="excluded"\."updated_at" RETURNING`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(dbnotifRowID))
	mock.ExpectCommit()

	err := UpsertFriendOverride(types.NotificationOverride{
		UserID:   testUserID,
		FriendID: &friendID,
		Prefs:    types.PrefOverrides{},
	})
	require.NoError(t, err)
}

func TestUpsertAnchoredGoalOverride_ShapeValidation_NoSQL(t *testing.T) {
	withMockDB(t) // zero expectations

	friendID, roadmapID := dbnotifFriendID, dbnotifRoadmapID
	for _, tc := range []struct {
		name string
		o    types.NotificationOverride
	}{
		{"nil AnchoredRoadmapID", types.NotificationOverride{UserID: testUserID}},
		{"both targets set", types.NotificationOverride{UserID: testUserID, FriendID: &friendID, AnchoredRoadmapID: &roadmapID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, UpsertAnchoredGoalOverride(tc.o))
		})
	}
}

func TestUpsertAnchoredGoalOverride_ValidInput(t *testing.T) {
	roadmapID := dbnotifRoadmapID
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_overrides" .* ON CONFLICT \("user_id","anchored_roadmap_id"\) DO UPDATE SET "prefs"="excluded"\."prefs","mute_until"="excluded"\."mute_until","updated_at"="excluded"\."updated_at" RETURNING`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(dbnotifRowID))
	mock.ExpectCommit()

	err := UpsertAnchoredGoalOverride(types.NotificationOverride{
		UserID:            testUserID,
		AnchoredRoadmapID: &roadmapID,
		Prefs:             types.PrefOverrides{},
	})
	require.NoError(t, err)
}

func TestDeleteFriendOverride(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notification_overrides" WHERE user_id = \$1 AND friend_id = \$2`).
		WithArgs(testUserID, dbnotifFriendID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteFriendOverride(testUserID, dbnotifFriendID))
}

func TestDeleteAnchoredGoalOverride(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notification_overrides" WHERE user_id = \$1 AND anchored_roadmap_id = \$2`).
		WithArgs(testUserID, dbnotifRoadmapID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, DeleteAnchoredGoalOverride(testUserID, dbnotifRoadmapID))
}

// =============================================================================
// RecordDelivery — the idempotent-send primitive
// =============================================================================

func TestRecordDelivery_FreshInsert_CallerShouldSend(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_delivery_log" .* ON CONFLICT DO NOTHING RETURNING`).
		WithArgs(testUserID, "missed_progress", "dk-1", "push").
		WillReturnRows(sqlmock.NewRows([]string{"id", "delivered_at"}).AddRow(dbnotifRowID, time.Now()))
	mock.ExpectCommit()

	inserted, err := RecordDelivery(testUserID, "missed_progress", "dk-1", "push")
	require.NoError(t, err)
	assert.True(t, inserted)
}

func TestRecordDelivery_Conflict_CallerShouldSkip(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	// ON CONFLICT DO NOTHING: no row comes back, RowsAffected is 0.
	mock.ExpectQuery(`INSERT INTO "notification_delivery_log" .* ON CONFLICT DO NOTHING RETURNING`).
		WithArgs(testUserID, "missed_progress", "dk-1", "push").
		WillReturnRows(sqlmock.NewRows([]string{"id", "delivered_at"}))
	mock.ExpectCommit()

	inserted, err := RecordDelivery(testUserID, "missed_progress", "dk-1", "push")
	require.NoError(t, err)
	assert.False(t, inserted, "existing row means already sent — skip")
}

func TestRecordDelivery_DBError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_delivery_log"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	inserted, err := RecordDelivery(testUserID, "missed_progress", "dk-1", "push")
	assert.ErrorContains(t, err, "insert failed")
	assert.False(t, inserted)
}

// =============================================================================
// RecordDeliveryAndSend — dedup slot + send in one transaction
// =============================================================================

func TestRecordDeliveryAndSend_FreshInsert_SendsAndCommits(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_delivery_log" .* ON CONFLICT DO NOTHING RETURNING`).
		WithArgs(testUserID, "streak_at_risk", "dk-2", "email").
		WillReturnRows(sqlmock.NewRows([]string{"id", "delivered_at"}).AddRow(dbnotifRowID, time.Now()))
	mock.ExpectCommit()

	sendCalls := 0
	sent, err := RecordDeliveryAndSend(testUserID, "streak_at_risk", "dk-2", "email", func() error {
		sendCalls++
		return nil
	})
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, 1, sendCalls)
}

func TestRecordDeliveryAndSend_Conflict_SkipsSendStillCommits(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_delivery_log" .* ON CONFLICT DO NOTHING RETURNING`).
		WithArgs(testUserID, "streak_at_risk", "dk-2", "email").
		WillReturnRows(sqlmock.NewRows([]string{"id", "delivered_at"}))
	mock.ExpectCommit() // no-op is not an error: the transaction commits

	sent, err := RecordDeliveryAndSend(testUserID, "streak_at_risk", "dk-2", "email", func() error {
		t.Fatal("send callback must not run on the conflict path")
		return nil
	})
	require.NoError(t, err)
	assert.False(t, sent)
}

func TestRecordDeliveryAndSend_SendFailure_RollsBackDedupSlot(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_delivery_log" .* ON CONFLICT DO NOTHING RETURNING`).
		WithArgs(testUserID, "streak_at_risk", "dk-2", "email").
		WillReturnRows(sqlmock.NewRows([]string{"id", "delivered_at"}).AddRow(dbnotifRowID, time.Now()))
	// The dedup-slot-reclaim contract: a failed send rolls the log row back
	// so the next scan retries naturally.
	mock.ExpectRollback()

	sent, err := RecordDeliveryAndSend(testUserID, "streak_at_risk", "dk-2", "email", func() error {
		return errors.New("smtp down")
	})
	assert.ErrorContains(t, err, "smtp down")
	assert.False(t, sent)
}

// =============================================================================
// RecentDeliveries / CleanupDeliveryLog / CountRecentPushes / IsAnchorOfRoadmap
// =============================================================================

func TestRecentDeliveries_OrderedNewestFirst(t *testing.T) {
	since := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_delivery_log" WHERE user_id = \$1 AND notification_type = \$2 AND delivered_at >= \$3 ORDER BY delivered_at DESC`).
		WithArgs(testUserID, "missed_progress", since).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "notification_type"}).
			AddRow("d-2", testUserID, "missed_progress").
			AddRow("d-1", testUserID, "missed_progress"))

	entries, err := RecentDeliveries(testUserID, "missed_progress", since)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "d-2", entries[0].ID)
}

func TestRecentDeliveries_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "notification_delivery_log"`).
		WillReturnError(errors.New("select failed"))

	_, err := RecentDeliveries(testUserID, "missed_progress", time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC))
	assert.ErrorContains(t, err, "select failed")
}

func TestCleanupDeliveryLog_ReturnsRowsAffected(t *testing.T) {
	cutoff := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notification_delivery_log" WHERE delivered_at < \$1`).
		WithArgs(cutoff).
		WillReturnResult(sqlmock.NewResult(0, 9))
	mock.ExpectCommit()

	n, err := CleanupDeliveryLog(cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(9), n)
}

func TestCleanupDeliveryLog_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notification_delivery_log"`).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	n, err := CleanupDeliveryLog(time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC))
	assert.ErrorContains(t, err, "delete failed")
	assert.Zero(t, n)
}

func TestCountRecentPushes_PinsPushChannelAndSince(t *testing.T) {
	since := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	mock := withMockDB(t)
	// The channel arg must be exactly 'push' — email deliveries don't count
	// against the push frequency cap.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "notification_delivery_log" WHERE user_id = \$1 AND channel = \$2 AND delivered_at >= \$3`).
		WithArgs(testUserID, types.NotificationChannelPush, since).
		WillReturnRows(dbnotifCountRow(3))

	n, err := CountRecentPushes(testUserID, since)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

func TestCountRecentPushes_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "notification_delivery_log"`).
		WillReturnError(errors.New("count failed"))

	n, err := CountRecentPushes(testUserID, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC))
	assert.ErrorContains(t, err, "count failed")
	assert.Zero(t, n)
}

func TestIsAnchorOfRoadmap(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		want  bool
	}{
		{"is anchor", 1, true},
		{"is not anchor", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := withMockDB(t)
			mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps" WHERE roadmap_id = \$1 AND anchor_id = \$2`).
				WithArgs(dbnotifRoadmapID, testUserID).
				WillReturnRows(dbnotifCountRow(tc.count))

			ok, err := IsAnchorOfRoadmap(testUserID, dbnotifRoadmapID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ok)
		})
	}
}

func TestIsAnchorOfRoadmap_Error(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps"`).
		WillReturnError(errors.New("count failed"))

	ok, err := IsAnchorOfRoadmap(testUserID, dbnotifRoadmapID)
	assert.ErrorContains(t, err, "count failed")
	assert.False(t, ok)
}
