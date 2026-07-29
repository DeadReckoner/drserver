package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// dbsocNudgeRows builds a nudges result set.
func dbsocNudgeRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"nudge_id", "sender_id", "receiver_id", "target_type", "target_id", "created_at",
	})
}

func dbsocExpectNudgeRateCount(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	// created_at > (now - 24h) is computed inside the function, so AnyArg.
	return mock.ExpectQuery(`SELECT count\(\*\) FROM "nudges" WHERE sender_id = \$1 AND target_type = \$2 AND target_id = \$3 AND created_at > \$4`).
		WithArgs(dbsocMeID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID, sqlmock.AnyArg())
}

func TestCheckNudgeRateLimit_UnderLimit_NotExceeded(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnRows(dbsocCountRow(types.NudgeRateLimitPerDay - 1))

	exceeded, err := CheckNudgeRateLimit(context.Background(), dbsocMeID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.NoError(t, err)
	assert.False(t, exceeded, "1 nudge in the last 24h is under the limit of 2")
}

func TestCheckNudgeRateLimit_AtLimit_Exceeded(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnRows(dbsocCountRow(types.NudgeRateLimitPerDay))

	exceeded, err := CheckNudgeRateLimit(context.Background(), dbsocMeID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.NoError(t, err)
	assert.True(t, exceeded, "count == limit is the boundary: already at 2 means no third nudge")
}

func TestCheckNudgeRateLimit_QueryError_Propagated(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnError(errors.New("count failed"))

	exceeded, err := CheckNudgeRateLimit(context.Background(), dbsocMeID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.Error(t, err)
	assert.False(t, exceeded)
}

func TestCreateNudge_RateLimitExceeded_NoInsert(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnRows(dbsocCountRow(2))
	// No INSERT expected.

	nudge, err := CreateNudge(context.Background(), dbsocMeID, dbsocFriendID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrNudgeRateLimitExceeded)
	assert.Nil(t, nudge)
}

func TestCreateNudge_RateCheckError_ShortCircuits(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnError(errors.New("count failed"))

	nudge, err := CreateNudge(context.Background(), dbsocMeID, dbsocFriendID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.Error(t, err)
	assert.NotErrorIs(t, err, types.ErrNudgeRateLimitExceeded)
	assert.Nil(t, nudge)
}

func TestCreateNudge_HappyPath_InsertsAndReturnsNudge(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnRows(dbsocCountRow(0))
	mock.ExpectBegin()
	// nudge_id and created_at are generated inside the function; GORM places
	// the function-default columns last and echoes them via RETURNING.
	mock.ExpectQuery(`INSERT INTO "nudges" \("sender_id","receiver_id","target_type","target_id","nudge_id","created_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\) RETURNING "nudge_id","created_at"`).
		WithArgs(dbsocMeID, dbsocFriendID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nudge_id", "created_at"}).
			AddRow("n1", time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)))
	mock.ExpectCommit()

	nudge, err := CreateNudge(context.Background(), dbsocMeID, dbsocFriendID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.NoError(t, err)
	require.NotNil(t, nudge)
	assert.NotEmpty(t, nudge.NudgeID)
	assert.Equal(t, dbsocMeID, nudge.SenderID)
	assert.Equal(t, dbsocFriendID, nudge.ReceiverID)
	assert.Equal(t, dbsocRoadmapID, nudge.TargetID)
}

func TestCreateNudge_InsertError_Propagated(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectNudgeRateCount(mock).WillReturnRows(dbsocCountRow(0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "nudges"`).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	nudge, err := CreateNudge(context.Background(), dbsocMeID, dbsocFriendID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID)
	require.Error(t, err)
	assert.Nil(t, nudge)
}

func TestGetNudgesReceived_DefaultLimitAndEnrichment(t *testing.T) {
	mock := withMockDB(t)
	created := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	// limit <= 0 defaults to 20.
	mock.ExpectQuery(`SELECT \* FROM "nudges" WHERE receiver_id = \$1 ORDER BY created_at DESC LIMIT \$2`).
		WithArgs(dbsocMeID, 20).
		WillReturnRows(dbsocNudgeRows().
			AddRow("n1", dbsocFriendID, dbsocMeID, types.NudgeTargetTypeRoadmap, dbsocRoadmapID, created))
	// Sender preload.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocFriendID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	// Enrichment: roadmap goal used as the target title.
	mock.ExpectQuery(`SELECT "roadmap_id","goal" FROM "roadmaps" WHERE roadmap_id IN \(\$1\)`).
		WithArgs(dbsocRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "goal"}).AddRow(dbsocRoadmapID, "Run a marathon"))

	resp, err := GetNudgesReceived(context.Background(), dbsocMeID, 0)
	require.NoError(t, err)
	require.Len(t, resp, 1)
	assert.Equal(t, "Run a marathon", resp[0].TargetTitle)
	require.NotNil(t, resp[0].Sender)
	assert.Equal(t, dbsocFriendID, resp[0].Sender.UserID)
}

func TestGetNudgesReceived_ExplicitLimitHonored(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "nudges" WHERE receiver_id = \$1 ORDER BY created_at DESC LIMIT \$2`).
		WithArgs(dbsocMeID, 5).
		WillReturnRows(dbsocNudgeRows())

	resp, err := GetNudgesReceived(context.Background(), dbsocMeID, 5)
	require.NoError(t, err)
	assert.Empty(t, resp)
}

func TestGetNudgesReceived_QueryError_NilAndError(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "nudges" WHERE receiver_id = \$1`).
		WillReturnError(errors.New("find failed"))

	resp, err := GetNudgesReceived(context.Background(), dbsocMeID, 0)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetNudgesSent_FiltersOnSenderWithReceiverPreload(t *testing.T) {
	mock := withMockDB(t)
	created := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM "nudges" WHERE sender_id = \$1 ORDER BY created_at DESC LIMIT \$2`).
		WithArgs(dbsocMeID, 5).
		WillReturnRows(dbsocNudgeRows().
			AddRow("n2", dbsocMeID, dbsocFriendID, types.NudgeTargetTypeHabit, dbsocHabitID, created))
	// Receiver preload.
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE "users"\."user_id" = \$1`).
		WithArgs(dbsocFriendID).
		WillReturnRows(dbsocUserRow(dbsocFriendID))
	// Habit nudges are titled from the habit's title column.
	mock.ExpectQuery(`SELECT "habit_id","title" FROM "habits" WHERE habit_id IN \(\$1\)`).
		WithArgs(dbsocHabitID).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "title"}).AddRow(dbsocHabitID, "Read 20 minutes"))

	resp, err := GetNudgesSent(context.Background(), dbsocMeID, 5)
	require.NoError(t, err)
	require.Len(t, resp, 1)
	assert.Equal(t, "Read 20 minutes", resp[0].TargetTitle)
	require.NotNil(t, resp[0].Receiver)
	assert.Equal(t, dbsocFriendID, resp[0].Receiver.UserID)
}

func TestGetNudgesSent_ZeroLimitDefaultsTo20(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "nudges" WHERE sender_id = \$1 ORDER BY created_at DESC LIMIT \$2`).
		WithArgs(dbsocMeID, 20).
		WillReturnRows(dbsocNudgeRows())

	resp, err := GetNudgesSent(context.Background(), dbsocMeID, -3)
	require.NoError(t, err)
	assert.Empty(t, resp)
}

func TestEnrichNudges_EmptyInput_NoQueries(t *testing.T) {
	withMockDB(t) // no expectations

	resp, err := enrichNudgesWithTargetTitles(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp)
}

func TestEnrichNudges_MixedTargets_TitlesMappedPerType(t *testing.T) {
	mock := withMockDB(t)
	nudges := []types.Nudge{
		{NudgeID: "n1", TargetType: types.NudgeTargetTypeRoadmap, TargetID: dbsocRoadmapID},
		{NudgeID: "n2", TargetType: types.NudgeTargetTypeHabit, TargetID: dbsocHabitID},
		{NudgeID: "n3", TargetType: "unknown", TargetID: dbsocOtherID},
	}
	mock.ExpectQuery(`SELECT "roadmap_id","goal" FROM "roadmaps" WHERE roadmap_id IN \(\$1\)`).
		WithArgs(dbsocRoadmapID).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "goal"}).AddRow(dbsocRoadmapID, "Run a marathon"))
	mock.ExpectQuery(`SELECT "habit_id","title" FROM "habits" WHERE habit_id IN \(\$1\)`).
		WithArgs(dbsocHabitID).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "title"}).AddRow(dbsocHabitID, "Read 20 minutes"))

	resp, err := enrichNudgesWithTargetTitles(context.Background(), nudges)
	require.NoError(t, err)
	require.Len(t, resp, 3)
	assert.Equal(t, "Run a marathon", resp[0].TargetTitle, "roadmap nudges are titled from the roadmap goal")
	assert.Equal(t, "Read 20 minutes", resp[1].TargetTitle, "habit nudges are titled from the habit title")
	assert.Equal(t, "", resp[2].TargetTitle, "unknown target types get an empty title")
}

