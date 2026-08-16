package handlers

import (
	"net/http"
	"sync"
	"time"

	"dr/server/analytics"
	"dr/server/api/utils"
	"dr/server/log"

	"github.com/gin-gonic/gin"
)

// EventHandler receives product events reported by the mobile app.
//
// Client events exist because a meaningful part of the funnel happens before
// the server ever hears from the user: the app opening, the signup screen
// appearing, the paywall being viewed, a permission prompt being declined.
// None of those produce a request, so without a client path the top of the
// funnel is permanently invisible.
//
// Everything the client sends is treated as a claim, not a fact. Event names
// are checked against the client-emittable subset, properties against the
// allowlist, and the envelope (user, platform, version) is rebuilt server-side
// from the authenticated request rather than read from the payload.
type EventHandler struct {
	dedupe *eventDedupe
}

func NewEventHandler() *EventHandler {
	return &EventHandler{dedupe: newEventDedupe()}
}

// maxEventsPerBatch bounds a single request. The mobile client batches its
// offline queue, and a queue that has been accumulating through a long offline
// stretch can be large; capping the batch keeps one request from turning into
// an unbounded amount of work, and the client simply sends the rest next time.
const maxEventsPerBatch = 100

type clientEvent struct {
	EventID    string         `json:"event_id"`
	EventName  string         `json:"event_name"`
	OccurredAt time.Time      `json:"occurred_at"`
	Properties map[string]any `json:"properties"`
}

type ingestRequest struct {
	Events []clientEvent `json:"events"`
}

// Ingest handles POST /api/v1/events.
//
// It always returns 202 for a well-formed request, reporting how many events
// were accepted rather than failing on the ones that were not. A client whose
// analytics call fails will retry, and a retry loop driven by a rejected event
// would burn the user's battery and bandwidth over a metric. Rejections are
// counted and returned so a misbehaving build is still diagnosable.
func (h *EventHandler) Ingest(c *gin.Context) {
	ctx := c.Request.Context()

	var req ingestRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	if len(req.Events) == 0 {
		utils.DataResponse(c, http.StatusAccepted, gin.H{"accepted": 0})
		return
	}
	if len(req.Events) > maxEventsPerBatch {
		req.Events = req.Events[:maxEventsPerBatch]
	}

	var accepted, rejected, duplicates int
	for _, e := range req.Events {
		if e.EventName == "" {
			rejected++
			continue
		}
		if !analytics.IsKnownEvent(e.EventName) || !analytics.IsClientEmittable(e.EventName) {
			rejected++
			continue
		}
		// The client generates event_id so that a retried batch — the common
		// case, since the queue retries until it gets a 2xx — does not double
		// count. Without it, every network blip would inflate the funnel.
		if e.EventID != "" && !h.dedupe.claim(e.EventID) {
			duplicates++
			continue
		}
		analytics.EmitClient(ctx, e.EventName, e.EventID, e.OccurredAt, e.Properties)
		accepted++
	}

	if rejected > 0 {
		// Worth a line: a build sending unknown event names is shipping
		// instrumentation that will silently produce no data, and the sooner
		// that is visible the less of a cohort is lost.
		log.Warn(ctx, "events: rejected client events",
			"rejected", rejected, "accepted", accepted)
	}

	utils.DataResponse(c, http.StatusAccepted, gin.H{
		"accepted":   accepted,
		"rejected":   rejected,
		"duplicates": duplicates,
	})
}

// eventDedupe drops event ids seen recently.
//
// In-memory and per-machine on purpose. A database round trip per event would
// make analytics the most expensive thing in the request, and the failure it
// prevents is bounded: the realistic duplicate is a client retrying a batch
// seconds after a timeout, which lands on the same machine within the window
// the vast majority of the time. Anything that slips past is caught at query
// time, where `dcount(event_id)` is the correct way to count regardless.
type eventDedupe struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

const (
	dedupeTTL     = 30 * time.Minute
	dedupeMaxSize = 50_000
)

func newEventDedupe() *eventDedupe {
	return &eventDedupe{seen: make(map[string]time.Time)}
}

// claim records id and reports whether it is new.
func (d *eventDedupe) claim(id string) bool {
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if ts, ok := d.seen[id]; ok && now.Sub(ts) < dedupeTTL {
		return false
	}

	// Evict on write rather than on a timer, so an idle process holds no
	// goroutine. The size check bounds memory even if eviction by age alone
	// cannot keep up with a burst.
	if len(d.seen) >= dedupeMaxSize {
		for k, ts := range d.seen {
			if now.Sub(ts) >= dedupeTTL {
				delete(d.seen, k)
			}
		}
		// Still full: the window is genuinely saturated. Reset rather than
		// grow without bound — losing dedupe for a window degrades accuracy
		// slightly, where an unbounded map would take the process down.
		if len(d.seen) >= dedupeMaxSize {
			d.seen = make(map[string]time.Time)
		}
	}

	d.seen[id] = now
	return true
}
