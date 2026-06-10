package types_test

import (
	"testing"
	"time"

	"dr/server/types"
)

// A clean reference week in May 2026: Mon 18th through Sun 24th.
var (
	mon = time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	tue = time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	wed = time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	thu = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	fri = time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	sat = time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	sun = time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
)

func datePtr(t time.Time) *time.Time { return &t }
func intPtr(i int) *int              { return &i }

// --- DayOfWeek bitmask -------------------------------------------------------

func TestDayOfWeek_Contains(t *testing.T) {
	cases := []struct {
		name string
		mask types.DayOfWeek
		day  time.Weekday
		want bool
	}{
		{"sunday bit matches sunday", types.Sunday, time.Sunday, true},
		{"sunday bit does not match monday", types.Sunday, time.Monday, false},
		{"saturday bit matches saturday", types.Saturday, time.Saturday, true},
		{"weekdays contains wednesday", types.Weekdays, time.Wednesday, true},
		{"weekdays excludes saturday", types.Weekdays, time.Saturday, false},
		{"weekdays excludes sunday", types.Weekdays, time.Sunday, false},
		{"weekends contains sunday", types.Weekends, time.Sunday, true},
		{"weekends excludes friday", types.Weekends, time.Friday, false},
		{"everyday contains all", types.EveryDay, time.Thursday, true},
		{"zero mask contains nothing", types.DayOfWeek(0), time.Monday, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mask.Contains(tc.day); got != tc.want {
				t.Errorf("(%d).Contains(%v) = %v, want %v", tc.mask, tc.day, got, tc.want)
			}
		})
	}
}

func TestDayOfWeek_Days_RoundTrip(t *testing.T) {
	mask := types.Monday | types.Wednesday | types.Friday
	days := mask.Days()

	want := []time.Weekday{time.Monday, time.Wednesday, time.Friday}
	if len(days) != len(want) {
		t.Fatalf("Days() = %v, want %v", days, want)
	}
	for i := range want {
		if days[i] != want[i] {
			t.Errorf("Days()[%d] = %v, want %v", i, days[i], want[i])
		}
	}

	// Round-trip: rebuilding the mask from Days() must yield the original.
	var rebuilt types.DayOfWeek
	for _, d := range days {
		rebuilt |= types.DayOfWeekFromTime(d)
	}
	if rebuilt != mask {
		t.Errorf("round-trip mask = %d, want %d", rebuilt, mask)
	}
}

// --- IsDueOn: active window (shared by all recurrence types) -----------------

func TestHabit_IsDueOn_BeforeStartDate_NeverDue(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceDaily,
		StartDate:      wed,
	}
	if h.IsDueOn(tue) {
		t.Error("daily habit due before its start date")
	}
	if !h.IsDueOn(wed) {
		t.Error("daily habit not due on its start date (start date is inclusive)")
	}
}

func TestHabit_IsDueOn_AfterEndDate_NeverDue(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceDaily,
		StartDate:      mon,
		EndDate:        datePtr(fri),
	}
	if !h.IsDueOn(fri) {
		t.Error("daily habit not due on its end date (end date is inclusive)")
	}
	if h.IsDueOn(sat) {
		t.Error("daily habit due after its end date")
	}
}

func TestHabit_IsDueOn_TimeOfDayIgnored(t *testing.T) {
	// Start date stored with an afternoon timestamp; query late in the
	// evening of the same calendar day. Both should normalize to date-only.
	h := types.Habit{
		RecurrenceType: types.RecurrenceDaily,
		StartDate:      time.Date(2026, 5, 18, 15, 30, 0, 0, time.UTC),
	}
	queryMorning := time.Date(2026, 5, 18, 0, 1, 0, 0, time.UTC)
	if !h.IsDueOn(queryMorning) {
		t.Error("habit not due in the morning of its start date (time-of-day should be ignored)")
	}
}

// --- IsDueOn: one-time todos (RecurrenceNone) --------------------------------

func TestHabit_IsDueOn_None_DueThroughDueDate(t *testing.T) {
	// A one-time todo is due every day from start through its due date
	// (inclusive), then stops. This is the "carry forward until done or
	// overdue" semantic.
	h := types.Habit{
		RecurrenceType: types.RecurrenceNone,
		StartDate:      mon,
		DueDate:        datePtr(wed),
	}
	for _, d := range []time.Time{mon, tue, wed} {
		if !h.IsDueOn(d) {
			t.Errorf("todo not due on %s (should be due start through due date)", d.Format("Mon"))
		}
	}
	if h.IsDueOn(thu) {
		t.Error("todo still due the day after its due date")
	}
}