func TestEnrichNudges_TitleFetchErrorTolerated(t *testing.T) {
	mock := withMockDB(t)
	nudges := []types.Nudge{
		{NudgeID: "n1", TargetType: types.NudgeTargetTypeRoadmap, TargetID: dbsocRoadmapID},
	}
	mock.ExpectQuery(`SELECT "roadmap_id","goal" FROM "roadmaps"`).
		WillReturnError(errors.New("titles failed"))

	// A title-fetch failure degrades to empty titles instead of failing the
	// whole listing.
	resp, err := enrichNudgesWithTargetTitles(context.Background(), nudges)
	require.NoError(t, err)
	require.Len(t, resp, 1)
	assert.Equal(t, "", resp[0].TargetTitle)
}

func dbsocExpectRoadmapForNudge(mock sqlmock.Sqlmock, anchorID any) {
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbsocRoadmapID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"roadmap_id", "user_id", "goal", "anchor_id"}).
			AddRow(dbsocRoadmapID, dbsocFriendID, "Run a marathon", anchorID))
}

func TestGetRoadmapForNudge_NotFound_GormErrorPassthrough(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "roadmaps" WHERE roadmap_id = \$1`).
		WithArgs(dbsocRoadmapID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := GetRoadmapForNudge(context.Background(), dbsocRoadmapID, dbsocMeID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestGetRoadmapForNudge_NoAnchorAssigned_ErrNotAnchor(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectRoadmapForNudge(mock, nil)

	_, err := GetRoadmapForNudge(context.Background(), dbsocRoadmapID, dbsocMeID)
	assert.ErrorIs(t, err, types.ErrNotAnchor)
}

func TestGetRoadmapForNudge_CallerNotAnchor_ErrNotAnchor(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectRoadmapForNudge(mock, dbsocOtherID)

	_, err := GetRoadmapForNudge(context.Background(), dbsocRoadmapID, dbsocMeID)
	assert.ErrorIs(t, err, types.ErrNotAnchor)
}

func TestGetRoadmapForNudge_CallerIsAnchor_ReturnsRoadmap(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectRoadmapForNudge(mock, dbsocMeID)

	roadmap, err := GetRoadmapForNudge(context.Background(), dbsocRoadmapID, dbsocMeID)
	require.NoError(t, err)
	require.NotNil(t, roadmap)
	assert.Equal(t, "Run a marathon", roadmap.Goal)
}

func dbsocExpectHabitForNudge(mock sqlmock.Sqlmock, anchorID any) {
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1`).
		WithArgs(dbsocHabitID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"habit_id", "user_id", "title", "anchor_id"}).
			AddRow(dbsocHabitID, dbsocFriendID, "Read 20 minutes", anchorID))
}

