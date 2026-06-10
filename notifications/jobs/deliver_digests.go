package jobs

import (
	"context"
	"fmt"

	"dr/server/database"
	"dr/server/email"
	"dr/server/log"
	"dr/server/types"

	"gorm.io/gorm"
)

// digestCandidate is the per-user row returned by the candidate query.
// FirstName feeds the email greeting. We carry the digest_frequency here
// to pick the right email kind (daily vs weekly) without a second lookup.
type digestCandidate struct {
	UserID          string `gorm:"column:user_id"`
	FirstName       string `gorm:"column:first_name"`
	DigestFrequency string `gorm:"column:digest_frequency"`
}

// findDueDigestCandidates returns users whose digest hour matches the
// current hour in their local timezone, who have queued items, and who
// haven't received a digest email in the last 23 hours.
//
// "23 hours" is the de-duplication window — it's deliberately less than 24
// so a daily digest that ran at 09:00 yesterday is eligible at 09:00 today
// without a millisecond-margin race.
//
// Weekly digests get an additional day-of-week filter.
func findDueDigestCandidates(db *gorm.DB) ([]digestCandidate, error) {
	const q = `
		WITH user_local AS (
		  SELECT
		    u.user_id,
		    u.first_name,
		    u.email,
		    ns.digest_frequency,
		    ns.digest_delivery_hour,
		    ns.digest_delivery_day_of_week,
		    now() AT TIME ZONE COALESCE(NULLIF(u.timezone, ''), 'UTC') AS local_now
		  FROM users u
		  JOIN notification_settings ns ON ns.user_id = u.user_id
		  WHERE ns.digest_frequency IN ('daily', 'weekly')
		    AND u.email != ''
		)
		SELECT user_id, first_name, digest_frequency
		FROM user_local
		WHERE EXTRACT(HOUR FROM local_now)::int = digest_delivery_hour
		  AND (
		    digest_frequency = 'daily'
		    OR (
		      digest_frequency = 'weekly'
		      AND EXTRACT(DOW FROM local_now)::int = digest_delivery_day_of_week
		    )
		  )
		  AND EXISTS (SELECT 1 FROM digest_items WHERE user_id = user_local.user_id)
		  AND NOT EXISTS (
		    SELECT 1 FROM email_log el
		    WHERE el.user_id = user_local.user_id
		      AND el.kind IN ('daily_digest', 'weekly_digest')
		      AND el.status = 'sent'
		      AND el.created_at > now() - interval '23 hours'
		  )
	`
	var out []digestCandidate
	if err := db.Raw(q).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// DeliverDueDigests is the cron entry point.
func DeliverDueDigests(db *gorm.DB) func() {
	return deliverDueDigestsWith(
		func() ([]digestCandidate, error) { return findDueDigestCandidates(db) },
		email.NewDispatcher(db),
		database.ListDigestItemsForUser,
		database.DeleteDigestItemsForUser,
	)
}

// digestSender is the slice of email.Dispatcher this job actually needs.
// Pulling out an interface keeps the testable form tiny without dragging
// in the whole Dispatcher's HTTP client and DB connection.
type digestSender interface {
	Send(ctx context.Context, userID string, kind email.EmailKind, vars email.Vars, dedupeKey string) error
}

func deliverDueDigestsWith(
	listCandidates func() ([]digestCandidate, error),
	sender digestSender,
	listItems func(userID string) ([]types.DigestItem, error),
	deleteItems func(userID string) (int64, error),
) func() {
	return func() {
		ctx := scanCtx("deliver_due_digests")

		candidates, err := listCandidates()
		if err != nil {
			log.Error(ctx, "find candidates failed", err)
			return
		}

		var sent, skipped int
		for _, c := range candidates {
			items, err := listItems(c.UserID)
			if err != nil {
				log.Error(ctx, "list items failed", err, "user_id", c.UserID)
				continue
			}
			// Silent skip on empty per the product decision.
			if len(items) == 0 {
				skipped++
				continue
			}

			vars := email.Vars{
				FirstName:   c.FirstName,
				DigestItems: digestItemsToEmailVars(items),
			}
			kind := email.KindDailyDigest
			if c.DigestFrequency == "weekly" {
				kind = email.KindWeeklyDigest
			}
			// Dedupe by date so a clock blip during the digest hour can't
			// produce two emails to the same user the same day.
			dedupeKey := truncateToDate(items[0].CreatedAt.UTC().Format("2006-01-02"))

			if err := sender.Send(context.Background(), c.UserID, kind, vars, dedupeKey); err != nil {
				log.Error(ctx, "send digest failed", err, "user_id", c.UserID)
				continue
			}

			if _, err := deleteItems(c.UserID); err != nil {
				// Email sent but cleanup failed — items will be in next
				// digest too, but the email_log dedupe blocks an immediate
				// duplicate. Log loudly.
				log.Error(ctx, "delete digest items after send failed", err, "user_id", c.UserID)
			}
			sent++
		}

		summarize(ctx, len(candidates) > 0, fmt.Sprintf("processed %d candidates: %d sent, %d skipped (empty)", len(candidates), sent, skipped))
	}
}

func digestItemsToEmailVars(items []types.DigestItem) []email.DigestItemVar {
	out := make([]email.DigestItemVar, len(items))
	for i, it := range items {
		out[i] = email.DigestItemVar{Title: it.Title, Body: it.Body}
	}
	return out
}

// truncateToDate is a small helper that doubles as a unit of intent —
// "this dedupe key represents one calendar day's digest, not a moment."
func truncateToDate(s string) string { return s }