func TestHabit_IsDueOn_None_NilDueDate_NeverDue(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceNone,
		StartDate:      mon,
	}
	if h.IsDueOn(tue) {
		t.Error("todo with no due date reported as due")
	}
}

// --- IsDueOn: daily -----------------------------------------------------------

func TestHabit_IsDueOn_Daily_EveryDayInWindow(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceDaily,
		StartDate:      mon,
	}
	for _, d := range []time.Time{mon, tue, wed, thu, fri, sat, sun} {
		if !h.IsDueOn(d) {
			t.Errorf("daily habit not due on %s", d.Format("Mon"))
		}
	}
}

// --- IsDueOn: weekly ----------------------------------------------------------

func TestHabit_IsDueOn_Weekly_OnlyOnMaskedDays(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceWeekly,
		DaysOfWeek:     int(types.Monday | types.Wednesday | types.Friday),
		StartDate:      mon,
	}
	dueDays := map[time.Weekday]bool{time.Monday: true, time.Wednesday: true, time.Friday: true}
	for _, d := range []time.Time{mon, tue, wed, thu, fri, sat, sun} {
		want := dueDays[d.Weekday()]
		if got := h.IsDueOn(d); got != want {
			t.Errorf("MWF habit IsDueOn(%s) = %v, want %v", d.Format("Mon"), got, want)
		}
	}
}

func TestHabit_IsDueOn_Weekly_WeekendMask(t *testing.T) {
	// Sunday occupies bit 0 — the easiest bit to get wrong when mapping
	// time.Weekday (Sunday=0) onto the bitmask.
	h := types.Habit{
		RecurrenceType: types.RecurrenceWeekly,
		DaysOfWeek:     int(types.Weekends),
		StartDate:      mon,
	}
	if h.IsDueOn(fri) {
		t.Error("weekend habit due on Friday")
	}
	if !h.IsDueOn(sat) {
		t.Error("weekend habit not due on Saturday")
	}
	if !h.IsDueOn(sun) {
		t.Error("weekend habit not due on Sunday")
	}
}

func TestHabit_IsDueOn_Weekly_ZeroMask_NeverDue(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceWeekly,
		DaysOfWeek:     0,
		StartDate:      mon,
	}
	for _, d := range []time.Time{mon, tue, wed, thu, fri, sat, sun} {
		if h.IsDueOn(d) {
			t.Errorf("zero-mask weekly habit due on %s", d.Format("Mon"))
		}
	}
}

// --- IsDueOn: monthly ---------------------------------------------------------

func TestHabit_IsDueOn_Monthly_MatchesDayOfMonth(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceMonthly,
		DayOfMonth:     intPtr(15),
		StartDate:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if !h.IsDueOn(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)) {
		t.Error("monthly habit not due on its day of month")
	}
	if h.IsDueOn(time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)) {
		t.Error("monthly habit due on the wrong day")
	}
	// Due again the following month.
	if !h.IsDueOn(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Error("monthly habit not due the following month")
	}
}

func TestHabit_IsDueOn_Monthly_Day31_SkipsShortMonths(t *testing.T) {
	// A habit on the 31st simply never comes due in 30-day months — it does
	// NOT roll to the 30th. Pinning this so a future "clamp to month end"
	// change is a conscious decision.
	h := types.Habit{
		RecurrenceType: types.RecurrenceMonthly,
		DayOfMonth:     intPtr(31),
		StartDate:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if !h.IsDueOn(time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)) {
		t.Error("day-31 habit not due on May 31")
	}
	// June has 30 days: not due on the 30th, and July 1 is day 1.
	if h.IsDueOn(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Error("day-31 habit due on June 30 (should skip short months, not clamp)")
	}
}

func TestHabit_IsDueOn_Monthly_NilDayOfMonth_NeverDue(t *testing.T) {
	h := types.Habit{
		RecurrenceType: types.RecurrenceMonthly,
		StartDate:      mon,
	}
	if h.IsDueOn(wed) {
		t.Error("monthly habit with nil day_of_month reported as due")
	}
}

// --- IsActive -----------------------------------------------------------------

