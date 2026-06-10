package jobs

import (
	"fmt"

	"dr/server/log"
	"dr/server/notifications"

	"gorm.io/gorm"
)

// atRiskMilestoneCandidate is the per-milestone shape returned by the
// candidate query.
type atRiskMilestoneCandidate struct {
	MilestoneID string `gorm:"column:milestone_id"`
	RoadmapID   string `gorm:"column:roadmap_id"`
	UserID      string `gorm:"column:user_id"`
	SubGoal     string `gorm:"column:sub_goal"`
	// IsoYearWeek is computed in SQL (e.g. "2026-21") and reused as part of
	// the dedup key so each milestone can re-trigger once per calendar week.
	IsoYearWeek string `gorm:"column:iso_year_week"`
}

// findAtRiskMilestoneCandidates returns every milestone that:
//   - belongs to a non-archived, non-paused roadmap
//   - is not yet reached
//   - has at least one incomplete task whose date has passed in the owner's TZ
//   - has not had a milestone_at_risk push delivered THIS ISO week
//     (dedup key = milestone_id + ":" + ISO year-week)
//
// "At risk" is defined here as "has overdue work and isn't complete." The
// once-per-week cooldown via the ISO-week dedup key prevents the same
// milestone from spamming the user every 5 minutes.
func findAtRiskMilestoneCandidates(db *gorm.DB) ([]atRiskMilestoneCandidate, error) {
	const q = `
		SELECT DISTINCT
		  m.milestone_id,
		  m.roadmap_id,
		  m.user_id,
		  m.sub_goal,
		  to_char(now(), 'IYYY-IW') AS iso_year_week
		FROM milestones m
		JOIN users    u ON u.user_id    = m.user_id
		JOIN roadmaps r ON r.roadmap_id = m.roadmap_id
		JOIN tasks    t ON t.milestone_id = m.milestone_id
		WHERE m.reached = false
		  AND r.archived = false
		  AND r.paused_at IS NULL
		  AND t.completed = false
		  AND t.date IS NOT NULL
		  AND t.date::date < (now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC'))::date
		  AND NOT EXISTS (
		    SELECT 1 FROM notification_delivery_log dl
		    WHERE dl.user_id           = m.user_id
		      AND dl.notification_type = 'milestone_at_risk'
		      AND dl.dedup_key         = m.milestone_id::text || ':' || to_char(now(), 'IYYY-IW')
		      AND dl.channel           = 'push'
		  )
	`
	var out []atRiskMilestoneCandidate
	if err := db.Raw(q).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ScanAtRiskMilestones is the cron entry point.
func ScanAtRiskMilestones(db *gorm.DB) func() {
	return scanAtRiskMilestonesWith(db, notifications.Dispatch)
}

// scanAtRiskMilestonesWith is the testable form.
func scanAtRiskMilestonesWith(db *gorm.DB, dispatch func(notifications.DispatchOptions) notifications.Decision) func() {
	return func() {
		ctx := scanCtx("scan_at_risk_milestones")

		candidates, err := findAtRiskMilestoneCandidates(db)
		if err != nil {
			log.Error(ctx, "find candidates failed", err)
			return
		}
		for _, c := range candidates {
			dispatch(notifications.DispatchOptions{
				UserID:           c.UserID,
				Category:         notifications.CategoryMyGoals,
				NotificationType: "milestone_at_risk",
				DedupKey:         c.MilestoneID + ":" + c.IsoYearWeek,
				Title:            "Milestone at risk",
				Body:             c.SubGoal + " is falling behind schedule",
				Data: map[string]any{
					"milestone_id": c.MilestoneID,
					"roadmap_id":   c.RoadmapID,
				},
			})
		}
		summarize(ctx, len(candidates) > 0, fmt.Sprintf("processed %d candidates", len(candidates)))
	}
}
