package log

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
)

// Failure categories. Every failure the Production Health dashboard tracks maps
// to exactly one of these, so a single query grouped by failure_category
// answers "what is broken right now" without knowing every message string in
// the codebase.
const (
	FailureAuth          = "auth"
	FailureDatabase      = "database"
	FailureAIProvider    = "ai_provider"
	FailureNotification  = "notification"
	FailureBilling       = "billing"
	FailureBackgroundJob = "background_job"
	FailureEmail         = "email"
	FailureStorage       = "storage"
	FailureAnalytics     = "analytics"
)

// Error codes. Free-text error messages are unusable as a dashboard dimension:
// they embed ids, they change when someone rewords a string, and they produce
// thousands of one-row groups. A closed set of codes is what makes "failures by
// cause" a chart rather than a list.
const (
	CodeTimeout        = "timeout"
	CodeCanceled       = "canceled"
	CodeUnavailable    = "unavailable"
	CodeUnauthorized   = "unauthorized"
	CodeRateLimited    = "rate_limited"
	CodeInvalidRequest = "invalid_request"
	CodeInvalidOutput  = "invalid_output"
	CodeConflict       = "conflict"
	CodeNotFound       = "not_found"
	CodeUnknown        = "unknown"
)

// Failure emits one structured failure line.
//
// It is deliberately a distinct message ("failure") rather than a level-based
// convention. Filtering on level == "ERROR" catches every stray error line in
// the codebase, including ones that are noisy, expected, or not about a
// subsystem being down — which is why error-count panels built that way are
// never trusted and eventually ignored. A dedicated stream that a call site
// opts into stays meaningful enough to alert on.
//
// The error is recorded as a message string for a human reading the line, and
// separately as a code for machines to group by. Both are needed: the code
// tells you how many and the message tells you which one.
func Failure(ctx context.Context, category, code string, err error, attrs ...slog.Attr) {
	base := []slog.Attr{
		slog.String("failure_category", category),
		slog.String("error_code", code),
	}
	if err != nil {
		base = append(base, slog.String("error", err.Error()))
	}
	FromContext(ctx).LogAttrs(LevelError, "failure", append(base, attrs...)...)
}

// ErrorCode classifies an error into one of the codes above.
//
// Best-effort by design: it recognizes the transport-level failures that all
// outbound calls share (timeouts, cancellation, refused connections, DNS) and
// falls back to string matching on the vocabulary providers actually use.
// Callers that know better should pass an explicit code instead — this is the
// floor, not the ceiling.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return CodeTimeout
	case errors.Is(err, context.Canceled):
		return CodeCanceled
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CodeTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return CodeUnavailable
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return CodeUnavailable
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return CodeTimeout
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "429"),
		strings.Contains(msg, "too many requests"), strings.Contains(msg, "overloaded"):
		return CodeRateLimited
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"),
		strings.Contains(msg, "forbidden"), strings.Contains(msg, "403"),
		strings.Contains(msg, "api key"):
		return CodeUnauthorized
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "503"), strings.Contains(msg, "502"),
		strings.Contains(msg, "unavailable"):
		return CodeUnavailable
	case strings.Contains(msg, "duplicate key"), strings.Contains(msg, "23505"),
		strings.Contains(msg, "conflict"):
		return CodeConflict
	case strings.Contains(msg, "not found"), strings.Contains(msg, "no rows"):
		return CodeNotFound
	case strings.Contains(msg, "failed to parse"), strings.Contains(msg, "unmarshal"),
		strings.Contains(msg, "no tool use"), strings.Contains(msg, "invalid character"):
		return CodeInvalidOutput
	case strings.Contains(msg, "invalid"), strings.Contains(msg, "400"):
		return CodeInvalidRequest
	}
	return CodeUnknown
}
