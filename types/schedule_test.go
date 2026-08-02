package types_test

import (
	"testing"
	"time"

	"dr/server/types"
)

// Tests for the scheduling primitives that decide which calendar day a
// milestone or task lands on: ScheduleDayIn, CalendarDaysBetween,
// MinScheduledDay and ScheduleOffset.
//
// These exist because both halves of "the roadmap starts tomorrow" lived here:
// AI-generated plans number their days from 1 while Day is an offset from the
// start day, and "today" used to be read off the server's UTC clock rather than
// the user's.

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata unavailable for %s: %v", name, err)
	}
	return loc
}

// --- ScheduleDayIn -----------------------------------------------------------

func TestScheduleDayIn_UsesTheLocalCalendarDay(t *testing.T) {
	ny := mustLoad(t, "America/New_York")

	// 9pm in New York is already tomorrow in UTC. The anchor must follow the
	// user's day, not the server's — this is the realign off-by-one.
	evening := time.Date(2026, 7, 29, 1, 30, 0, 0, time.UTC) // Jul 28, 21:30 EDT
	got := types.ScheduleDayIn(evening, ny)

	if y, m, d := got.In(ny).Date(); y != 2026 || m != time.July || d != 28 {
		t.Errorf("local day: got %04d-%02d-%02d, want 2026-07-28", y, m, d)
	}
	if h := got.In(ny).Hour(); h != types.ScheduleAnchorHour {
		t.Errorf("anchor hour: got %d, want %d", h, types.ScheduleAnchorHour)
	}
}

// The anchor exists so the stored instant lands on the same calendar day
// whether it is read locally (the mobile client) or in UTC (the `::date` casts
// in the notification and stats SQL). Local midnight would break the UTC side
// for every zone east of Greenwich.
func TestScheduleDayIn_LocalAndUTCDaysAgree(t *testing.T) {
	for _, name := range []string{
		"Pacific/Pago_Pago",   // UTC-11
		"America/Los_Angeles", // UTC-8/-7
		"UTC",
		"Europe/Berlin", // UTC+1/+2
		"Asia/Tokyo",    // UTC+9
		"Pacific/Fiji",  // UTC+12
	} {
		loc := mustLoad(t, name)
		instant := time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC)
		anchored := types.ScheduleDayIn(instant, loc)

		ly, lm, ld := anchored.In(loc).Date()
		uy, um, ud := anchored.UTC().Date()
		if ly != uy || lm != um || ld != ud {
			t.Errorf("%s: local day %04d-%02d-%02d != UTC day %04d-%02d-%02d",
				name, ly, lm, ld, uy, um, ud)
		}
	}
}

func TestScheduleDayIn_NilLocationIsUTC(t *testing.T) {
	instant := time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC)
	if got, want := types.ScheduleDayIn(instant, nil), types.ScheduleDayIn(instant, time.UTC); !got.Equal(want) {
		t.Errorf("nil loc: got %v, want %v", got, want)
	}
}

// --- CalendarDaysBetween -----------------------------------------------------

func TestCalendarDaysBetween(t *testing.T) {
	ny := mustLoad(t, "America/New_York")

	tests := []struct {
		name     string
		from, to time.Time
		loc      *time.Location
		want     int
	}{
		{
			name: "same local day, different UTC days",
			// Jul 28 21:30 EDT and Jul 28 08:00 EDT are one UTC day apart.
			from: time.Date(2026, 7, 29, 1, 30, 0, 0, time.UTC),
			to:   time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
			loc:  ny,
			want: 0,
		},
		{
			name: "forward across a DST end",
			// Oct 25 -> Nov 8 spans the 2026-11-01 fall-back: 14 calendar days
			// but 14*24+1 hours.
			from: time.Date(2026, 10, 25, 16, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 11, 8, 17, 0, 0, 0, time.UTC),
			loc:  ny,
			want: 14,
		},
		{
			name: "backward",
			from: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
			loc:  time.UTC,
			want: -3,
		},
		{
			name: "nil location falls back to UTC",
			from: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
			loc:  nil,
			want: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := types.CalendarDaysBetween(tc.from, tc.to, tc.loc); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// --- MinScheduledDay / ScheduleOffset ----------------------------------------

func roadmapWithDays(milestoneDay int, taskDays ...int) types.Roadmap {
	tasks := make([]types.Task, len(taskDays))
	for i, d := range taskDays {
		tasks[i] = types.Task{Day: d}
	}
	return types.Roadmap{
		Milestones: []types.Milestone{{Day: milestoneDay, Tasks: tasks}},
	}
}

func TestMinScheduledDay(t *testing.T) {
	tests := []struct {
		name string
		rm   types.Roadmap
		want int
	}{
		{
			name: "generated plan numbered from 1",
			rm:   roadmapWithDays(1, 1, 4, 9),
			want: 1,
		},
		{
			name: "imported plan already numbered from 0",
			rm:   roadmapWithDays(0, 0, 3, 7),
			want: 0,
		},
		{
			name: "task earlier than its milestone wins",
			rm:   roadmapWithDays(5, 2, 6),
			want: 2,
		},
		{
			name: "negative days clamp to 0",
			rm:   roadmapWithDays(-3, -1),
			want: 0,
		},
		{
			name: "phase milestones counted too",
			rm: types.Roadmap{
				Phases: []types.Phase{{Milestones: []types.Milestone{{Day: 2, Tasks: []types.Task{{Day: 2}}}}}},
				// A later top-level milestone must not raise the minimum.
				Milestones: []types.Milestone{{Day: 30}},
			},
			want: 2,
		},
		{
			name: "empty roadmap leaves scheduling unshifted",
			rm:   types.Roadmap{},
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rm.MinScheduledDay(); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// The regression this whole change is about: a plan whose earliest day is 1
// must still put its first item on the start day, not the day after.
func TestScheduleOffset_RebasesGeneratedPlanOntoTheStartDay(t *testing.T) {
	rm := roadmapWithDays(1, 1, 4, 9)
	dayZero := rm.MinScheduledDay()

	got := make([]int, 0, 3)
	for _, task := range rm.Milestones[0].Tasks {
		got = append(got, types.ScheduleOffset(task.Day, dayZero))
	}

	want := []int{0, 3, 8}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("offsets: got %v, want %v", got, want)
		}
	}
}

func TestScheduleOffset_NeverNegative(t *testing.T) {
	if got := types.ScheduleOffset(2, 5); got != 0 {
		t.Errorf("day below day zero: got %d, want 0", got)
	}
	if got := types.ScheduleOffset(5, 5); got != 0 {
		t.Errorf("day at day zero: got %d, want 0", got)
	}
}
