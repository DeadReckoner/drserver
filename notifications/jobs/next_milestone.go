package jobs

import (
	"fmt"

	"dr/server/log"
	"dr/server/notifications"

	"gorm.io/gorm"
)

// Next-milestone reminder escalation (drserver#67).
//
// The "generate next milestone" flow is easy to miss in the app, so when a
// roadmap runs dry — every existing task completed, roadmap still active —
// the owner gets escalating reminders 24h, 72h, and 1 week after the scan
// first noticed. Milestones/tasks have no completion timestamps, so
// detected_at in next_milestone_nudges is the escalation anchor; rows are
// pruned once the roadmap has incomplete tasks again (or is archived, paused,
// or completed), so a later dry spell starts a fresh cycle.

// Reminder stage thresholds in hours since detected_at. Index = stage-1.
var nextMilestoneStageHours = [3]float64{24, 72, 168}

// exhaustedRoadmapPredicate matches active roadmaps with at least one task
// and no incomplete tasks — the "user has nothing left to do" state, whether
// the fix is generating the next milestone or generating tasks for an empty
// one. The at-least-one-task guard keeps drafts (generation still processing,
// zero tasks) out. Shared by the upsert and prune statements so they can't
// drift apart.
const exhaustedRoadmapPredicate = `
	r.archived = false
	AND r.paused_at IS NULL
	AND r.completed = false
	AND r.start_date IS NOT NULL
	AND EXISTS (SELECT 1 FROM tasks t WHERE t.roadmap_id = r.roadmap_id)
	AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.roadmap_id = r.roadmap_id AND t.completed = false)
`

// nextMilestoneDue is the row shape for reminders that may be due.
type nextMilestoneDue struct {
	RoadmapID  string  `gorm:"column:roadmap_id"`
	UserID     string  `gorm:"column:user_id"`
	Stage      int     `gorm:"column:stage"`
	Goal       string  `gorm:"column:goal"`
	AgeHours   float64 `gorm:"column:age_hours"`
	DetectedOn string  `gorm:"column:detected_on"` // YYYY-MM-DD, cycle marker for dedup keys
}

// dueStage returns the highest reminder stage the row's age qualifies for.
func (d nextMilestoneDue) dueStage() int {
	stage := 0
	for i, hours := range nextMilestoneStageHours {
		if d.AgeHours >= hours {
			stage = i + 1
		}
	}
	return stage
}

func nextMilestoneBody(stage int, goal string) string {
	switch stage {
	case 1:
		return fmt.Sprintf("Nice work finishing your milestone on %q! Take a minute to set up the next one.", goal)
	case 2:
		return fmt.Sprintf("%q has no next milestone yet — generate one to keep your momentum.", goal)
	default:
		return fmt.Sprintf("It's been a week since you finished your last milestone on %q. Set up the next one to get back on course.", goal)
	}
}

// ScanNextMilestoneNudges is the cron entry point. Returns a func() suitable
// for cron.AddFunc, wired to the production Dispatch.
func ScanNextMilestoneNudges(db *gorm.DB) func() {
	return scanNextMilestoneNudgesWith(db, notifications.Dispatch)
}

// scanNextMilestoneNudgesWith is the testable form: takes a dispatch function
// so tests can substitute a fake and assert what was dispatched.
func scanNextMilestoneNudgesWith(db *gorm.DB, dispatch func(notifications.DispatchOptions) notifications.Decision) func() {
	return func() {
		ctx := scanCtx("scan_next_milestone")

		// 1. Start the clock for newly-exhausted roadmaps. DO NOTHING keeps
		// the original detected_at and stage for rows already tracked.
		if err := db.Exec(`
			INSERT INTO next_milestone_nudges (roadmap_id, user_id, detected_at, stage)
			SELECT r.roadmap_id, r.user_id, now(), 0
			FROM roadmaps r
			WHERE ` + exhaustedRoadmapPredicate + `
			ON CONFLICT (roadmap_id) DO NOTHING
		`).Error; err != nil {
			log.Error(ctx, "upsert candidates failed", err)
			return
		}

		// 2. Stop tracking roadmaps that no longer match — the user generated
		// their next milestone (new incomplete tasks exist) or the roadmap
		// was archived/paused/completed.
		if err := db.Exec(`
			DELETE FROM next_milestone_nudges n
			WHERE NOT EXISTS (
				SELECT 1 FROM roadmaps r
				WHERE r.roadmap_id = n.roadmap_id
				AND ` + exhaustedRoadmapPredicate + `
			)
		`).Error; err != nil {
			log.Error(ctx, "prune cleared candidates failed", err)
			return
		}

		// 3. Load rows old enough for at least the first reminder and not yet
		// fully escalated. Age math runs in Postgres so the cron stays
		// stateless; the due-stage decision happens in Go where it's testable.
		var due []nextMilestoneDue
		if err := db.Raw(`
			SELECT n.roadmap_id, n.user_id, n.stage, r.goal,
			       EXTRACT(EPOCH FROM (now() - n.detected_at)) / 3600.0 AS age_hours,
			       to_char(n.detected_at, 'YYYY-MM-DD') AS detected_on
			FROM next_milestone_nudges n
			JOIN roadmaps r ON r.roadmap_id = n.roadmap_id
			WHERE n.stage < 3 AND n.detected_at <= now() - interval '24 hours'
		`).Scan(&due).Error; err != nil {
			log.Error(ctx, "load due reminders failed", err)
			return
		}

		sent := 0
		for _, d := range due {
			stage := d.dueStage()
			if stage <= d.Stage {
				continue
			}

			// If the job was down past several thresholds, the user gets one
			// reminder at the highest due stage, not a burst. detected_on in
			// the dedup key separates cycles: a roadmap that runs dry again
			// months later dedups against fresh keys.
			dispatch(notifications.DispatchOptions{
				UserID:           d.UserID,
				Category:         notifications.CategoryMyGoals,
				NotificationType: "follow_on_milestone_ready",
				DedupKey:         fmt.Sprintf("%s:next_milestone:%d:%s", d.RoadmapID, stage, d.DetectedOn),
				Title:            "Set up your next milestone",
				Body:             nextMilestoneBody(stage, d.Goal),
				Data: map[string]any{
					"roadmap_id": d.RoadmapID,
					"stage":      stage,
				},
			})
			sent++

			// Advance the cursor regardless of the dispatch decision (muted,
			// quiet hours → deferred, …): the escalation clock is about not
			// re-asking, not about delivery bookkeeping — the delivery log
			// and pending queue own that.
			if err := db.Exec(
				`UPDATE next_milestone_nudges SET stage = ? WHERE roadmap_id = ?`,
				stage, d.RoadmapID,
			).Error; err != nil {
				log.Error(ctx, "advance stage failed", err)
			}
		}

		summarize(ctx, sent > 0, fmt.Sprintf("processed %d due rows, dispatched %d", len(due), sent))
	}
}
