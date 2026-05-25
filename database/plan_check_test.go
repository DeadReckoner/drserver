package database

import (
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newSqlmockDB returns a *gorm.DB backed by go-sqlmock using regex query
// matching. Inlined here (instead of using internal/testutil) to avoid an
// import cycle: testutil already imports the database package.
func newSqlmockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

const testUserID = "11111111-1111-1111-1111-111111111111"

// entitlementRow returns a sqlmock.Rows for SELECT * FROM entitlements.
// Column set must match the order Gorm uses (it pulls from the model's
// declared field order).
func entitlementRow(plan string, aiUsed int, periodStart time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"user_id", "plan", "source", "status",
		"current_period_start", "current_period_end", "trial_ends_at", "cancelled_at",
		"provider_customer_id", "provider_subscription_id", "product_id",
		"ai_generations_used", "ai_generations_period_start",
		"created_at", "updated_at",
	}).AddRow(
		testUserID, plan, "default", nil,
		nil, nil, nil, nil,
		nil, nil, nil,
		aiUsed, periodStart,
		time.Now(), time.Now(),
	)
}

// expectGetUserPlanContext stubs the read sequence GetUserPlanContext emits:
// SELECT entitlements, then (optionally) UPDATE counters when the period
// rolled, then COUNT roadmaps, then COUNT habits.
func expectGetUserPlanContext(t *testing.T, mock sqlmock.Sqlmock, plan string, aiUsed, roadmapCount, habitCount int) {
	t.Helper()
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(plan, aiUsed, now))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(testUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(roadmapCount))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WithArgs(testUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(habitCount))
}

// withMockDB installs a sqlmock-backed *gorm.DB as the package global for the
// duration of one test case.
func withMockDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	mockDB, mock := newSqlmockDB(t)
	restore := SetDBForTest(mockDB)
	t.Cleanup(restore)
	return mock
}

func TestCheckCreateRoadmap_FreeUnderLimit_Allowed(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanFree, 0, 0, 0)

	res, err := CheckCreateRoadmap(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res, "user with 0 active roadmaps should be allowed")
}

func TestCheckCreateRoadmap_FreeAtLimit_Blocked(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanFree, 0, 1, 0)

	res, err := CheckCreateRoadmap(testUserID)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "plan_limit", res.Code)
	assert.Equal(t, "active_roadmaps", res.Feature)
	assert.Equal(t, 1, res.Limit)
	assert.Equal(t, 1, res.Current)
}

func TestCheckCreateRoadmap_Paid_AlwaysAllowed(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanPaid, 0, 99, 0)

	res, err := CheckCreateRoadmap(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res, "paid users have no roadmap cap")
}

func TestCheckCreateHabit_FreeAtLimit_Blocked(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanFree, 0, 0, 3)

	res, err := CheckCreateHabit(testUserID)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "active_habits", res.Feature)
	assert.Equal(t, 3, res.Limit)
	assert.Equal(t, 3, res.Current)
}

func TestCheckCreateHabit_FreeUnderLimit_Allowed(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanFree, 0, 0, 2)

	res, err := CheckCreateHabit(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCheckAIGeneration_FreeAtLimit_Blocked(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanFree, 3, 0, 0)

	res, err := CheckAIGeneration(testUserID)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "ai_generations_per_month", res.Feature)
	assert.Equal(t, 3, res.Limit)
	assert.Equal(t, 3, res.Current)
}

func TestCheckAIGeneration_FreeUnderLimit_Allowed(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanFree, 2, 0, 0)

	res, err := CheckAIGeneration(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCheckAIGeneration_Paid_AlwaysAllowed(t *testing.T) {
	mock := withMockDB(t)
	expectGetUserPlanContext(t, mock, types.PlanPaid, 9999, 0, 0)

	res, err := CheckAIGeneration(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCheckCreateRoadmapAnchor_FreeWithExistingAnchor_Blocked(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanFree, 0, now))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2 AND anchor_id IS NOT NULL`).
		WithArgs(testUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	res, err := CheckCreateRoadmapAnchor(testUserID)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "roadmap_anchors", res.Feature)
}

func TestCheckCreateRoadmapAnchor_FreeNoExistingAnchor_Allowed(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanFree, 0, now))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2 AND anchor_id IS NOT NULL`).
		WithArgs(testUserID, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	res, err := CheckCreateRoadmapAnchor(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCheckCreateHabitAnchor_FreeAlwaysBlocked(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanFree, 0, now))

	res, err := CheckCreateHabitAnchor(testUserID)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "habit_anchors", res.Feature)
	assert.Equal(t, 0, res.Limit, "habit anchors have no numeric cap on free - the feature itself is paid-only")
}

func TestCheckCreateHabitAnchor_Paid_Allowed(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanPaid, 0, now))

	res, err := CheckCreateHabitAnchor(testUserID)
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCheckPaidFeature_Free_Blocked(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanFree, 0, now))

	res, err := CheckPaidFeature(testUserID, types.FeatureArchive, "Archive is Pro.")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, string(types.FeatureArchive), res.Feature)
	assert.Equal(t, "Archive is Pro.", res.Message)
}

func TestCheckPaidFeature_Paid_Allowed(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanPaid, 0, now))

	res, err := CheckPaidFeature(testUserID, types.FeatureCustomReminders, "")
	require.NoError(t, err)
	assert.Nil(t, res)
}
