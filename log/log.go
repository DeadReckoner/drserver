package log

import (
	"context"
	"log"
	"os"
)

type contextKey string

const loggerKey contextKey = "logger"

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
	FromContext(ctx).Info(msg, args...)
}

func Warn(ctx context.Context, msg string, args ...interface{}) {
	FromContext(ctx).Info(msg, args...)
}

func Error(ctx context.Context, msg string, args ...interface{}) {
	FromContext(ctx).Info(msg, args...)
}

func (l *Logger) Info(msg string, args ...interface{}) {
	l.logWithArgs("INFO", msg, args...)
}

func (l *Logger) Debug(msg string, args ...interface{}) {
	l.logWithArgs("DEBUG", msg, args...)
}

func (l *Logger) Warn(msg string, args ...interface{}) {
	l.logWithArgs("WARN", msg, args...)
}

func (l *Logger) Error(msg string, args ...interface{}) {
	l.logWithArgs("ERROR", msg, args...)
}

// logWithArgs formats and logs a message with contextual arguments
func (l *Logger) logWithArgs(level, msg string, args ...interface{}) {
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
