package main

import "testing"

func TestResolveRole_DefaultsToAll(t *testing.T) {
	t.Setenv("ROLE", "")

	role, err := resolveRole(nil)
	if err != nil {
		t.Fatalf("resolveRole() error = %v", err)
	}
	if role != RoleAll {
		t.Errorf("role = %q, want %q — a bare `go run .` must still do both", role, RoleAll)
	}
}

func TestResolveRole_FromFlag(t *testing.T) {
	t.Setenv("ROLE", "")

	for _, tc := range []struct {
		arg  string
		want Role
	}{
		{"--role=web", RoleWeb},
		{"--role=scheduler", RoleScheduler},
		{"--role=all", RoleAll},
	} {
		role, err := resolveRole([]string{tc.arg})
		if err != nil {
			t.Fatalf("resolveRole(%q) error = %v", tc.arg, err)
		}
		if role != tc.want {
			t.Errorf("resolveRole(%q) = %q, want %q", tc.arg, role, tc.want)
		}
	}
}

func TestResolveRole_FromEnvWhenNoFlag(t *testing.T) {
	t.Setenv("ROLE", "scheduler")

	role, err := resolveRole(nil)
	if err != nil {
		t.Fatalf("resolveRole() error = %v", err)
	}
	if role != RoleScheduler {
		t.Errorf("role = %q, want %q", role, RoleScheduler)
	}
}

// fly.toml sets the role via [processes] args; ROLE is the escape hatch for
// plain environments. The flag has to win so a stale env var can't override
// what the process group asked for.
func TestResolveRole_FlagBeatsEnv(t *testing.T) {
	t.Setenv("ROLE", "scheduler")

	role, err := resolveRole([]string{"--role=web"})
	if err != nil {
		t.Fatalf("resolveRole() error = %v", err)
	}
	if role != RoleWeb {
		t.Errorf("role = %q, want %q — the flag must take precedence", role, RoleWeb)
	}
}

func TestResolveRole_CaseAndWhitespaceInsensitive(t *testing.T) {
	t.Setenv("ROLE", "  Scheduler  ")

	role, err := resolveRole(nil)
	if err != nil {
		t.Fatalf("resolveRole() error = %v", err)
	}
	if role != RoleScheduler {
		t.Errorf("role = %q, want %q", role, RoleScheduler)
	}
}

// The important failure mode: an unrecognized role must NOT quietly fall back
// to RoleAll. A typo'd web process that starts cron double-fires every
// notification, which is worse and far less visible than failing to boot.
func TestResolveRole_UnknownValueIsAnError(t *testing.T) {
	t.Setenv("ROLE", "")

	if _, err := resolveRole([]string{"--role=webb"}); err == nil {
		t.Error("resolveRole(--role=webb) returned no error; a typo must not " +
			"silently start a second scheduler")
	}
	t.Setenv("ROLE", "worker")
	if _, err := resolveRole(nil); err == nil {
		t.Error("resolveRole() with ROLE=worker returned no error")
	}
}

func TestRole_Responsibilities(t *testing.T) {
	for _, tc := range []struct {
		role          Role
		wantWeb       bool
		wantScheduler bool
	}{
		{RoleWeb, true, false},
		{RoleScheduler, false, true},
		{RoleAll, true, true},
	} {
		if got := tc.role.runsWeb(); got != tc.wantWeb {
			t.Errorf("%q.runsWeb() = %v, want %v", tc.role, got, tc.wantWeb)
		}
		if got := tc.role.runsScheduler(); got != tc.wantScheduler {
			t.Errorf("%q.runsScheduler() = %v, want %v", tc.role, got, tc.wantScheduler)
		}
	}
}

// Exactly one role may run cron. If a future edit makes RoleWeb schedule, the
// deploy silently doubles every notification — pin it here.
func TestRole_OnlySchedulerAndAllRunCron(t *testing.T) {
	if RoleWeb.runsScheduler() {
		t.Fatal("RoleWeb runs the scheduler: every web machine would double-fire " +
			"every scan, and fly.toml runs 2+ of them")
	}
}
