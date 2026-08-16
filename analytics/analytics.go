// Package analytics emits structured product events — the behavioral stream
// the funnel, retention, execution, and revenue dashboards are built from.
//
// It is deliberately separate from package log. Operational logs answer "is
// the server healthy?", are read by engineers during an incident, and can be
// dropped after 30 days. Product events answer "is the product working?", are
// read by the business, and must be retained for years to compute retention at
// all. Mixing them means either paying years of retention on debug noise or
// losing the cohort data — so they get separate datasets and separate rules.
//
// Three properties are non-negotiable at every call site:
//
//   - Emitting never blocks the user's action. A dead analytics backend must
//     degrade the dashboard, never the request.
//   - Emitting never fails the caller. There is no error return by design;
//     no handler should have an error path because a metric could not be sent.
//   - Emitting never leaks content. Properties pass an allowlist (catalog.go).
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dr/server/log"
)

// Sink is where finished events go. Swappable so tests can capture events
// without a network or a log scrape.
type Sink interface {
	// Send must not block for longer than it takes to hand off the event.
	Send(Event)
	// Close flushes anything buffered, bounded by ctx.
	Close(ctx context.Context) error
}

var (
	mu      sync.RWMutex
	current Sink = newLogSink()
)

// Init selects the sink from the environment and returns a flush function the
// caller defers.
//
// With AXIOM_TOKEN and AXIOM_PRODUCT_DATASET set, events are shipped directly
// to that dataset over HTTPS. Without them, events fall back to the structured
// log stream, which the existing Fly log drain already carries into Axiom.
//
// That fallback is what makes this safe to deploy now: instrumentation lands
// and starts producing data against the current dataset immediately, and
// pointing it at a dedicated long-retention dataset later is a secrets change
// with no code change and no gap in collection.
func Init() func() {
	token := strings.TrimSpace(os.Getenv("AXIOM_TOKEN"))
	dataset := strings.TrimSpace(os.Getenv("AXIOM_PRODUCT_DATASET"))

	mu.Lock()
	defer mu.Unlock()

	if token == "" || dataset == "" {
		current = newLogSink()
		slog.Info("analytics: using log sink",
			slog.String("reason", "AXIOM_TOKEN or AXIOM_PRODUCT_DATASET unset"))
		return func() {}
	}

	sink := newAxiomSink(token, dataset)
	current = sink
	slog.Info("analytics: using direct Axiom ingest", slog.String("dataset", dataset))

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sink.Close(ctx); err != nil {
			slog.Error("analytics: flush on shutdown failed", slog.String("error", err.Error()))
		}
	}
}

// SetSink replaces the active sink and returns a function restoring the
// previous one. For tests.
func SetSink(s Sink) func() {
	mu.Lock()
	prev := current
	current = s
	mu.Unlock()
	return func() {
		mu.Lock()
		current = prev
		mu.Unlock()
	}
}

func send(e Event) {
	mu.RLock()
	s := current
	mu.RUnlock()
	if s != nil {
		s.Send(e)
	}
}

// Emit records a server-observed product event. Identity and client metadata
// come from ctx; properties are filtered through the allowlist.
//
// Unknown event names are dropped with a warning rather than emitted, so a
// typo cannot create a shadow metric that looks real in Axiom but is missing
// most of its data.
func Emit(ctx context.Context, name string, props map[string]any) {
	emit(ctx, name, SourceServer, props)
}

// EmitScheduler records an event produced by a cron job rather than a request.
func EmitScheduler(ctx context.Context, name string, props map[string]any) {
	emit(ctx, name, SourceScheduler, props)
}

func emit(ctx context.Context, name, source string, props map[string]any) {
	if !IsKnownEvent(name) {
		slog.Warn("analytics: unknown event name dropped", slog.String("event_name", name))
		return
	}
	send(newEvent(RequestContextFrom(ctx), name, source, props))
}

// EmitClient records an event reported by the mobile app. The envelope is
// rebuilt server-side from the request context and the client's declared
// identity; only the event name, its properties, the client-supplied event_id,
// and the client timestamp survive from the payload.
//
// Rebuilding rather than trusting the posted envelope is the point: a client
// that could set its own user_id could attribute another user's behavior to
// itself.
func EmitClient(ctx context.Context, name string, eventID string, occurredAt time.Time, props map[string]any) {
	if !IsKnownEvent(name) || !IsClientEmittable(name) {
		return
	}
	e := newEvent(RequestContextFrom(ctx), name, SourceClient, props)
	if eventID != "" {
		e.EventID = eventID
	}
	// Client clocks are unreliable and offline queues can flush days late, so
	// a client timestamp is only honored when it is plausible: not in the
	// future, not older than the queue could reasonably hold. Otherwise the
	// server receipt time stands, which at worst shifts an event slightly
	// later rather than scattering events across impossible dates.
	if !occurredAt.IsZero() {
		age := time.Since(occurredAt)
		if age >= 0 && age < 7*24*time.Hour {
			e.Time = occurredAt.UTC()
		}
	}
	send(e)
}

