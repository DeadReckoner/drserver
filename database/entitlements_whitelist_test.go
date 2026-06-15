package database

import (
	"testing"
	"time"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEntitlement_WhitelistedEmail_OverlaysPaid(t *testing.T) {
	t.Setenv("PRO_WHITELIST_DOMAINS", "") // use built-in default deadreckoner.io
	mock := withMockDB(t)
	now := time.Now().UTC()

	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanFree, 0, now))
	mock.ExpectQuery(`SELECT "email" FROM "users" WHERE user_id = \$1`).
		WithArgs(testUserID).
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("mike@deadreckoner.io"))

	ent, err := GetEntitlement(testUserID)
	require.NoError(t, err)
	assert.True(t, ent.IsPaid(), "whitelisted email should be reported as paid")
	assert.Equal(t, types.EntitlementSourceComp, ent.Source, "overlay tags source as comp")
}

func TestGetEntitlement_NonWhitelistedEmail_StaysFree(t *testing.T) {
	t.Setenv("PRO_WHITELIST_DOMAINS", "")
	mock := withMockDB(t)
	now := time.Now().UTC()

	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanFree, 0, now))
	mock.ExpectQuery(`SELECT "email" FROM "users" WHERE user_id = \$1`).
		WithArgs(testUserID).
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("someone@gmail.com"))

	ent, err := GetEntitlement(testUserID)
	require.NoError(t, err)
	assert.False(t, ent.IsPaid())
	assert.Equal(t, types.PlanFree, ent.Plan)
}

func TestGetEntitlement_AlreadyPaid_SkipsEmailLookup(t *testing.T) {
	mock := withMockDB(t)
	now := time.Now().UTC()

	// Only the entitlement SELECT is expected; a paid user must NOT trigger
	// the whitelist email lookup. sqlmock cleanup fails if an unexpected
	// query is issued, so this asserts the skip.
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WithArgs(testUserID, 1).
		WillReturnRows(entitlementRow(types.PlanPaid, 0, now))

	ent, err := GetEntitlement(testUserID)
	require.NoError(t, err)
	assert.True(t, ent.IsPaid())
}
