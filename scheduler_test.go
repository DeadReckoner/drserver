package main

import (
	"testing"

	"dr/server/internal/testutil"
)

// TestSetupScheduler_RegistersAllJobs guards against accidentally deleting
// an AddFunc line — the count is the single source of truth for "how many
// jobs the notification system runs."
//
// Update the expected count when intentionally adding/removing a job.
func TestSetupScheduler_RegistersAllJobs(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	c := setupScheduler(db)
	t.Cleanup(func() {
		<-c.Stop().Done()
	})

	// 8 notification jobs + reap_stale_jobs (background-job recovery).
	const want = 9
	if got := len(c.Entries()); got != want {
		t.Errorf("setupScheduler registered %d jobs, want %d", got, want)
	}
}

// TestSetupScheduler_AllEntriesHaveSchedules sanity-checks that every
// registered job has a non-nil Schedule (i.e. AddFunc actually parsed the
// spec, didn't silently no-op).
func TestSetupScheduler_AllEntriesHaveSchedules(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	c := setupScheduler(db)
	t.Cleanup(func() {
		<-c.Stop().Done()
	})

	for i, entry := range c.Entries() {
		if entry.Schedule == nil {
			t.Errorf("entry %d: nil Schedule (cron spec failed to parse?)", i)
		}
		if entry.Job == nil {
			t.Errorf("entry %d: nil Job", i)
		}
	}
}
