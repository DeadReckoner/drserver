// Package observability wires error tracking. It is deliberately the only
// place that imports the Sentry SDK, so the rest of the codebase reports
// errors through a small local surface and never grows a hard dependency on
// the vendor.
//
// Everything here is a no-op when SENTRY_DSN is unset. That is the normal
// state for local development and the entire test suite — nothing needs to
// know whether Sentry is configured, and no test ever ships an event.
package observability

import (
	"context"
	"fmt"
	"os"
	"time"

	"dr/server/log"

	"github.com/getsentry/sentry-go"
)

// logCtx tags this package's own startup lines with a fixed correlation id;
// Init runs before any request context exists.
func logCtx() context.Context {
	return log.WithLogger(context.Background(), log.NewLogger("observability"))
}

// flushTimeout bounds how long shutdown waits for queued events to reach
// Sentry. The SDK sends asynchronously, so without a flush a crash-and-exit
// loses exactly the events you most wanted.
const flushTimeout = 2 * time.Second

// enabled records whether Init actually configured a client. Read by the
// report helpers so they can return immediately when Sentry is off.
var enabled bool

// Init configures error reporting from the environment. Returns a shutdown
// function that flushes pending events; it is always safe to call, including
// when Sentry is disabled.
//
// Configuration:
//
//	SENTRY_DSN         — the project DSN. Unset disables reporting entirely.
//	SENTRY_ENVIRONMENT — environment tag (defaults to "production").
//	FLY_MACHINE_VERSION — used as the release when present; Fly sets it per
//	                      deploy, which is what makes "first seen in release X"
//	                      meaningful.
func Init() (shutdown func()) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		log.Info(logCtx(), "Sentry disabled: SENTRY_DSN not set")
		return func() {}
	}

	env := os.Getenv("SENTRY_ENVIRONMENT")
	if env == "" {
		env = "production"
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: env,
		Release:     os.Getenv("FLY_MACHINE_VERSION"),
		// The structured logger already ships every request to Axiom, so
		// Sentry is for errors only — no performance tracing, no spend on
		// duplicate request telemetry.
		EnableTracing: false,
		// Panics carry the most diagnostic value and are rare enough that
		// the stack trace is worth the payload size.
		AttachStacktrace: true,
	})
	if err != nil {
		// A bad DSN must not stop the server from booting: error reporting
		// is diagnostic infrastructure, not a runtime dependency.
		log.Error(logCtx(), "Sentry init failed; continuing without error reporting", err)
		return func() {}
	}

	enabled = true
	log.Info(logCtx(), fmt.Sprintf("Sentry enabled (environment=%s)", env))

	return func() {
		sentry.Flush(flushTimeout)
	}
}

// Enabled reports whether error tracking is active.
func Enabled() bool { return enabled }

// ReportPanic sends a recovered panic with its stack. Called from the Recovery
// middleware; the middleware still logs and still returns 500 regardless.
//
// requestID, userID, route and method are threaded through as tags so a Sentry
// issue can be correlated back to the matching Axiom access-log line.
func ReportPanic(recovered any, stack []byte, requestID, userID, method, route string) {
	if !enabled {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelFatal)
		scope.SetTag("request_id", requestID)
		scope.SetTag("http.method", method)
		scope.SetTag("http.route", route)
		if userID != "" {
			scope.SetUser(sentry.User{ID: userID})
		}
		// The SDK captures its own stack at the point of Recover; this is the
		// stack as seen by the recovery middleware, which is the one that
		// actually shows the panicking handler frame.
		scope.SetContext("panic", sentry.Context{"stack": string(stack)})
		sentry.CurrentHub().Recover(recovered)
	})
}

// ReportError sends a non-panic error worth waking someone for. Use sparingly:
// the structured logger is the right home for routine failures, and an error
// tracker that cries wolf gets muted. Reserve this for conditions that mean
// something is broken rather than something went wrong for one user.
func ReportError(err error, tags map[string]string) {
	if !enabled || err == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		sentry.CaptureException(err)
	})
}
