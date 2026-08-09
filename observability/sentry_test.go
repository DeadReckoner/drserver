package observability

import (
	"errors"
	"testing"
)

// The whole test suite and every local run happen with SENTRY_DSN unset, so
// "disabled" is the path that must never panic, block, or emit.
func TestInit_DisabledWithoutDSN(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")
	enabled = false

	shutdown := Init()
	defer shutdown()

	if Enabled() {
		t.Error("Enabled() = true with no SENTRY_DSN; reporting must stay off")
	}
}

// Shutdown is deferred unconditionally in main, including on the path where
// Sentry never initialized.
func TestInit_ShutdownSafeWhenDisabled(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")
	enabled = false

	shutdown := Init()
	shutdown()
	shutdown() // idempotent: a second call must not panic either
}

// Report helpers are called from the recovery middleware on every panic. If
// they misbehaved while disabled they'd turn a recoverable 500 into a crash.
func TestReportHelpers_NoOpWhenDisabled(t *testing.T) {
	enabled = false

	ReportPanic("boom", []byte("stack"), "req-1", "user-1", "POST", "/api/v1/test")
	ReportError(errors.New("nope"), map[string]string{"job": "test"})
}

// A nil error is not an error. Guarding here keeps call sites from needing to.
func TestReportError_IgnoresNilError(t *testing.T) {
	enabled = true
	defer func() { enabled = false }()

	ReportError(nil, nil)
}

// A malformed DSN must not stop the server booting — error reporting is
// diagnostic infrastructure, not a runtime dependency.
func TestInit_BadDSNDoesNotEnableOrPanic(t *testing.T) {
	t.Setenv("SENTRY_DSN", "not-a-valid-dsn")
	enabled = false

	shutdown := Init()
	defer shutdown()

	if Enabled() {
		t.Error("Enabled() = true after a failed init")
	}
}
