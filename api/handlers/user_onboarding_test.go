package handlers_test

import (
	"net/http"
	"testing"

	"dr/server/database"
	"dr/server/internal/testutil"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestUpdateUser_PersistsOnboardingCompleted covers drmobile#185: onboarding
// completion is stored on the user record via the existing PATCH /users/:id,
// so re-login or a new device doesn't replay onboarding.
func TestUpdateUser_PersistsOnboardingCompleted(t *testing.T) {
	const userID = "12121212-3434-5656-7878-909090909090"

	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	mock.ExpectQuery(`SELECT \* FROM "users" WHERE user_id = \$1`).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "onboarding_completed"}).
			AddRow(userID, false))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "users" SET "onboarding_completed"=\$1 WHERE "user_id" = \$2`).
		WithArgs(true, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	router := testutil.NewTestRouterAs(db, userID)
	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/users/"+userID, userID,
		map[string]any{"onboarding_completed": true})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
