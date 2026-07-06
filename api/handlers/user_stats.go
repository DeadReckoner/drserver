package handlers

import (
	"net/http"
	"time"

	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// Account-page stats (drmobile#151): user-level streak and rolling 30-day
// compliance across tasks and habits.
//
// Tasks carry no completion timestamps, so both stats are SCHEDULE-based: a
// day's tally is "how many items were scheduled for that day, and how many of
// those are completed now". Definitions:
//
//   - A day is COMPLIANT when everything due that day (tasks dated that day
//     on non-archived roadmaps + habit instances due that day) is completed.
//     Days with nothing due are neutral: they neither extend nor break a
//     streak.
//   - CURRENT STREAK counts consecutive compliant days back from today
//     (observed in the user's timezone). Today gets grace: an unfinished
//     today never breaks the streak — it just doesn't count yet.
//   - COMPLIANCE is completed/due over the 30 full days ending YESTERDAY
//     (today is excluded so a morning check doesn't read as a dip). Null when
//     nothing was due in the window.

// statsStreakLookbackDays bounds the streak scan.
const statsStreakLookbackDays = 365

// statsComplianceWindowDays is the rolling compliance window.
const statsComplianceWindowDays = 30

type accountStatsResponse struct {
	CurrentStreakDays int      `json:"current_streak_days"`
	Compliance        *float64 `json:"compliance_30d"` // 0..1, null when nothing was due
	WindowDays        int      `json:"window_days"`

	TasksDue        int `json:"tasks_due_30d"`
	TasksCompleted  int `json:"tasks_completed_30d"`
	HabitsDue       int `json:"habit_instances_due_30d"`
	HabitsCompleted int `json:"habit_instances_completed_30d"`
}

// dayTally aggregates what was due/done on one UTC calendar day, split by
// source so the response can report tasks and habits separately.
type dayTally struct {
	taskDue   int
	taskDone  int
	habitDue  int
	habitDone int
}

func (d dayTally) due() int  { return d.taskDue + d.habitDue }
func (d dayTally) done() int { return d.taskDone + d.habitDone }

// buildDayTallies merges task tallies and habit schedules into per-day
// due/done counts for [from, today]. Days are keyed YYYY-MM-DD (UTC calendar
// days, matching how task dates and habit completion dates are stored).
func buildDayTallies(taskTallies []database.TaskDayTally, habits []types.Habit, from, today time.Time) map[string]dayTally {
	tallies := make(map[string]dayTally)

	for _, t := range taskTallies {
		key := t.Day.UTC().Format("2006-01-02")
		d := tallies[key]
		d.taskDue += t.Due
		d.taskDone += t.Done
		tallies[key] = d
	}

	for i := range habits {
		h := &habits[i]

		completedOn := make(map[string]bool, len(h.Completions))
		for _, c := range h.Completions {
			completedOn[c.Date.UTC().Format("2006-01-02")] = true
		}

		if h.RecurrenceType == types.RecurrenceNone {
			// One-shot habit: due once, on its due date. Counting it due
			// every day until then (IsDueOn's display semantics) would tank
			// compliance. Completed if it was ever done.
			if h.DueDate == nil {
				continue
			}
			day := time.Date(h.DueDate.Year(), h.DueDate.Month(), h.DueDate.Day(), 0, 0, 0, 0, time.UTC)
			if day.Before(from) || day.After(today) {
				continue
			}
			key := day.Format("2006-01-02")
			d := tallies[key]
			d.habitDue++
			if len(h.Completions) > 0 {
				d.habitDone++
			}
			tallies[key] = d
			continue
		}

		for day := from; !day.After(today); day = day.AddDate(0, 0, 1) {
			if !h.IsDueOn(day) {
				continue
			}
			key := day.Format("2006-01-02")
			d := tallies[key]
			d.habitDue++
			if completedOn[key] {
				d.habitDone++
			}
			tallies[key] = d
		}
	}

	return tallies
}

// computeAccountStats derives streak and compliance from per-day tallies.
// Pure — today is passed in as a UTC calendar day.
func computeAccountStats(tallies map[string]dayTally, today time.Time) accountStatsResponse {
	resp := accountStatsResponse{WindowDays: statsComplianceWindowDays}

	// Streak: today counts only when fully done; an unfinished today is
	// skipped (grace), then walk back until a non-compliant day.
	if d := tallies[today.Format("2006-01-02")]; d.due() > 0 && d.done() == d.due() {
		resp.CurrentStreakDays++
	}
	for i := 1; i <= statsStreakLookbackDays; i++ {
		d := tallies[today.AddDate(0, 0, -i).Format("2006-01-02")]
		if d.due() == 0 {
			continue // nothing scheduled: neutral day
		}
		if d.done() != d.due() {
			break
		}
		resp.CurrentStreakDays++
	}

	// Compliance: the 30 full days ending yesterday.
	for i := 1; i <= statsComplianceWindowDays; i++ {
		d := tallies[today.AddDate(0, 0, -i).Format("2006-01-02")]
		resp.TasksDue += d.taskDue
		resp.TasksCompleted += d.taskDone
		resp.HabitsDue += d.habitDue
		resp.HabitsCompleted += d.habitDone
	}
	if due := resp.TasksDue + resp.HabitsDue; due > 0 {
		compliance := float64(resp.TasksCompleted+resp.HabitsCompleted) / float64(due)
		resp.Compliance = &compliance
	}

	return resp
}

// GetUserStats handles GET /users/stats (drmobile#151).
func (h *UserHandler) GetUserStats(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	// "Today" flips at the user's local midnight; days themselves are UTC
	// calendar days, matching storage.
	loc := time.UTC
	if user, err := database.GetUserByID(userID); err == nil {
		loc = types.LocationOrUTC(user.Timezone)
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -statsStreakLookbackDays)

	taskTallies, err := database.GetTaskDayTallies(userID, from, today.AddDate(0, 0, 1))
	if err != nil {
		log.Error(ctx, "GetUserStats: task tallies failed", err)
		utils.InternalErrorResponse(c, err)
		return
	}
	habits, err := database.GetActiveHabitsWithCompletions(userID, from, today.AddDate(0, 0, 1))
	if err != nil {
		log.Error(ctx, "GetUserStats: habits failed", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	tallies := buildDayTallies(taskTallies, habits, from, today)
	resp := computeAccountStats(tallies, today)

	utils.DataResponse(c, http.StatusOK, resp)
}