// logSink writes each event as one JSON line on the operational log stream,
// tagged so a drain or an Axiom query can separate product events from server
// logs. Synchronous, but it is a single buffered write to stderr.
type logSink struct{ logger *slog.Logger }

func newLogSink() *logSink {
	return &logSink{logger: slog.Default().With(slog.String("stream", "product_event"))}
}

func (s *logSink) Send(e Event) {
	// Marshalling to a json.RawMessage keeps the event's own schema intact as
	// a nested object instead of flattening it into the log line's fields,
	// where `environment` and `release` would collide with the base logger's.
	payload, err := json.Marshal(e)
	if err != nil {
		slog.Error("analytics: marshal failed",
			slog.String("event_name", e.EventName), slog.String("error", err.Error()))
		return
	}
	s.logger.LogAttrs(context.Background(), slog.LevelInfo, "product_event",
		slog.String("event_name", e.EventName),
		slog.Any("event", json.RawMessage(payload)),
	)
}

func (s *logSink) Close(context.Context) error { return nil }

// axiomSink batches events and ships them to Axiom's ingest API.
//
// The buffer is bounded and drops when full rather than applying backpressure.
// That is the correct trade: analytics is not worth adding latency to a user's
// request, let alone stalling one. Drops are counted and reported so a
// persistently overwhelmed sink is visible instead of silent.
type axiomSink struct {
	token   string
	dataset string
	client  *http.Client

	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup

	dropped atomic.Int64
}

const (
	axiomBufferSize   = 4096
	axiomBatchSize    = 500
	axiomFlushEvery   = 2 * time.Second
	axiomSendAttempts = 3
)

func newAxiomSink(token, dataset string) *axiomSink {
	s := &axiomSink{
		token:   token,
		dataset: dataset,
		client:  &http.Client{Timeout: 15 * time.Second},
		events:  make(chan Event, axiomBufferSize),
		done:    make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *axiomSink) Send(e Event) {
	select {
	case s.events <- e:
	default:
		// Buffer full. Report the first drop and then every 1000th, so an
		// incident is visible without the log itself becoming the overload.
		if n := s.dropped.Add(1); n == 1 || n%1000 == 0 {
			slog.Warn("analytics: event dropped, buffer full",
				slog.Int64("dropped_total", n), slog.String("event_name", e.EventName))
		}
	}
}

func (s *axiomSink) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(axiomFlushEvery)
	defer ticker.Stop()

	batch := make([]Event, 0, axiomBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.post(batch)
		batch = batch[:0]
	}

	for {
		select {
		case e := <-s.events:
			batch = append(batch, e)
			if len(batch) >= axiomBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.done:
			// Drain whatever is still queued before exiting so a clean
			// shutdown does not discard the last few seconds of events.
			for {
				select {
				case e := <-s.events:
					batch = append(batch, e)
					if len(batch) >= axiomBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (s *axiomSink) post(batch []Event) {
	body, err := json.Marshal(batch)
	if err != nil {
		slog.Error("analytics: batch marshal failed", slog.String("error", err.Error()))
		return
	}

	url := fmt.Sprintf("https://api.axiom.co/v1/datasets/%s/ingest", s.dataset)
	var lastErr error
	for attempt := 1; attempt <= axiomSendAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt-1) * 500 * time.Millisecond)
		}
		lastErr = s.postOnce(url, body)
		if lastErr == nil {
			return
		}
	}

	// Every attempt failed. Fall back to the log stream so the events still
	// reach Axiom through the Fly drain, degraded (wrong dataset, shorter
	// retention) rather than lost.
	slog.Error("analytics: ingest failed, falling back to log stream",
		slog.String("error", lastErr.Error()), slog.Int("events", len(batch)))
	fallback := newLogSink()
	for _, e := range batch {
		fallback.Send(e)
	}
}

func (s *axiomSink) postOnce(url string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("axiom ingest returned %s", resp.Status)
	}
	return nil
}

func (s *axiomSink) Close(ctx context.Context) error {
	close(s.done)
	finished := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		if n := s.dropped.Load(); n > 0 {
			slog.Warn("analytics: events dropped over process lifetime", slog.Int64("dropped_total", n))
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Heartbeat emits the telemetry heartbeat. A monitor alerting on the absence
// of this event is what distinguishes "no users did anything" from "the
// pipeline is broken" — without it, total instrumentation failure looks
// exactly like a quiet night.
func Heartbeat(ctx context.Context) {
	EmitScheduler(ctx, EventTelemetryHeartbeat, Props(
		"count", 1,
	))
	// Also emit on the operational stream. If the product-event pipeline is
	// what broke, the heartbeat riding on it cannot report its own absence.
	log.FromContext(ctx).LogAttrs(log.LevelInfo, "telemetry_heartbeat",
		slog.String("environment", log.Environment()),
		slog.String("release", log.Release()),
	)
}
