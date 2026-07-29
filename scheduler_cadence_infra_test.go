package main

// Cadence tests for setupScheduler, complementing the count/nil checks in
// scheduler_test.go. The cron is built but never Start()ed; each entry's
// Schedule is interrogated with a fixed reference time, so the tests are
// fully deterministic. Entries carry no names, so the cadences are asserted
// as a multiset rather than per-job.

import (
	"testing"
	"time"

	"dr/server/internal/testutil"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// infraCadenceRef is a fixed reference instant mid-minute, mid-hour, so the
// next firing of each cadence is unambiguous.
var infraCadenceRef = time.Date(2026, 7, 1, 10, 2, 30, 0, time.UTC)

// infraEntryCadence derives an entry's firing interval by asking its Schedule
// for the next two activations after ref.
func infraEntryCadence(e cron.Entry, ref time.Time) (first time.Time, interval time.Duration) {
	first = e.Schedule.Next(ref)
	second := e.Schedule.Next(first)
	return first, second.Sub(first)
}

// infraCadenceCron builds the scheduler against a sqlmock DB, mirroring
// scheduler_test.go. Job constructors don't touch the DB, so no expectations
// are needed.
func infraCadenceCron(t *testing.T) *cron.Cron {
	t.Helper()
	db, _ := testutil.NewSqlmockDBRegex(t)
	c := setupScheduler(db)
	t.Cleanup(func() {
		<-c.Stop().Done()
	})
	return c
}

// TestSetupScheduler_CadenceMultiset guards against fat-fingering a cron spec
// (e.g. "*/5" -> "5") without touching the count test: the 8 schedules must
// partition into exactly 4 every-minute, 3 every-5-minute, and 1 hourly job.
func TestSetupScheduler_CadenceMultiset(t *testing.T) {
	c := infraCadenceCron(t)
	entries := c.Entries()
	require.Len(t, entries, 8)

	counts := make(map[time.Duration]int)
	for _, e := range entries {
		_, interval := infraEntryCadence(e, infraCadenceRef)
		counts[interval]++
	}

	want := map[time.Duration]int{
		time.Minute:     4, // missed tasks, due digests, anchor batches, pending deliveries
		5 * time.Minute: 3, // falling behind, at-risk milestones, next milestone
		time.Hour:       1, // delivery-log cleanup
	}
	assert.Equal(t, want, counts)
}

// TestSetupScheduler_CadenceAlignment pins where each cadence lands on the
// clock: the hourly sweep fires on minute 0 and the 5-minute scans fire on
// minutes divisible by 5 (standard cron semantics, not "5 minutes from now").
func TestSetupScheduler_CadenceAlignment(t *testing.T) {
	c := infraCadenceCron(t)

	for i, e := range c.Entries() {
		first, interval := infraEntryCadence(e, infraCadenceRef)

		switch interval {
		case time.Hour:
			assert.Equal(t, 0, first.Minute(), "entry %d: hourly job must fire on the hour", i)
			assert.Equal(t, 0, first.Second(), "entry %d", i)
		case 5 * time.Minute:
			assert.Zero(t, first.Minute()%5, "entry %d: 5-minute job must fire on a 5-minute boundary, got minute %d", i, first.Minute())
			assert.Equal(t, 0, first.Second(), "entry %d", i)
		case time.Minute:
			assert.Equal(t, 0, first.Second(), "entry %d: per-minute job fires on the minute", i)
		default:
			t.Errorf("entry %d: unexpected cadence %v", i, interval)
		}
	}
}

// TestSetupScheduler_SchedulesRunInUTC verifies cron.WithLocation(time.UTC)
// actually took effect: every next activation is strictly after the reference
// time and expressed in UTC.
func TestSetupScheduler_SchedulesRunInUTC(t *testing.T) {
	c := infraCadenceCron(t)

	for i, e := range c.Entries() {
		first, _ := infraEntryCadence(e, infraCadenceRef)
		assert.True(t, first.After(infraCadenceRef), "entry %d: Next must be strictly after the reference time", i)
		assert.Equal(t, time.UTC, first.Location(), "entry %d: schedule must be evaluated in UTC", i)
	}
}
