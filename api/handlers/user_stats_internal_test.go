package handlers

import (
	"testing"
	"time"

	"dr/server/database"
	"dr/server/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var statsToday = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func day(offset int) time.Time { return statsToday.AddDate(0, 0, offset) }

func taskTally(offset, due, done int) database.TaskDayTally {
	return database.TaskDayTally{Day: day(offset), Due: due, Done: done}
}

func dailyHabit(startOffset int, completionOffsets ...int) types.Habit {
	h := types.Habit{
		HabitID:        "habit-1",
		RecurrenceType: types.RecurrenceDaily,
		StartDate:      day(startOffset),
	}
	for _, off := range completionOffsets {
		h.Completions = append(h.Completions, types.HabitCompletion{Date: day(off)})
	}
	return h
}

func stats(taskTallies []database.TaskDayTally, habits []types.Habit) accountStatsResponse {
	tallies := buildDayTallies(taskTallies, habits, day(-statsStreakLookbackDays), statsToday)
	return computeAccountStats(tallies, statsToday)
}

func TestAccountStats_StreakCountsConsecutiveCompliantDays(t *testing.T) {
	// Tasks fully done yesterday and the day before; the day before that has
	// an incomplete task → streak 2. Today has an unfinished task → grace,
	// doesn't break or count.
	got := stats([]database.TaskDayTally{
		taskTally(0, 1, 0),  // today, unfinished → grace
		taskTally(-1, 2, 2), // compliant
		taskTally(-2, 1, 1), // compliant
		taskTally(-3, 2, 1), // broken here
	}, nil)

	assert.Equal(t, 2, got.CurrentStreakDays)
}

func TestAccountStats_TodayFullyDoneCountsTowardStreak(t *testing.T) {
	got := stats([]database.TaskDayTally{
		taskTally(0, 1, 1),
		taskTally(-1, 1, 1),
	}, nil)
	assert.Equal(t, 2, got.CurrentStreakDays)
}

func TestAccountStats_EmptyDaysAreNeutral(t *testing.T) {
	// Nothing scheduled for the last 3 days; compliant before that. The gap
	// must not break the streak.
	got := stats([]database.TaskDayTally{
		taskTally(-4, 1, 1),
		taskTally(-5, 1, 1),
	}, nil)
	assert.Equal(t, 2, got.CurrentStreakDays)
}

func TestAccountStats_HabitInstancesCountTowardStreakAndCompliance(t *testing.T) {
	// Daily habit started 3 days ago, completed the last 2 days but not the
	// first → streak 2 (today has no completion yet: due but not done →
	// grace day, doesn't count).
	h := dailyHabit(-3, -1, -2)
	got := stats(nil, []types.Habit{h})

	assert.Equal(t, 2, got.CurrentStreakDays)
	// Window covers days -30..-1; the habit was due on -3..-1, done on -2, -1.
	assert.Equal(t, 3, got.HabitsDue)
	assert.Equal(t, 2, got.HabitsCompleted)
	require.NotNil(t, got.Compliance)
	assert.InDelta(t, 2.0/3.0, *got.Compliance, 1e-9)
}

func TestAccountStats_MixedSourcesMustBothBeDoneForCompliantDay(t *testing.T) {
	// Yesterday: task done, but habit missed → day not compliant → streak 0.
	h := dailyHabit(-1) // due yesterday and today, never completed
	got := stats([]database.TaskDayTally{taskTally(-1, 1, 1)}, []types.Habit{h})

	assert.Equal(t, 0, got.CurrentStreakDays)
}

func TestAccountStats_ComplianceExcludesTodayAndOldDays(t *testing.T) {
	got := stats([]database.TaskDayTally{
		taskTally(0, 5, 0),    // today: excluded from compliance
		taskTally(-1, 2, 1),   // in window
		taskTally(-30, 2, 2),  // in window (oldest included day)
		taskTally(-31, 10, 0), // outside window
	}, nil)

	assert.Equal(t, 4, got.TasksDue)
	assert.Equal(t, 3, got.TasksCompleted)
	require.NotNil(t, got.Compliance)
	assert.InDelta(t, 0.75, *got.Compliance, 1e-9)
}

func TestAccountStats_NothingDueGivesNullCompliance(t *testing.T) {
	got := stats(nil, nil)
	assert.Nil(t, got.Compliance)
	assert.Equal(t, 0, got.CurrentStreakDays)
}

func TestAccountStats_OneShotHabitDueOnlyOnDueDate(t *testing.T) {
	due := day(-2)
	h := types.Habit{
		HabitID:        "habit-once",
		RecurrenceType: types.RecurrenceNone,
		StartDate:      day(-10),
		DueDate:        &due,
		Completions:    []types.HabitCompletion{{Date: day(-3)}}, // done early
	}
	got := stats(nil, []types.Habit{h})

	assert.Equal(t, 1, got.HabitsDue, "one-shot habit counts once, not every day until due")
	assert.Equal(t, 1, got.HabitsCompleted)
}

func TestAccountStats_WeeklyHabitOnlyCountsScheduledDays(t *testing.T) {
	// Weekly habit due only on statsToday's weekday (Sunday 2026-07-05).
	// Last week's instance completed → due twice in window+today
	// (today and -7), completed on -7.
	h := types.Habit{
		HabitID:        "habit-weekly",
		RecurrenceType: types.RecurrenceWeekly,
		DaysOfWeek:     int(types.DayOfWeekFromTime(statsToday.Weekday())),
		StartDate:      day(-8),
		Completions:    []types.HabitCompletion{{Date: day(-7)}},
	}
	got := stats(nil, []types.Habit{h})

	assert.Equal(t, 1, got.HabitsDue, "window (excluding today) contains one scheduled instance")
	assert.Equal(t, 1, got.HabitsCompleted)
	assert.Equal(t, 1, got.CurrentStreakDays, "the completed weekly instance is the streak; today is grace")
}
