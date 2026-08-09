package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Role selects which half of the server this process runs.
//
// The scheduler is in-process cron with no distributed locking, so exactly one
// process may run it. That constraint used to be enforced by pinning the whole
// app to a single Fly machine (max_machines_running = 1), which bought
// correctness at the cost of every deploy being a brief outage and there being
// no redundancy at all.
//
// Splitting the roles moves the constraint off the web tier: the scheduler
// stays pinned to one machine, and the web process is then free to run as many
// as it likes, with rolling deploys and a spare to fail over to.
type Role string

const (
	// RoleWeb serves the API. Runs no cron, so it is safe to run N of these.
	RoleWeb Role = "web"
	// RoleScheduler runs cron and nothing else. MUST be exactly one machine —
	// a second one double-fires every scan.
	RoleScheduler Role = "scheduler"
	// RoleAll runs both in one process. The default, because it is what a
	// local `go run .` should do, and what a single-machine deploy did before
	// process groups existed.
	RoleAll Role = "all"
)

// runsWeb reports whether this role serves the public API.
func (r Role) runsWeb() bool { return r == RoleWeb || r == RoleAll }

// runsScheduler reports whether this role runs cron.
func (r Role) runsScheduler() bool { return r == RoleScheduler || r == RoleAll }

// resolveRole determines the role from the --role flag, falling back to the
// ROLE environment variable, then to RoleAll.
//
// The flag is what fly.toml's [processes] entries set, since Fly has no way to
// vary [env] per process group. ROLE exists so the same binary can be steered
// from a plain environment (docker run, a local shell) without arguments.
//
// An unrecognized value is fatal rather than defaulting: silently falling back
// to RoleAll would start a second scheduler and double-send every
// notification, which is far worse than failing to boot.
func resolveRole(args []string) (Role, error) {
	fs := flag.NewFlagSet("drserver", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	roleFlag := fs.String("role", "", "process role: web | scheduler | all (default all)")
	if err := fs.Parse(args); err != nil {
		return "", err
	}

	raw := *roleFlag
	if raw == "" {
		raw = os.Getenv("ROLE")
	}
	if raw == "" {
		return RoleAll, nil
	}

	switch Role(strings.ToLower(strings.TrimSpace(raw))) {
	case RoleWeb:
		return RoleWeb, nil
	case RoleScheduler:
		return RoleScheduler, nil
	case RoleAll:
		return RoleAll, nil
	default:
		return "", fmt.Errorf("unknown role %q: want one of web, scheduler, all", raw)
	}
}
