package log

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type contextKey string

const loggerKey contextKey = "logger"

// Level is the log severity. Ordering is debug < info < warn < error;
// messages below the configured minimum level are dropped.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// slogLevel maps our Level onto slog's so the emitted JSON carries the standard
// "level":"DEBUG|INFO|WARN|ERROR" field that log aggregators key off of.
func (l Level) slogLevel() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// minLevel is the process-wide minimum level, read once at startup from the
// LOG_LEVEL env var ("debug" | "info" | "warn" | "error", case-insensitive).
// Defaults to info, which keeps high-volume debug detail (auth response
// bodies, no-op cron ticks) out of production logs.
//
// Filtering happens in code (logWithArgs) against this value rather than in the
// slog handler, so SetMinLevel keeps working for tests and the handler can stay
// permissive. The bare standard-library log.* calls bridged in via SetDefault
// are gated by the handler instead and are migrated in a later pass.
var minLevel = ParseLevel(os.Getenv("LOG_LEVEL"))

// ParseLevel maps a LOG_LEVEL string to a Level. Unrecognized or empty input
// falls back to info — a typo in an env var should never silence errors.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// SetMinLevel overrides the process minimum level and returns a func that
// restores the previous value. Intended for tests.
func SetMinLevel(l Level) func() {
	prev := minLevel
	minLevel = l
	return func() { minLevel = prev }
}

// base is the process-wide JSON logger. Every per-request logger is derived
// from it via With(...), so all output shares one structured JSON format that
// log aggregators (Axiom) and Fly's log stream can parse field-by-field.
//
// Registering it as slog's default also routes the standard library's log
// package — the bare log.Printf/Println calls still scattered through the
// codebase — through this same JSON handler, so the stream is uniform even
// before those call sites are migrated to carry request context.
var base *slog.Logger

func init() {
	// Handler stays at debug; level filtering is done in code against minLevel
	// (see its doc comment) so SetMinLevel remains the single knob for tests.
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	base = slog.New(handler)
	slog.SetDefault(base)
}

// Logger is a request-scoped wrapper around slog. It is created once per
// request (tagged with request_id, and later user_id) and threaded through
// context so handlers and the database layer log with that context attached.
type Logger struct {
	sl *slog.Logger
}

// NewLogger returns a logger that tags every line with the given request_id.
func NewLogger(requestID string) *Logger {
	return &Logger{sl: base.With(slog.String("request_id", requestID))}
}

// With returns a derived logger that adds the given attributes to every line.
// Accepts slog.Attr values or key/value pairs (slog's With semantics). Used by
// middleware to attach user_id once auth has resolved it.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{sl: l.sl.With(args...)}
}

func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext returns the request-scoped logger, or a base logger (no
// request_id) if none was injected — so logging never panics on a bare context.
func FromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(loggerKey).(*Logger); ok {
		return logger
	}
	return &Logger{sl: base}
}

func Info(ctx context.Context, msg string, args ...interface{}) {
	FromContext(ctx).Info(msg, args...)
}

func Debug(ctx context.Context, msg string, args ...interface{}) {
	FromContext(ctx).Debug(msg, args...)
}

func Warn(ctx context.Context, msg string, args ...interface{}) {
	FromContext(ctx).Warn(msg, args...)
}

func Error(ctx context.Context, msg string, args ...interface{}) {
	FromContext(ctx).Error(msg, args...)
}

func (l *Logger) Info(msg string, args ...interface{}) {
	l.logWithArgs(LevelInfo, msg, args...)
}

func (l *Logger) Debug(msg string, args ...interface{}) {
	l.logWithArgs(LevelDebug, msg, args...)
}

func (l *Logger) Warn(msg string, args ...interface{}) {
	l.logWithArgs(LevelWarn, msg, args...)
}

func (l *Logger) Error(msg string, args ...interface{}) {
	l.logWithArgs(LevelError, msg, args...)
}

// LogAttrs logs with first-class slog attributes that land at the top level of
// the JSON object (method, status, duration_ms, ...). Unlike the freeform
// Info/Error helpers — which bundle their variadic args under a single "detail"
// field — this is for machine-queried fields like the HTTP access log and any
// future metrics. request_id (and user_id, where attached) come along
// automatically from the logger the attrs are applied to.
func (l *Logger) LogAttrs(level Level, msg string, attrs ...slog.Attr) {
	if level < minLevel {
		return
	}
	l.sl.LogAttrs(context.Background(), level.slogLevel(), msg, attrs...)
}

// logWithArgs captures the legacy freeform variadic args under a single
// "detail" field, dropping the message entirely when below the process minimum
// level. The existing call sites pass a grab-bag — errors, ids, counts, and the
// occasional printf-style format string + values — none of which are slog
// key/value pairs, so forwarding them to slog directly would produce garbled
// output. Bundling under "detail" keeps every call site working unchanged while
// still emitting valid structured JSON.
func (l *Logger) logWithArgs(level Level, msg string, args ...interface{}) {
	if level < minLevel {
		return
	}

	if len(args) == 0 {
		l.sl.LogAttrs(context.Background(), level.slogLevel(), msg)
		return
	}

	l.sl.LogAttrs(context.Background(), level.slogLevel(), msg, slog.Any("detail", normalizeDetail(args)))
}

// normalizeDetail makes freeform args JSON-friendly: a single arg is emitted
// bare, multiple args as an array. Errors are stringified because json.Marshal
// renders most error types as an empty object.
func normalizeDetail(args []interface{}) interface{} {
	out := make([]interface{}, len(args))
	for i, a := range args {
		if err, ok := a.(error); ok {
			out[i] = err.Error()
			continue
		}
		out[i] = a
	}
	if len(out) == 1 {
		return out[0]
	}
	return out
}
