package jobs

import (
	"fmt"

	"dr/server/log"
	"dr/server/notifications"

	"gorm.io/gorm"
)

// missedTaskCandidate is the per-task shape returned by the candidate query.
// Includes the recipient's timezone so the dispatcher doesn't need a follow-up
// user lookup.
type missedTaskCandidate struct {
	TaskID    string `gorm:"column:task_id"`
	RoadmapID string `gorm:"column:roadmap_id"`
	UserID    string `gorm:"column:user_id"`
	Title     string `gorm:"column:title"`
}

// findMissedTaskCandidates returns every task that:
//   - belongs to a non-archived, non-paused roadmap
//   - is incomplete and has a scheduled date
//   - whose date has passed in the owner's timezone
//   - has NOT already had a missed_task push delivered (dedup key = task_id)
//
// Timezone math runs in Postgres so the cron stays stateless.
func findMissedTaskCandidates(db *gorm.DB) ([]missedTaskCandidate, error) {
	const q = `
		SELECT t.task_id, t.roadmap_id, t.user_id, t.title
		FROM tasks t
		JOIN users    u ON u.user_id    = t.user_id
		JOIN roadmaps r ON r.roadmap_id = t.roadmap_id
		WHERE t.completed = false
		  AND t.date IS NOT NULL
		  AND r.archived = false
		  AND r.paused_at IS NULL
		  AND t.date::date < (now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC'))::date
		  AND NOT EXISTS (
		    SELECT 1 FROM notification_delivery_log dl
		    WHERE dl.user_id           = t.user_id
		      AND dl.notification_type = 'missed_task'
		      AND dl.dedup_key         = t.task_id::text
		      AND dl.channel           = 'push'
		  )
	`
	var out []missedTaskCandidate
	if err := db.Raw(q).Scan(&out).Error; err != nil {
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
			log.Info(ctx, "find candidates failed", err)
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
					"task_id":    c.TaskID,
					"roadmap_id": c.RoadmapID,
				},
			})
		}
		log.Info(ctx, fmt.Sprintf("processed %d candidates", len(candidates)))
	}
}