func TestHabit_IsActive(t *testing.T) {
	cases := []struct {
		name  string
		habit types.Habit
		date  time.Time
		want  bool
	}{
		{
			name:  "active within window",
			habit: types.Habit{StartDate: mon, EndDate: datePtr(fri)},
			date:  wed,
			want:  true,
		},
		{
			name:  "archived is never active",
			habit: types.Habit{StartDate: mon, Archived: true},
			date:  wed,
			want:  false,
		},
		{
			name:  "before start date",
			habit: types.Habit{StartDate: wed},
			date:  mon,
			want:  false,
		},
		{
			name:  "after end date",
			habit: types.Habit{StartDate: mon, EndDate: datePtr(wed)},
			date:  fri,
			want:  false,
		},
		{
			name:  "no end date stays active",
			habit: types.Habit{StartDate: mon},
			date:  time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.habit.IsActive(tc.date); got != tc.want {
				t.Errorf("IsActive = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHabit_IsActive_ExactTimestampComparison(t *testing.T) {
	// Unlike IsDueOn, IsActive compares raw timestamps without normalizing
	// to date-only. A habit whose StartDate carries a noon timestamp is NOT
	// active that morning. Pinned deliberately: if this ever changes to
	// date-only comparison (probably the right fix for consistency with
	// IsDueOn), this test should be updated consciously rather than the
	// behavior drifting.
	h := types.Habit{StartDate: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}

	if h.IsActive(time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)) {
		t.Error("IsActive normalized start date to midnight — intentional change? Update this test and check IsDueOn consistency")
	}
	if !h.IsActive(time.Date(2026, 5, 18, 13, 0, 0, 0, time.UTC)) {
		t.Error("habit not active after its start timestamp")
	}
}

// --- CreateHabitRequest.Validate ----------------------------------------------

func TestCreateHabitRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     types.CreateHabitRequest
		wantErr error
	}{
		{
			name:    "daily needs nothing extra",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceDaily},
			wantErr: nil,
		},
		{
			name:    "invalid recurrence type",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: "yearly"},
			wantErr: types.ErrInvalidRecurrenceType,
		},
		{
			name:    "none requires due date",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceNone},
			wantErr: types.ErrDueDateRequired,
		},
		{
			name:    "none with due date ok",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceNone, DueDate: datePtr(fri)},
			wantErr: nil,
		},
		{
			name:    "weekly requires day mask",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceWeekly, DaysOfWeek: 0},
			wantErr: types.ErrInvalidDaysOfWeek,
		},
		{
			name:    "weekly mask above 127 invalid",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceWeekly, DaysOfWeek: 128},
			wantErr: types.ErrInvalidDaysOfWeek,
		},
		{
			name:    "weekly with valid mask ok",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceWeekly, DaysOfWeek: int(types.Weekdays)},
			wantErr: nil,
		},
		{
			name:    "monthly requires day of month",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceMonthly},
			wantErr: types.ErrInvalidDayOfMonth,
		},
		{
			name:    "monthly day 0 invalid",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceMonthly, DayOfMonth: intPtr(0)},
			wantErr: types.ErrInvalidDayOfMonth,
		},
		{
			name:    "monthly day 32 invalid",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceMonthly, DayOfMonth: intPtr(32)},
			wantErr: types.ErrInvalidDayOfMonth,
		},
		{
			name:    "monthly day 1 ok",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceMonthly, DayOfMonth: intPtr(1)},
			wantErr: nil,
		},
		{
			name:    "monthly day 31 ok",
			req:     types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceMonthly, DayOfMonth: intPtr(31)},
			wantErr: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if err != tc.wantErr {
				t.Errorf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// --- Milestone.IsComplete (small gap from Phase 1) -----------------------------

func TestMilestone_IsComplete(t *testing.T) {
	cases := []struct {
		name string
		m    types.Milestone
		want bool
	}{
		{"empty milestone is not complete", types.Milestone{}, false},
		{"reached overrides everything", types.Milestone{Reached: true}, true},
		{
			"all tasks completed",
			types.Milestone{Tasks: []types.Task{{Completed: true}, {Completed: true}}},
			true,
		},
		{
			"one incomplete task",
			types.Milestone{Tasks: []types.Task{{Completed: true}, {Completed: false}}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.IsComplete(); got != tc.want {
				t.Errorf("IsComplete() = %v, want %v", got, tc.want)
			}
		})
	}
}
