package database

import (
	"time"

	"dr/server/types"

	"gorm.io/gorm/clause"
)

// =============================================================================
// digest_items
// =============================================================================
// Holds notifications whose resolver decision was DeliveryDigest, waiting for
// the user's next daily or weekly digest email. The DeliverDueDigests cron
// drains them per-user when their digest hour rolls around.

// UpsertDigestItem inserts a digest item, or no-ops on conflict. Duplicate
// scans within the same digest window resolve to the SAME row — there's no
// counter to bump because the item content is captured-at-trigger and
// shouldn't be overwritten.
func UpsertDigestItem(item types.DigestItem) error {
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "notification_type"},
			{Name: "dedup_key"},
		},
		DoNothing: true,
	}).Create(&item).Error
}

// ListDigestItemsForUser returns all queued items for a single user, oldest
// first. Used by the digest composer to build the email body.
func ListDigestItemsForUser(userID string) ([]types.DigestItem, error) {
	var out []types.DigestItem
	err := db.Where("user_id = ?", userID).
		Order("created_at ASC").
		Find(&out).Error
	return out, err
}

// DeleteDigestItemsForUser removes all queued items for one user. Called
// from the cron after a successful digest send.
func DeleteDigestItemsForUser(userID string) (int64, error) {
	res := db.Where("user_id = ?", userID).Delete(&types.DigestItem{})
	return res.RowsAffected, res.Error
}

// CleanupStaleDigestItems deletes digest items older than the cutoff. Safety
// net for items that will never drain — e.g. the user queued digest-mode
// notifications and then switched digest_frequency back to 'none', or their
// email emptied out. Returns the number of rows removed.
func CleanupStaleDigestItems(cutoff time.Time) (int64, error) {
	res := db.Where("created_at < ?", cutoff).Delete(&types.DigestItem{})
	return res.RowsAffected, res.Error
}
