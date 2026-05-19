package testutil

import (
	"testing"
	"time"

	"dr/server/database"
	"dr/server/types"
)

// FreezeTime pins types.Now and database.Now to the given instant for the
// duration of the test (restored in t.Cleanup). All overdue/streak logic that
// goes through these clocks becomes deterministic.
func FreezeTime(t *testing.T, frozen time.Time) {
	t.Helper()
	origTypes := types.Now
	origDB := database.Now
	types.Now = func() time.Time { return frozen }
	database.Now = func() time.Time { return frozen }
	t.Cleanup(func() {
		types.Now = origTypes
		database.Now = origDB
	})
}

// MustParseDate parses YYYY-MM-DD at UTC midnight, failing the test on error.
// Use this for fixture dates so test intent reads cleanly: MustParseDate(t, "2026-05-19").
func MustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("MustParseDate(%q): %v", s, err)
	}
	return parsed
}

// DatePtr returns a pointer to the given time. The roadmap model uses
// *time.Time for many date fields; this trims the boilerplate at call sites.
func DatePtr(t time.Time) *time.Time {
	return &t
}
