package analytics

import "context"

type contextKey string

const requestContextKey contextKey = "analytics.request"

// RequestContext is the per-request identity and client metadata every event
// emitted during that request inherits. Middleware populates it once; handlers
// never assemble it by hand, so a handler that forgets to pass app_version is
// not a thing that can happen.
type RequestContext struct {
	UserID    string
	RequestID string

	// Client metadata, sent by the mobile app as headers. Empty for requests
	// from anything else (webhooks, curl, the scheduler).
	Platform    string
	AppVersion  string
	BuildNumber string
	AnonymousID string
	SessionID   string
}

// WithRequestContext returns a context carrying rc.
func WithRequestContext(ctx context.Context, rc RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey, rc)
}

// RequestContextFrom returns the request context, or a zero value when none
// was attached. Never nil — background work (cron jobs, detached generation
// goroutines) legitimately emits events with no request behind them.
func RequestContextFrom(ctx context.Context) RequestContext {
	if ctx == nil {
		return RequestContext{}
	}
	if rc, ok := ctx.Value(requestContextKey).(RequestContext); ok {
		return rc
	}
	return RequestContext{}
}

// WithUserID returns a context whose analytics identity carries userID,
// preserving any client metadata already attached.
//
// Needed because roadmap generation runs in a goroutine that outlives the
// request: the handler detaches a fresh context (the request's is canceled the
// moment the response is written) and re-attaches the identity so the
// generation events still land against the right user.
func WithUserID(ctx context.Context, userID string) context.Context {
	rc := RequestContextFrom(ctx)
	rc.UserID = userID
	return WithRequestContext(ctx, rc)
}
