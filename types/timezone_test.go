package types_test

import (
	"testing"
	"time"

	"dr/server/internal/testutil"
	"dr/server/types"
)

func TestLocationOrUTC(t *testing.T) {
	if got := types.LocationOrUTC(""); got != time.UTC {
		t.Errorf("empty tz: got %v, want UTC", got)
	}
	if got := types.LocationOrUTC("Not/AZone"); got != time.UTC {
		t.Errorf("invalid tz: got %v, want UTC (fallback)", got)
	}
	if got := types.LocationOrUTC("UTC"); got.String() != "UTC" {
		t.Errorf("UTC tz: got %v, want UTC", got)
	}
	// A valid IANA zone should load when the system has tzdata; if it
	// doesn't, LocationOrUTC falls back to UTC. Either way it must not panic
	// and must return a usable location.
	if got := types.LocationOrUTC("America/Los_Angeles"); got == nil {
		t.Error("America/Los_Angeles: got nil location")
	}
}

// singleTaskRoadmap builds a started roadmap with one incomplete task due on
// the given instant — the minimal shape for exercising the overdue boundary.
func singleTaskRoadmap(due time.Time) types.Roadmap {
	start := due.AddDate(0, 0, -10)
	return testutil.NewRoadmap("u1",
		testutil.WithStartDate(start),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(due)),
		))),
	)
}

func TestComputeInLocation_AheadOfUTC_OverdueAtLocalMidnight(t *testing.T) {
	// User in UTC+9 (e.g. Tokyo). It's 16:00 UTC on May 19, which is already
	// 01:00 on May 20 for the user. A task due May 19 is past for them.
	frozen := time.Date(2026, 5, 19, 16, 0, 0, 0, time.UTC)
	testutil.FreezeTime(t, frozen)
	tokyo := time.FixedZone("UTC+9", 9*3600)

	due := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	// UTC view: still May 19, so the task is the current task, not overdue.
	rUTC := singleTaskRoadmap(due)
	rUTC.ComputeAllStatuses()
	if got := rUTC.Milestones[0].Tasks[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("UTC view: got %q, want %q (still today in UTC)", got, types.StatusInProgress)
	}

	// User-local view: already May 20, so the task is overdue.
	rTokyo := singleTaskRoadmap(due)
	rTokyo.ComputeAllStatusesInLocation(tokyo)
	if got := rTokyo.Milestones[0].Tasks[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("Tokyo view: got %q, want %q (already tomorrow locally)", got, types.StatusOverdue)
	}
}

func TestComputeInLocation_BehindUTC_NotOverdueUntilLocalMidnight(t *testing.T) {
	// User in UTC-7 (e.g. US Pacific, PDT). It's 05:00 UTC on May 20, which is
	// still 22:00 on May 19 for the user. A task due May 19 is NOT yet past
	// for them — this is the case that would otherwise show a false overdue.
	frozen := time.Date(2026, 5, 20, 5, 0, 0, 0, time.UTC)
	testutil.FreezeTime(t, frozen)
	pacific := time.FixedZone("UTC-7", -7*3600)

	due := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	// UTC view: already May 20, so the task reads as overdue (the bug for
	// behind-UTC users).
	rUTC := singleTaskRoadmap(due)
	rUTC.ComputeAllStatuses()
	if got := rUTC.Milestones[0].Tasks[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("UTC view: got %q, want %q (UTC has rolled over)", got, types.StatusOverdue)
	}

	// User-local view: still May 19, so the task is the current task, on time.
	rPacific := singleTaskRoadmap(due)
	rPacific.ComputeAllStatusesInLocation(pacific)
	if got := rPacific.Milestones[0].Tasks[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("Pacific view: got %q, want %q (still today locally)", got, types.StatusInProgress)
	}
}

func TestComputeInLocation_NilLocation_DefaultsToUTC(t *testing.T) {
	frozen := time.Date(2026, 5, 20, 5, 0, 0, 0, time.UTC)
	testutil.FreezeTime(t, frozen)

	due := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	rNil := singleTaskRoadmap(due)
	rNil.ComputeAllStatusesInLocation(nil)

	rUTC := singleTaskRoadmap(due)
	rUTC.ComputeAllStatuses()

	if rNil.Milestones[0].Tasks[0].Status != rUTC.Milestones[0].Tasks[0].Status {
		t.Errorf("nil location = %q, UTC = %q; nil should behave as UTC",
			rNil.Milestones[0].Tasks[0].Status, rUTC.Milestones[0].Tasks[0].Status)
	}
}
