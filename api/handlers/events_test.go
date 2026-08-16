package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"dr/server/analytics"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// analyticsRecorder captures what the ingest endpoint actually let through,
// so the assertions are about emitted events rather than log output.
type analyticsRecorder struct {
	mu     sync.Mutex
	events []analytics.Event
}

func newAnalyticsRecorder() *analyticsRecorder { return &analyticsRecorder{} }

func (r *analyticsRecorder) Send(e analytics.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *analyticsRecorder) Close(context.Context) error { return nil }

func (r *analyticsRecorder) all() []analytics.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]analytics.Event, len(r.events))
	copy(out, r.events)
	return out
}

func postEvents(t *testing.T, body any) (*httptest.ResponseRecorder, *analyticsRecorder) {
	t.Helper()

	rec := newAnalyticsRecorder()
	restore := analytics.SetSink(rec)
	t.Cleanup(restore)

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	router := gin.New()
	h := NewEventHandler()
	router.POST("/api/v1/events", h.Ingest)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	return w, rec
}

func TestIngest_AcceptsClientEmittableEvents(t *testing.T) {
	w, rec := postEvents(t, ingestRequest{Events: []clientEvent{
		{EventID: "e1", EventName: analytics.EventPaywallViewed},
		{EventID: "e2", EventName: analytics.EventAppFirstOpen},
	}})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusAccepted, w.Body.String())
	}
	if got := len(rec.all()); got != 2 {
		t.Fatalf("emitted %d events, want 2", got)
	}
}

func TestIngest_RejectsServerAuthoritativeEvents(t *testing.T) {
	// A tampered or buggy client claiming these would directly corrupt the
	// activation and revenue numbers the business runs on.
	w, rec := postEvents(t, ingestRequest{Events: []clientEvent{
		{EventID: "e1", EventName: analytics.EventSubscriptionActivated},
		{EventID: "e2", EventName: analytics.EventSignupCompleted},
		{EventID: "e3", EventName: analytics.EventWorkItemCompleted},
	}})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if got := len(rec.all()); got != 0 {
		t.Fatalf("emitted %d server-authoritative events, want 0", got)
	}

	var body struct {
		Rejected int `json:"rejected"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Rejected != 3 {
		t.Errorf("rejected = %d, want 3 (body: %s)", body.Rejected, w.Body.String())
	}
}

func TestIngest_DedupesRepeatedEventIDs(t *testing.T) {
	// The client retries its batch until it gets a 2xx, so the same event id
	// arriving twice is the normal case, not an anomaly.
	w, rec := postEvents(t, ingestRequest{Events: []clientEvent{
		{EventID: "same", EventName: analytics.EventPaywallViewed},
		{EventID: "same", EventName: analytics.EventPaywallViewed},
		{EventID: "other", EventName: analytics.EventPaywallViewed},
	}})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if got := len(rec.all()); got != 2 {
		t.Fatalf("emitted %d events, want 2 (one duplicate should be dropped)", got)
	}
}

func TestIngest_StripsDisallowedProperties(t *testing.T) {
	_, rec := postEvents(t, ingestRequest{Events: []clientEvent{{
		EventID:   "e1",
		EventName: analytics.EventPaywallViewed,
		Properties: map[string]any{
			"plan_type":  "pro",
			"goal_title": "Learn Japanese",
			"email":      "a@b.com",
		},
	}}})

	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Properties["plan_type"] != "pro" {
		t.Errorf("allowlisted property dropped: %+v", events[0].Properties)
	}
	for _, banned := range []string{"goal_title", "email"} {
		if _, ok := events[0].Properties[banned]; ok {
			t.Errorf("client-supplied %q reached the sink", banned)
		}
	}
}

func TestIngest_CapsBatchSize(t *testing.T) {
	events := make([]clientEvent, maxEventsPerBatch+50)
	for i := range events {
		events[i] = clientEvent{
			EventID:   string(rune('a'+i%26)) + string(rune('0'+i/26)),
			EventName: analytics.EventAppSessionStarted,
		}
	}

	w, rec := postEvents(t, ingestRequest{Events: events})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if got := len(rec.all()); got > maxEventsPerBatch {
		t.Errorf("emitted %d events, want at most %d", got, maxEventsPerBatch)
	}
}

func TestIngest_EmptyBatchIsAccepted(t *testing.T) {
	w, rec := postEvents(t, ingestRequest{Events: nil})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if got := len(rec.all()); got != 0 {
		t.Fatalf("emitted %d events, want 0", got)
	}
}

func TestEventDedupe_ClaimIsIdempotentWithinWindow(t *testing.T) {
	d := newEventDedupe()

	if !d.claim("x") {
		t.Fatal("first claim must succeed")
	}
	if d.claim("x") {
		t.Error("second claim of the same id must be rejected")
	}
	if !d.claim("y") {
		t.Error("a different id must still be claimable")
	}
}
