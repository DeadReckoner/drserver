package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"testing"
)

// tleSwapBase replaces the package-wide base logger with a buffer-backed one
// for the duration of a test, so NewLogger / FromContext output can be
// captured (and stays out of the test's stderr).
func tleSwapBase(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := base
	base = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { base = prev })
	return &buf
}

// tleDecodeLines parses each emitted JSON log line into a map.
func tleDecodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestLevelString(t *testing.T) {
	cases := []struct {
		in   Level
		want string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		// Out-of-range levels fall back to INFO rather than something garbled.
		{Level(99), "INFO"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Level(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestNewLogger_StampsRequestID(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()
	buf := tleSwapBase(t)

	NewLogger("req-123").Info("hello")

	lines := tleDecodeLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (output: %q)", len(lines), buf.String())
	}
	if got := lines[0]["request_id"]; got != "req-123" {
		t.Errorf("request_id = %v, want req-123", got)
	}
	if got := lines[0]["msg"]; got != "hello" {
		t.Errorf("msg = %v, want hello", got)
	}
	if got := lines[0]["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
}

func TestLoggerWith_DerivesWithoutMutatingParent(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()

	buf := tleSwapBase(t)
	// Derive from the swapped base so output is captured.
	parent := NewLogger("req-9")
	child := parent.With("user_id", "u1")

	child.Info("child line")
	parent.Info("parent line")

	lines := tleDecodeLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (output: %q)", len(lines), buf.String())
	}
	// Child carries both the inherited request_id and the added user_id.
	if lines[0]["request_id"] != "req-9" || lines[0]["user_id"] != "u1" {
		t.Errorf("child line missing fields: %v", lines[0])
	}
	// Parent is unaffected by the derivation - no user_id key at all.
	if _, present := lines[1]["user_id"]; present {
		t.Errorf("parent line gained user_id: %v", lines[1])
	}
	if lines[1]["request_id"] != "req-9" {
		t.Errorf("parent line lost request_id: %v", lines[1])
	}
}

func TestWithLoggerFromContext_RoundTrip(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()

	l, buf := captureLogger()
	ctx := WithLogger(context.Background(), l)

	got := FromContext(ctx)
	if got != l {
		t.Fatalf("FromContext returned a different logger than was stored")
	}
	got.Info("via-ctx")
	if !bytes.Contains(buf.Bytes(), []byte("via-ctx")) {
		t.Errorf("round-tripped logger did not write to its buffer: %q", buf.String())
	}
}

func TestFromContext_BareContext_FallsBackToBase(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()
	buf := tleSwapBase(t)

	l := FromContext(context.Background())
	if l == nil {
		t.Fatal("FromContext(bare) returned nil")
	}
	l.Info("no-panic") // must not panic and must reach the base handler
	if !bytes.Contains(buf.Bytes(), []byte("no-panic")) {
		t.Errorf("fallback logger did not write to base: %q", buf.String())
	}
	// The fallback carries no request_id - it was never tagged.
	lines := tleDecodeLines(t, buf)
	if _, present := lines[0]["request_id"]; present {
		t.Errorf("fallback logger unexpectedly has request_id: %v", lines[0])
	}
}

func TestPackageLevelFuncs_UseContextLoggerAndLevelTags(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()

	l, buf := captureLogger()
	ctx := WithLogger(context.Background(), l)

	Debug(ctx, "d-msg")
	Info(ctx, "i-msg")
	Warn(ctx, "w-msg")
	Error(ctx, "e-msg")

	lines := tleDecodeLines(t, buf)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (output: %q)", len(lines), buf.String())
	}
	want := []struct{ level, msg string }{
		{"DEBUG", "d-msg"}, {"INFO", "i-msg"}, {"WARN", "w-msg"}, {"ERROR", "e-msg"},
	}
	for i, w := range want {
		if lines[i]["level"] != w.level || lines[i]["msg"] != w.msg {
			t.Errorf("line %d = %v, want level=%s msg=%s", i, lines[i], w.level, w.msg)
		}
	}
}

func TestLogAttrs_TopLevelKeys(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()

	l, buf := captureLogger()
	l.LogAttrs(LevelInfo, "access", slog.Int("status", 200), slog.String("method", "GET"))

	lines := tleDecodeLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	// Unlike the freeform helpers, attrs land at the TOP level of the JSON
	// object (machine-queried fields), not bundled under "detail".
	if got := lines[0]["status"]; got != float64(200) {
		t.Errorf("status = %v (%T), want 200 at top level", got, got)
	}
	if got := lines[0]["method"]; got != "GET" {
		t.Errorf("method = %v, want GET at top level", got)
	}
	if _, present := lines[0]["detail"]; present {
		t.Errorf("LogAttrs must not produce a detail field: %v", lines[0])
	}
}

func TestLogAttrs_DroppedBelowMinLevel(t *testing.T) {
	restore := SetMinLevel(LevelWarn)
	defer restore()

	l, buf := captureLogger()
	l.LogAttrs(LevelInfo, "quiet", slog.Int("status", 200))

	if buf.Len() != 0 {
		t.Errorf("info-level LogAttrs should be dropped at min=warn, got %q", buf.String())
	}
}

func TestNormalizeDetail(t *testing.T) {
	cases := []struct {
		name string
		in   []interface{}
		want interface{}
	}{
		// A single arg is emitted bare, not wrapped in a one-element array.
		{"single bare", []interface{}{42}, 42},
		{"multiple as array", []interface{}{"a", 7}, []interface{}{"a", 7}},
		// Errors are stringified: json.Marshal of errors.New would yield {}.
		{"error stringified", []interface{}{errors.New("boom")}, "boom"},
		{"mixed error and value", []interface{}{errors.New("boom"), 42}, []interface{}{"boom", 42}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeDetail(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("normalizeDetail(%v) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

func TestFreeformArgs_BundledUnderDetail(t *testing.T) {
	restore := SetMinLevel(LevelDebug)
	defer restore()

	l, buf := captureLogger()
	l.Info("m", errors.New("boom"), 42)

	lines := tleDecodeLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	want := []any{"boom", float64(42)} // JSON numbers decode as float64
	if got := lines[0]["detail"]; !reflect.DeepEqual(got, want) {
		t.Errorf("detail = %#v, want %#v", got, want)
	}
}
