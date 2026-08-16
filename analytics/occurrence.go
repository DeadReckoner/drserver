package analytics

import (
	"time"

	"github.com/google/uuid"
)

// occurrenceNamespace scopes the deterministic occurrence ids. A fixed random
// UUID, generated once and pinned here forever: changing it would renumber
// every occurrence and break continuity with all previously emitted events.
var occurrenceNamespace = uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")

// OccurrenceID returns a stable id for one scheduled instance of a recurring
// work item — "the 14 March occurrence of this habit" rather than "this
// habit".
//
// This is the field an honest completion rate depends on. A recurring habit is
// one row in the database; what the user actually succeeds or fails at is each
// day that habit came due. Counting completions against habit ids answers
// "how many habits were touched", never "what fraction of what was due got
// done" — the denominator does not exist in the schema.
//
// Deriving the id from (item, date) rather than materializing an occurrences
// table is deliberate. It needs no migration and no backfill, it is stable
// across services and replays, and the completion side becomes correct
// immediately. Emitting work_item_became_due from a scheduled scan — the
// denominator — computes the same id for the same day and joins to it, so the
// remaining work adds the missing half without revisiting this one.
//
// The date is normalized to a calendar day in the location it is expressed in,
// so an evening completion and the morning scan that marked the same day due
// agree on the id.
func OccurrenceID(itemID string, scheduled time.Time) string {
	if itemID == "" {
		return ""
	}
	day := scheduled.Format("2006-01-02")
	return uuid.NewSHA1(occurrenceNamespace, []byte(itemID+"|"+day)).String()
}
