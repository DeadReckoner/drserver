package jobs

import (
	"fmt"

	"dr/server/log"
	"dr/server/notifications"

	"gorm.io/gorm"
)

// missedTaskCandidate is the per-task shape returned by the candidate query.
// Includes MilestoneID so the frontend can route the user to the milestone
// view from a notification tap without an extra lookup.
type missedTaskCandidate struct {
	TaskID      string `gorm:"column:task_id"`
	RoadmapID   string `gorm:"column:roadmap_id"`
	MilestoneID string `gorm:"column:milestone_id"`
	UserID      string `gorm:"column:user_id"`
	Title       string `gorm:"column:title"`
}

// missedTaskWindowDays bounds how far back a task's date can be and still
// trigger a missed-task push. Two reasons it exists:
//
//  1. Retention safety: missed_task dedup is once-ever (dedup key = task_id),
//     and the cleanup cron drops delivery-log rows after deliveryLogRetention
//     (90 days). Without a candidate bound, a still-incomplete old task would
//     re-notify the moment its log row aged out. The window must stay well
//     under the retention period — a tripwire test in cleanup_test.go guards
//     the relationship.
//  2. Product: a task missed a month ago isn't news; pushing about it would
//     read as spam (e.g. un-archiving an old roadmap shouldn't unleash its
//     entire backlog).
const missedTaskWindowDays = 30

// findMissedTaskCandidates returns every task that:
//   - belongs to a non-archived, non-paused roadmap
//   - is incomplete and has a scheduled date
//   - whose date has passed in the owner's timezone, but is within the last
//     missedTaskWindowDays days
//   - has NOT already had a missed_task push delivered (dedup key = task_id)
//
// Timezone math runs in Postgres so the cron stays stateless.
func findMissedTaskCandidates(db *gorm.DB) ([]missedTaskCandidate, error) {
	const q = `
		SELECT t.task_id, t.roadmap_id, t.milestone_id, t.user_id, t.title
		FROM tasks t
		JOIN users    u ON u.user_id    = t.user_id
		JOIN roadmaps r ON r.roadmap_id = t.roadmap_id
		WHERE t.completed = false
		  AND t.date IS NOT NULL
		  AND r.archived = false
		  AND r.paused_at IS NULL
		  AND t.date::date < (now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC'))::date
		  AND t.date::date >= (now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC'))::date - (? * interval '1 day')
		  AND NOT EXISTS (
		    SELECT 1 FROM notification_delivery_log dl
		    WHERE dl.user_id           = t.user_id
		      AND dl.notification_type = 'missed_task'
		      AND dl.dedup_key         = t.task_id::text
		      AND dl.channel           = 'push'
		  )
	`
	var out []missedTaskCandidate
	if err := db.Raw(q, missedTaskWindowDays).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ScanMissedTasks is the cron entry point. Returns a func() suitable for
// cron.AddFunc. Wires through to scanMissedTasksWith using the production
// Dispatch so callers don't have to think about injection.
func ScanMissedTasks(db *gorm.DB) func() {
	return scanMissedTasksWith(db, notifications.Dispatch)
}

// scanMissedTasksWith is the testable form: takes a dispatch function so
// tests can substitute a fake and assert what was dispatched.
func scanMissedTasksWith(db *gorm.DB, dispatch func(notifications.DispatchOptions) notifications.Decision) func() {
	return func() {
		ctx := scanCtx("scan_missed_tasks")

		candidates, err := findMissedTaskCandidates(db)
		if err != nil {
			log.Error(ctx, "find candidates failed", err)
			return
		}
		for _, c := range candidates {
			dispatch(notifications.DispatchOptions{
				UserID:           c.UserID,
				Category:         notifications.CategoryMyGoals,
				NotificationType: "missed_task",
				DedupKey:         c.TaskID,
				Title:            "Missed task",
				Body:             c.Title,
				Data: map[string]any{
					"task_id":      c.TaskID,
					"milestone_id": c.MilestoneID,
					"roadmap_id":   c.RoadmapID,
				},
			})
		}
		summarize(ctx, len(candidates) > 0, fmt.Sprintf("processed %d candidates", len(candidates)))
	}
}
