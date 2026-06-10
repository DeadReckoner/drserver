package jobs

import (
	"context"
	"fmt"

	"dr/server/email"
	"dr/server/log"

	"gorm.io/gorm"
)

// anchorCandidate is one user who is acting as an anchor and is currently
// at 9am in their local timezone.
type anchorCandidate struct {
	AnchorUserID    string `gorm:"column:user_id"`
	AnchorFirstName string `gorm:"column:first_name"`
}

// anchoredSummaryRow is one person the anchor is anchoring, with the
// raw counters needed to compose their one-line summary. Same TZ-aware
// "missed in last 7 days" predicate as the falling-behind detector so
// the numbers line up.
type anchoredSummaryRow struct {
	AnchorUserID  string `gorm:"column:anchor_user_id"`
	AnchoredName  string `gorm:"column:anchored_name"`
	MissedIn7d    int    `gorm:"column:missed_in_7d"`
	OverdueNow    int    `gorm:"column:overdue_now"`
	CompletedIn7d int    `gorm:"column:completed_in_7d"`
}

// findAnchorBatchCandidates returns users who are at 9am local AND are the
// accepted anchor of at least one non-archived/non-paused roadmap AND
// haven't received an anchor summary in the last 23 hours.
func findAnchorBatchCandidates(db *gorm.DB) ([]anchorCandidate, error) {
	const q = `
		WITH user_local AS (
		  SELECT
		    u.user_id,
		    u.first_name,
		    u.email,
		    now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC') AS local_now
		  FROM users u
		  WHERE u.email != ''
		)
		SELECT user_id, first_name
		FROM user_local
		WHERE EXTRACT(HOUR FROM local_now)::int = 9
		  AND EXISTS (
		    SELECT 1 FROM roadmaps r
		    WHERE r.anchor_id = user_local.user_id
		      AND r.archived = false
		      AND r.paused_at IS NULL
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM email_log el
		    WHERE el.user_id = user_local.user_id
		      AND el.kind = 'anchor_daily_summary'
		      AND el.status = 'sent'
		      AND el.created_at > now() - interval '23 hours'
		  )
	`
	var out []anchorCandidate
	if err := db.Raw(q).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// findAnchoredSummariesFor returns one row per (anchor, anchored-user) pair
// with progress counters. Counts are computed in the OWNER's timezone (the
// person whose progress is being summarized), since that's what they'd see
// in their own digest.
func findAnchoredSummariesFor(db *gorm.DB, anchorUserIDs []string) ([]anchoredSummaryRow, error) {
	if len(anchorUserIDs) == 0 {
		return nil, nil
	}
	const q = `
		WITH owner_today AS (
		  SELECT u.user_id,
		         (now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC'))::date AS today
		  FROM users u
		)
		SELECT
		  r.anchor_id AS anchor_user_id,
		  COALESCE(NULLIF(TRIM(CONCAT(u.first_name, ' ', u.last_name)), ''), u.email) AS anchored_name,
		  COUNT(DISTINCT t.task_id) FILTER (
		    WHERE t.completed = false
		      AND t.date IS NOT NULL
		      AND t.date::date >= ot.today - interval '7 days'
		      AND t.date::date <  ot.today
		  ) AS missed_in_7d,
		  COUNT(DISTINCT t.task_id) FILTER (
		    WHERE t.completed = false
		      AND t.date IS NOT NULL
		      AND t.date::date < ot.today
		  ) AS overdue_now,
		  COUNT(DISTINCT t.task_id) FILTER (
		    WHERE t.completed = true
		      AND t.date IS NOT NULL
		      AND t.date::date >= ot.today - interval '7 days'
		  ) AS completed_in_7d
		FROM roadmaps r
		JOIN users u        ON u.user_id    = r.user_id
		JOIN owner_today ot ON ot.user_id   = r.user_id
		LEFT JOIN tasks t   ON t.roadmap_id = r.roadmap_id
		WHERE r.anchor_id = ANY(?)
		  AND r.archived = false
		  AND r.paused_at IS NULL
		GROUP BY r.anchor_id, u.user_id, u.first_name, u.last_name, u.email
	`
	var out []anchoredSummaryRow
	if err := db.Raw(q, anchorUserIDs).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// summaryLine renders the human-readable status text for one anchored user.
// Empty return means "nothing to say" — used to suppress noise rows when
// the person has been quiet.
func summaryLine(r anchoredSummaryRow) string {
	switch {
	case r.MissedIn7d >= 3:
		return fmt.Sprintf("Has missed %d tasks this week. Worth a check-in.", r.MissedIn7d)
	case r.OverdueNow >= 1:
		return fmt.Sprintf("Has %d overdue task(s) right now.", r.OverdueNow)
	case r.CompletedIn7d > 0:
		return fmt.Sprintf("Completed %d task(s) this week. Keep cheering them on.", r.CompletedIn7d)
	default:
		return ""
	}
}

// DeliverAnchorBatches is the cron entry point.
func DeliverAnchorBatches(db *gorm.DB) func() {
	return deliverAnchorBatchesWith(
		func() ([]anchorCandidate, error) { return findAnchorBatchCandidates(db) },
		func(ids []string) ([]anchoredSummaryRow, error) { return findAnchoredSummariesFor(db, ids) },
		email.NewDispatcher(db),
	)
}

func deliverAnchorBatchesWith(
	listCandidates func() ([]anchorCandidate, error),
	listSummaries func(anchorIDs []string) ([]anchoredSummaryRow, error),
	sender digestSender,
) func() {
	return func() {
		ctx := scanCtx("deliver_anchor_batches")

		candidates, err := listCandidates()
		if err != nil {
			log.Error(ctx, "find candidates failed", err)
			return
		}
		if len(candidates) == 0 {
			log.Debug(ctx, "no candidates")
			return
		}

		anchorIDs := make([]string, 0, len(candidates))
		for _, c := range candidates {
			anchorIDs = append(anchorIDs, c.AnchorUserID)
		}
		summaries, err := listSummaries(anchorIDs)
		if err != nil {
			log.Error(ctx, "find summaries failed", err)
			return
		}

		// Group summaries by anchor.
		byAnchor := map[string][]email.AnchoredUserVar{}
		for _, s := range summaries {
			line := summaryLine(s)
			if line == "" {
				continue // skip the no-op rows
			}
			byAnchor[s.AnchorUserID] = append(byAnchor[s.AnchorUserID], email.AnchoredUserVar{
				Name:    s.AnchoredName,
				Summary: line,
			})
		}

		var sent, skipped int
		for _, c := range candidates {
			rows := byAnchor[c.AnchorUserID]
			// Silent skip when nothing noteworthy is happening per the
			// product decision.
			if len(rows) == 0 {
				skipped++
				continue
			}

			vars := email.Vars{
				FirstName:     c.AnchorFirstName,
				AnchoredUsers: rows,
			}
			dedupeKey := "anchor-batch" // 23h email_log dedupe is the real guard
			if err := sender.Send(context.Background(), c.AnchorUserID, email.KindAnchorDailySummary, vars, dedupeKey); err != nil {
				log.Error(ctx, "send anchor summary failed", err, "anchor_user_id", c.AnchorUserID)
				continue
			}
			sent++
		}

		log.Info(ctx, fmt.Sprintf("processed %d anchor candidates: %d sent, %d skipped (no content)", len(candidates), sent, skipped))
	}
}
