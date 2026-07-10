package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoadmap_Restore_BlockedAtActiveRoadmapCap covers drmobile#183:
// reactivating an archived roadmap counts against the free active-roadmap
// cap, so a free user who already has an active roadmap gets the upgrade
// prompt instead of a second active roadmap.
func TestRoadmap_Restore_BlockedAtActiveRoadmapCap(t *testing.T) {
	t.Setenv("PRO_GATING_ENABLED", "true")

	mockDB, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(mockDB))

	// Free entitlement; period start "now" so the daily AI reset doesn't fire.
	mock.ExpectQuery(`SELECT \* FROM "entitlements" WHERE user_id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "plan", "source", "status",
			"ai_generations_used", "ai_generations_period_start",
		}).AddRow(testUser, types.PlanFree, "default", nil, 0, time.Now().UTC()))

	// One active roadmap already → at the free cap.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps" WHERE user_id = \$1 AND archived = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "habits" WHERE user_id = \$1 AND archived = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	restoreCalled := false
	svc := &testutil.FakeRoadmapService{
		RestoreFunc: func(c *gin.Context, _ string, _ bool) (types.Roadmap, error) {
			restoreCalled = true
			return types.Roadmap{}, nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/roadmaps/"+uuid.NewString()+"/restore", testUser, nil)

	require.Equal(t, http.StatusPaymentRequired, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "active_roadmaps")
	assert.False(t, restoreCalled, "restore must not run when the plan check blocks")
}
