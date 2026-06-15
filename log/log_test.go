package log

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"  debug  ", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		// Empty and garbage both fall back to info — a typo'd env var
		// must never silence errors.
		{"", LevelInfo},
		{"verbose", LevelInfo},
	}
	for _, c := range cases {
		if got := ParseLevel(c.in); got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// captureLogger returns a Logger writing JSON to an in-memory buffer instead of
// stderr, so tests can assert on emitted (or suppressed) output. The handler is
// permissive (debug); level filtering is exercised via SetMinLevel, matching
// how the production logger gates in code.
func captureLogger() (*Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &Logger{sl: slog.New(h)}, &buf
}

func TestMinLevelFiltering(t *testing.T) {
	cases := []struct {
		name     string
		min      Level
		emit     func(l *Logger)
		wantDrop bool
	}{
		{"debug dropped at info", LevelInfo, func(l *Logger) { l.Debug("x") }, true},
		{"info kept at info", LevelInfo, func(l *Logger) { l.Info("x") }, false},
		{"warn kept at info", LevelInfo, func(l *Logger) { l.Warn("x") }, false},
		{"error kept at info", LevelInfo, func(l *Logger) { l.Error("x") }, false},
		{"debug kept at debug", LevelDebug, func(l *Logger) { l.Debug("x") }, false},
		{"info dropped at warn", LevelWarn, func(l *Logger) { l.Info("x") }, true},
		{"warn dropped at error", LevelError, func(l *Logger) { l.Warn("x") }, true},
		{"error kept at error", LevelError, func(l *Logger) { l.Error("x") }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restore := SetMinLevel(c.min)
			defer restore()

			l, buf := captureLogger()
			c.emit(l)

			if dropped := buf.Len() == 0; dropped != c.wantDrop {
				t.Errorf("dropped = %v, want %v (output: %q)", dropped, c.wantDrop, buf.String())
			}
		})
	}
}

// TestLevelTagsInOutput guards the level → tag mapping so each method stamps
// its own name (a previous version routed everything through INFO).
func TestLevelTagsInOutput(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()

	cases := []struct {
		emit func(l *Logger)
		want string
	}{
		{func(l *Logger) { l.Debug("m") }, "DEBUG"},
		{func(l *Logger) { l.Info("m") }, "INFO"},
		{func(l *Logger) { l.Warn("m") }, "WARN"},
		{func(l *Logger) { l.Error("m") }, "ERROR"},
	}
	for _, c := range cases {
		l, buf := captureLogger()
		c.emit(l)
		if !bytes.Contains(buf.Bytes(), []byte(c.want)) {
			t.Errorf("output %q missing level tag %q", buf.String(), c.want)
		}
	}
}
