package database

import (
	"time"

	"dr/server/types"

	"gorm.io/gorm"
)

// TaskDayTally is how many tasks were scheduled (and how many of those are
// completed) on one calendar day, across the user's non-archived roadmaps.
// Task dates are stored as UTC calendar days.
type TaskDayTally struct {
	Day  time.Time `gorm:"column:day"`
	Due  int       `gorm:"column:due"`
	Done int       `gorm:"column:done"`
}

// GetTaskDayTallies returns per-day scheduled/completed task counts for the
// user in [from, to). Archived roadmaps are excluded — an archived roadmap's
// history shouldn't count for or against the account-page stats.
func GetTaskDayTallies(userID string, from, to time.Time) ([]TaskDayTally, error) {
	const q = `
		SELECT t.date::date AS day,
		       COUNT(*) AS due,
		       COUNT(*) FILTER (WHERE t.completed) AS done
		FROM tasks t
		JOIN roadmaps r ON r.roadmap_id = t.roadmap_id
		WHERE t.user_id = ?
		  AND r.archived = false
		  AND t.date IS NOT NULL
		  AND t.date >= ? AND t.date < ?
		GROUP BY 1
	`
	var out []TaskDayTally
	if err := db.Raw(q, userID, from, to).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetActiveHabitsWithCompletions returns the user's non-archived habits with
// completions limited to [from, to), for schedule-based stats computation.
func GetActiveHabitsWithCompletions(userID string, from, to time.Time) ([]types.Habit, error) {
	var habits []types.Habit
	if err := db.Where("user_id = ? AND archived = false", userID).
		Preload("Completions", func(db *gorm.DB) *gorm.DB {
			return db.Where("date >= ? AND date < ?", from, to)
		}).
		Find(&habits).Error; err != nil {
		return nil, err
	}
	return habits, nil
}
