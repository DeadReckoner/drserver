package analytics

import (
	"context"
	"sync"
	"testing"
	"time"
)

// captureSink records events in memory for assertions.
type captureSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *captureSink) Send(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *captureSink) Close(context.Context) error { return nil }

func (s *captureSink) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func withCapture(t *testing.T) *captureSink {
	t.Helper()
	sink := &captureSink{}
	restore := SetSink(sink)
	t.Cleanup(restore)
	return sink
}

func TestEmit_PopulatesEnvelopeFromContext(t *testing.T) {
	sink := withCapture(t)

	ctx := WithRequestContext(context.Background(), RequestContext{
		UserID:      "user-1",
		RequestID:   "req-1",
		Platform:    "ios",
		AppVersion:  "1.2.3",
		BuildNumber: "42",
		SessionID:   "sess-1",
	})

	Emit(ctx, EventGoalCreated, Props("roadmap_id", "rm-1"))

	events := sink.all()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]

	if e.UserID != "user-1" || e.RequestID != "req-1" {
		t.Errorf("identity not carried: user=%q request=%q", e.UserID, e.RequestID)
	}
	if e.Platform != "ios" || e.AppVersion != "1.2.3" || e.BuildNumber != "42" {
		t.Errorf("client metadata not carried: %+v", e)
	}
	if e.EventName != EventGoalCreated || e.Source != SourceServer {
		t.Errorf("name/source wrong: %q %q", e.EventName, e.Source)
	}
	if e.EventID == "" {
		t.Error("event_id must always be populated; dedupe depends on it")
	}
	if e.EventVersion != SchemaVersion {
		t.Errorf("event_version = %d, want %d", e.EventVersion, SchemaVersion)
	}
	if e.Properties["roadmap_id"] != "rm-1" {
		t.Errorf("properties not carried: %+v", e.Properties)
	}
}

func TestEmit_DropsUnknownEventName(t *testing.T) {
	sink := withCapture(t)

	// A typo'd name must produce nothing rather than a plausible-looking
	// metric that quietly holds a fraction of the real data.
	Emit(context.Background(), "goals_created", nil)

	if got := len(sink.all()); got != 0 {
		t.Fatalf("emitted %d events for an unknown name, want 0", got)
	}
}

func TestEmit_DropsDisallowedProperties(t *testing.T) {
	sink := withCapture(t)

	Emit(context.Background(), EventGoalCreated, map[string]any{
		"roadmap_id":  "rm-1",
		"goal_title":  "Run a marathon by June",
		"email":       "someone@example.com",
		"notes":       "private reflection",
		"prompt":      "system prompt text",
		"first_name":  "Alex",
		"description": "free text",
	})

	events := sink.all()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	props := events[0].Properties
	if props["roadmap_id"] != "rm-1" {
		t.Errorf("allowlisted property was dropped: %+v", props)
	}
	for _, banned := range []string{"goal_title", "email", "notes", "prompt", "first_name", "description"} {
		if _, present := props[banned]; present {
			t.Errorf("property %q reached the sink; the allowlist is the only thing "+
				"standing between user content and a long-retention analytics store", banned)
		}
	}
}

func TestEmitClient_RejectsServerAuthoritativeEvents(t *testing.T) {
	sink := withCapture(t)

	// The client must not be able to assert facts that decide revenue or
	// activation numbers.
	for _, name := range []string{
		EventSignupCompleted,
		EventWorkItemCompleted,
		EventSubscriptionActivated,
		EventTrialStarted,
		EventRoadmapActivated,
	} {
		EmitClient(context.Background(), name, "evt-1", time.Now(), nil)
	}

	if got := len(sink.all()); got != 0 {
		t.Fatalf("client emitted %d server-authoritative events, want 0", got)
	}
}

func TestEmitClient_AcceptsClientEvents(t *testing.T) {
	sink := withCapture(t)

	EmitClient(context.Background(), EventPaywallViewed, "evt-abc", time.Time{}, nil)

	events := sink.all()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Source != SourceClient {
		t.Errorf("source = %q, want %q", events[0].Source, SourceClient)
	}
	if events[0].EventID != "evt-abc" {
		t.Errorf("client event_id must be preserved for dedupe, got %q", events[0].EventID)
	}
}

func TestEmitClient_IgnoresImplausibleTimestamps(t *testing.T) {
	sink := withCapture(t)

	future := time.Now().Add(48 * time.Hour)
	ancient := time.Now().Add(-30 * 24 * time.Hour)

	EmitClient(context.Background(), EventPaywallViewed, "a", future, nil)
	EmitClient(context.Background(), EventPaywallViewed, "b", ancient, nil)

	for _, e := range sink.all() {
		// Falling back to server receipt time shifts an event slightly;
		// honoring a bad client clock scatters events across dates that never
		// happened and corrupts every cohort they land in.
		if e.Time.After(time.Now().Add(time.Minute)) {
			t.Errorf("accepted a future client timestamp: %v", e.Time)
		}
		if e.Time.Before(time.Now().Add(-8 * 24 * time.Hour)) {
			t.Errorf("accepted a stale client timestamp: %v", e.Time)
		}
	}
}

func TestEmitClient_HonorsRecentClientTimestamp(t *testing.T) {
	sink := withCapture(t)

	// An offline queue flushing an hour late should still report when the
	// thing actually happened.
	occurred := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	EmitClient(context.Background(), EventAppFirstOpen, "c", occurred, nil)

	events := sink.all()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if !events[0].Time.Equal(occurred) {
		t.Errorf("time = %v, want %v", events[0].Time, occurred)
	}
}

func TestOccurrenceID_StableAcrossTimeOfDay(t *testing.T) {
	// The scan that marks a day due and the completion that arrives that
	// evening must agree, or the numerator and denominator never join.
	morning := time.Date(2026, 3, 14, 6, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 3, 14, 22, 30, 0, 0, time.UTC)

	if a, b := OccurrenceID("habit-1", morning), OccurrenceID("habit-1", evening); a != b {
		t.Errorf("same day produced different occurrence ids: %s vs %s", a, b)
	}
}

func TestOccurrenceID_DistinctPerDayAndItem(t *testing.T) {
	day1 := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)

	if OccurrenceID("habit-1", day1) == OccurrenceID("habit-1", day2) {
		t.Error("different days must produce different occurrence ids")
	}
	if OccurrenceID("habit-1", day1) == OccurrenceID("habit-2", day1) {
		t.Error("different items must produce different occurrence ids")
	}
	if OccurrenceID("", day1) != "" {
		t.Error("empty item id must produce an empty occurrence id, not a hash of nothing")
	}
}

func TestCatalog_ClientEmittableIsSubsetOfKnown(t *testing.T) {
	for name := range clientEmittable {
		if !IsKnownEvent(name) {
			t.Errorf("%q is client-emittable but not in the catalog; the ingest "+
				"endpoint would accept an event nothing else recognizes", name)
		}
	}
}
