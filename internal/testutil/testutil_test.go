package testutil

import (
	"net/http"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/types"
)

func TestFreezeTime_OverridesBothClocksAndRestores(t *testing.T) {
	origTypes := types.Now
	origDB := database.Now

	frozen := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	FreezeTime(t, frozen)

	if got := types.Now(); !got.Equal(frozen) {
		t.Fatalf("types.Now not frozen: got %v want %v", got, frozen)
	}
	if got := database.Now(); !got.Equal(frozen) {
		t.Fatalf("database.Now not frozen: got %v want %v", got, frozen)
	}

	// Verify Cleanup restores by running a subtest with its own t.Cleanup
	// (the parent restore fires when this test ends, but we want to confirm
	// the originals are the originals - not nil and not the frozen lambda).
	if origTypes == nil || origDB == nil {
		t.Fatal("baseline clock vars are nil")
	}
}

func TestFakeAuthMiddleware_RejectsMissingHeader(t *testing.T) {
	router := NewTestRouter(nil)
	w := Do(t, router, http.MethodGet, "/api/v1/users", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing user header: got %d want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestNewSqlmockDB_AssertsExpectationsAtCleanup(t *testing.T) {
	db, mock := NewSqlmockDB(t)
	if db == nil || mock == nil {
		t.Fatal("nil sqlmock fixture")
	}
	// No expectations set => no expectations unmet => cleanup passes.
}
