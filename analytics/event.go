package analytics

import (
	"time"

	"dr/server/log"

	"github.com/google/uuid"
)

// SchemaVersion is stamped on every event as event_version. Bump it only for a
// breaking change to the base envelope; queries pin to it so old and new shapes
// can coexist during a migration rather than silently mixing.
const SchemaVersion = 1

// Event sources.
const (
	SourceServer    = "server" // observed by the backend; authoritative
	SourceClient    = "client" // reported by the mobile app; best-effort
	SourceScheduler = "scheduler"
)

// Event is the product-analytics envelope. Field names and JSON tags are the
// contract the Axiom dashboards are built against — renaming one silently
// breaks every query that reads it, so treat this struct as a published API.
//
// Everything user-identifying here is an opaque id. Correlating an event back
// to a human requires the primary database, which is the point: the analytics
// store holds behavior, not identity.
type Event struct {
	Time         time.Time `json:"_time"`
	EventID      string    `json:"event_id"`
	EventName    string    `json:"event_name"`
	EventVersion int       `json:"event_version"`
	Environment  string    `json:"environment"`
	Source       string    `json:"source"`

	UserID      string `json:"user_id,omitempty"`
	AnonymousID string `json:"anonymous_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`

	Platform    string `json:"platform,omitempty"`
	AppVersion  string `json:"app_version,omitempty"`
	BuildNumber string `json:"build_number,omitempty"`

	SubscriptionTier string `json:"subscription_tier,omitempty"`

	RequestID string `json:"request_id,omitempty"`
	Service   string `json:"service,omitempty"`
	Release   string `json:"release,omitempty"`

	Properties map[string]any `json:"properties,omitempty"`
}

// newEvent builds an event with the envelope filled from ctx and process
// identity, and properties filtered through the allowlist.
func newEvent(rc RequestContext, name, source string, props map[string]any) Event {
	return Event{
		Time:         time.Now().UTC(),
		EventID:      uuid.NewString(),
		EventName:    name,
		EventVersion: SchemaVersion,
		Environment:  log.Environment(),
		Source:       source,
		UserID:       rc.UserID,
		AnonymousID:  rc.AnonymousID,
		SessionID:    rc.SessionID,
		Platform:     rc.Platform,
		AppVersion:   rc.AppVersion,
		BuildNumber:  rc.BuildNumber,
		RequestID:    rc.RequestID,
		Service:      log.Service(),
		Release:      log.Release(),
		Properties:   FilterProperties(props),
	}
}

// FilterProperties drops any key not in the property allowlist and returns a
// new map. Returns nil for an empty result so the field is omitted entirely
// rather than serializing as {}.
//
// Dropping silently is intentional at this layer: a rejected property must
// never turn into a failed user request. Callers that want to know they got it
// wrong get that from the allowlist test, which runs in CI.
func FilterProperties(props map[string]any) map[string]any {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]any, len(props))
	for k, v := range props {
		if IsAllowedProperty(k) {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Props is a convenience constructor for a property bag, so call sites read as
// one expression rather than a multi-line map literal.
func Props(kv ...any) map[string]any {
	if len(kv) < 2 {
		return nil
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		m[key] = kv[i+1]
	}
	return m
}
