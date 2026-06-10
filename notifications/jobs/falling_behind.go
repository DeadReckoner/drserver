package jobs

import (
	"fmt"

	"dr/server/log"
	"dr/server/notifications"

	"gorm.io/gorm"
)

// fallingBehindStats is the per-roadmap shape returned by the stats query.
// The formula in isFallingBehind operates on these three counters; nothing
// about Postgres or timezones leaks into the threshold logic.
type fallingBehindStats struct {
	RoadmapID   string `gorm:"column:roadmap_id"`
	UserID      string `gorm:"column:user_id"`
	Goal        string `gorm:"column:goal"`
	IsoYearWeek string `gorm:"column:iso_year_week"`

	// Leg B input: count of incomplete tasks whose scheduled date falls in
	// the last 7 days (in the owner's timezone, NOT including today).
	MissedIn7d int `gorm:"column:missed_in_7d"`

	// Leg D inputs: how many tasks should have been done by today in the
	// owner's timezone, and how many actually are.
	ShouldBeDone int `gorm:"column:should_be_done"`
	ActuallyDone int `gorm:"column:actually_done"`
}

// isFallingBehind applies the B ∪ D formula:
//
//   - Leg B: ≥3 missed tasks in the rolling 7-day window
//   - Leg D absolute: deficit (should_be_done − actually_done) ≥ 5
//   - Leg D ratio:    deficit / should_be_done ≥ 20%, with a floor of
//     should_be_done ≥ 5 to prevent 1/1 = 100% triggers
//
// Returns true if ANY leg trips. Pure function — no DB, no clock — so the
// thresholds can be unit-tested exhaustively in milliseconds.
//
// Threshold constants below are duplicated in the doc comment for
// `DeadReckoner/Notification Settings LOE.md`. If you tune one, tune both.
func isFallingBehind(s fallingBehindStats) bool {
	const (
		legBThreshold      = 3
		legDAbsThreshold   = 5
		legDRatioThreshold = 0.20
		legDRatioFloor     = 5
	)

	if s.MissedIn7d >= legBThreshold {
		return true
	}

	deficit := s.ShouldBeDone - s.ActuallyDone
	if deficit >= legDAbsThreshold {
		return true
	}

	if s.ShouldBeDone >= legDRatioFloor &&
		float64(deficit)/float64(s.ShouldBeDone) >= legDRatioThreshold {
		return true
	}

	return false
}

// findFallingBehindStats returns one row per active roadmap with the three
// counters needed by isFallingBehind, EXCLUDING any roadmap that has
// already had a falling_behind push delivered this ISO week.
//
// All timezone math happens in Postgres via a `user_today` CTE so the cron
// stays stateless. The formula thresholds are applied in Go by the caller.
func findFallingBehindStats(db *gorm.DB) ([]fallingBehindStats, error) {
	const q = `
		WITH user_today AS (
		  SELECT user_id,
		         (now() AT TIME ZONE COALESCE(NULLIF(timezone, ''), 'UTC'))::date AS today
		  FROM users
		),
		roadmap_stats AS (
		  SELECT
		    r.roadmap_id,
		    r.user_id,
		    r.goal,
		    ut.today,
		    -- Leg B: incomplete tasks scheduled in the last 7 days (exclusive of today)
		    COUNT(*) FILTER (
		      WHERE t.completed = false
		        AND t.date IS NOT NULL
		        AND t.date::date >= ut.today - interval '7 days'
		        AND t.date::date <  ut.today
		    ) AS missed_in_7d,
		    -- Leg D: tasks that should be done by today
		    COUNT(*) FILTER (
		      WHERE t.date IS NOT NULL
		        AND t.date::date <= ut.today
		    ) AS should_be_done,
		    -- Leg D: tasks actually done by today
		    COUNT(*) FILTER (
		      WHERE t.completed = true
		        AND t.date IS NOT NULL
		        AND t.date::date <= ut.today
		    ) AS actually_done
		  FROM roadmaps r
		  JOIN tasks t       ON t.roadmap_id = r.roadmap_id
		  JOIN user_today ut ON ut.user_id   = r.user_id
		  WHERE r.archived = false
		    AND r.paused_at IS NULL
		  GROUP BY r.roadmap_id, r.user_id, r.goal, ut.today
		)
		SELECT
		  s.roadmap_id,
		  s.user_id,
		  s.goal,
		  to_char(now(), 'IYYY-IW') AS iso_year_week,
		  s.missed_in_7d,
		  s.should_be_done,
		  s.actually_done
		FROM roadmap_stats s
		WHERE NOT EXISTS (
		  SELECT 1 FROM notification_delivery_log dl
		  WHERE dl.user_id           = s.user_id
		    AND dl.notification_type = 'falling_behind'
		    AND dl.dedup_key         = s.roadmap_id::text || ':' || to_char(now(), 'IYYY-IW')
		    AND dl.channel           = 'push'
		)
	`
	var out []fallingBehindStats
	if err := db.Raw(q).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ScanFallingBehind is the cron entry point.
func ScanFallingBehind(db *gorm.DB) func() {
	return scanFallingBehindWith(db, notifications.Dispatch)
}

// scanFallingBehindWith is the testable form.
func scanFallingBehindWith(db *gorm.DB, dispatch func(notifications.DispatchOptions) notifications.Decision) func() {
	return func() {
		ctx := scanCtx("scan_falling_behind")

		stats, err := findFallingBehindStats(db)
		if err != nil {
			log.Info(ctx, "find stats failed", err)
			return
		}

		fired := 0
		for _, s := range stats {
			if !isFallingBehind(s) {
				continue
			}
			dispatch(notifications.DispatchOptions{
				UserID:           s.UserID,
				Category:         notifications.CategoryMyGoals,
				NotificationType: "falling_behind",
				DedupKey:         s.RoadmapID + ":" + s.IsoYearWeek,
				Title:            "Falling behind",
				Body:             fallingBehindBody(s),
				Data: map[string]any{
					"roadmap_id":     s.RoadmapID,
					"missed_in_7d":   s.MissedIn7d,
					"should_be_done": s.ShouldBeDone,
					"actually_done":  s.ActuallyDone,
				},
			})
			fired++
		}
		log.Info(ctx, fmt.Sprintf("evaluated %d roadmaps, %d firing", len(stats), fired))
	}
}

// fallingBehindBody picks a human-readable body that reflects WHICH leg
// of the formula tripped. Users will get the most relevant framing rather
// than a generic "you're behind" message.
func fallingBehindBody(s fallingBehindStats) string {
	deficit := s.ShouldBeDone - s.ActuallyDone

	// Pace deficit takes priority over the 7-day count if both trip —
	// it's the more meaningful number for big roadmaps.
	if deficit >= 5 {
		return fmt.Sprintf("You're about %d tasks behind schedule on %q", deficit, s.Goal)
	}
	if s.MissedIn7d >= 3 {
		return fmt.Sprintf("You've missed %d tasks this week on %q", s.MissedIn7d, s.Goal)
	}
	// Ratio leg only
	return fmt.Sprintf("You're falling behind on %q", s.Goal)
}
