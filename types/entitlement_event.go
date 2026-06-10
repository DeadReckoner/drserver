package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// EntitlementEvent mirrors a row in entitlement_events. Append-only; used
// for both idempotency (PK on EventID) and for replay/debug (raw Payload).
type EntitlementEvent struct {
	EventID          string    `gorm:"primaryKey;column:event_id" json:"event_id"`
	UserID           *string   `gorm:"column:user_id" json:"user_id,omitempty"`
	Provider         string    `gorm:"column:provider;not null" json:"provider"`
	Store            string    `gorm:"column:store" json:"store,omitempty"`
	EventType        string    `gorm:"column:event_type;not null" json:"event_type"`
	ProductID        string    `gorm:"column:product_id" json:"product_id,omitempty"`
	EventTimestampMs int64     `gorm:"column:event_timestamp_ms;not null" json:"event_timestamp_ms"`
	ReceivedAt       time.Time `gorm:"column:received_at;not null" json:"received_at"`
	Payload          JSONRaw   `gorm:"column:payload;type:jsonb;not null" json:"-"`
	ApplyStatus      string    `gorm:"column:apply_status;not null" json:"apply_status"`
	ApplyError       string    `gorm:"column:apply_error" json:"apply_error,omitempty"`
}

func (EntitlementEvent) TableName() string { return "entitlement_events" }

// JSONRaw is a thin []byte wrapper that survives Gorm/Postgres jsonb round
// trips without being parsed into a map. Lets us store raw RC payloads
// verbatim for later debugging.
type JSONRaw []byte

// Value implements driver.Valuer.
func (j JSONRaw) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSONRaw) Scan(v any) error {
	if v == nil {
		*j = nil
		return nil
	}
	switch s := v.(type) {
	case string:
		*j = []byte(s)
	case []byte:
		cpy := make([]byte, len(s))
		copy(cpy, s)
		*j = cpy
	default:
		return errors.New("JSONRaw: unsupported Scan source type")
	}
	return nil
}

// MarshalJSON emits the bytes verbatim (no double-encoding).
func (j JSONRaw) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON stores the bytes verbatim.
func (j *JSONRaw) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) {
		return errors.New("JSONRaw: invalid JSON")
	}
	cpy := make([]byte, len(data))
	copy(cpy, data)
	*j = cpy
	return nil
}
