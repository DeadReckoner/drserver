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

func (l *Logger) Info(msg string, args ...interface{}) {
	l.logger.Printf("[%s] INFO: %s %v", l.requestID, msg, args)
}