func TestGetHabitForNudge_NotFound_GormErrorPassthrough(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "habits" WHERE habit_id = \$1`).
		WithArgs(dbsocHabitID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := GetHabitForNudge(context.Background(), dbsocHabitID, dbsocMeID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestGetHabitForNudge_NoAnchorAssigned_ErrNotAnchor(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectHabitForNudge(mock, nil)

	_, err := GetHabitForNudge(context.Background(), dbsocHabitID, dbsocMeID)
	assert.ErrorIs(t, err, types.ErrNotAnchor)
}

func TestGetHabitForNudge_CallerNotAnchor_ErrNotAnchor(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectHabitForNudge(mock, dbsocOtherID)

	_, err := GetHabitForNudge(context.Background(), dbsocHabitID, dbsocMeID)
	assert.ErrorIs(t, err, types.ErrNotAnchor)
}

func TestGetHabitForNudge_CallerIsAnchor_ReturnsHabit(t *testing.T) {
	mock := withMockDB(t)
	dbsocExpectHabitForNudge(mock, dbsocMeID)

	habit, err := GetHabitForNudge(context.Background(), dbsocHabitID, dbsocMeID)
	require.NoError(t, err)
	require.NotNil(t, habit)
	assert.Equal(t, "Read 20 minutes", habit.Title)
}
