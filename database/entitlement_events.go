package database

import (
	"errors"
	"time"

	"dr/server/types"

	"gorm.io/gorm"
)

// InsertEntitlementEvent records a webhook event for idempotency and audit.
// Returns a typed sentinel when the event_id already exists so the caller
// can short-circuit without bubbling raw DB errors.
//
// Pass the *gorm.DB the caller is using; webhook flow runs the entire apply
// in a single transaction so the entitlements UPDATE and this INSERT either
// both commit or both roll back.
func InsertEntitlementEvent(tx *gorm.DB, evt *types.EntitlementEvent) error {
	if tx == nil {
		tx = db
	}
	if evt.ReceivedAt.IsZero() {
		evt.ReceivedAt = time.Now().UTC()
	}
	err := tx.Create(evt).Error
	if err == nil {
		return nil
	}
	// gorm wraps the underlying driver error; rely on the error message
	// containing "duplicate key" / "23505" detection at the caller via
	// IsEntitlementEventDuplicate.
	return err
}

// EntitlementEventExists returns true if an event with this provider event_id
// has already been recorded. The caller short-circuits on true rather than
// re-applying the event.
func EntitlementEventExists(tx *gorm.DB, eventID string) (bool, error) {
	if tx == nil {
		tx = db
	}
	if eventID == "" {
		return false, errors.New("missing event_id")
	}
	var count int64
	if err := tx.Model(&types.EntitlementEvent{}).
		Where("event_id = ?", eventID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
