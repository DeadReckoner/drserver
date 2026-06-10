package log

import (
	"context"
	"log"
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

// minLevel is the process-wide minimum level, read once at startup from the
// LOG_LEVEL env var ("debug" | "info" | "warn" | "error", case-insensitive).
// Defaults to info, which keeps high-volume debug detail (auth response
// bodies, no-op cron ticks) out of production logs.
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

type Logger struct {
	logger    *log.Logger
	requestID string
}

func NewLogger(requestID string) *Logger {

	return &Logger{
		logger:    log.New(os.Stderr, "[DEAD RECKONER] - ", log.Ldate|log.Ltime|log.Lshortfile),
		requestID: requestID,
	}
}

func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func FromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(loggerKey).(*Logger); ok {
		return logger
	}
	return NewLogger("unknown")
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

// logWithArgs formats and logs a message with contextual arguments, dropping
// it entirely when below the process minimum level.
func (l *Logger) logWithArgs(level Level, msg string, args ...interface{}) {
	if level < minLevel {
		return
	}

	if len(args) == 0 {
		l.logger.Printf("[%s] %s: %s", l.requestID, level, msg)
		return
	}

	// If there's exactly one arg and it's a string, append it directly
	if len(args) == 1 {
		if str, ok := args[0].(string); ok {
			l.logger.Printf("[%s] %s: %s [%s]", l.requestID, level, msg, str)
			return
		}
	}

	// Otherwise format all args
	l.logger.Printf("[%s] %s: %s %v", l.requestID, level, msg, args)
}
