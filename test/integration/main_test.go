//go:build integration

// Package integration holds end-to-end tests that run the real notification
// SQL against a throwaway PostgreSQL started via testcontainers. They are
// gated behind the `integration` build tag so the normal `go test ./...` /
// `make ci` path (which has no Docker) never compiles or runs them.
//
// Run locally with Docker:
//
//	make integration_test
//	# or: go test -tags integration ./test/integration/...
//
// CI runs `make integration_test` in the `integration` job (see
// .github/workflows/test.yml).
//
// SCHEMA STRATEGY
// The very first app migration is a full Supabase pg_dump (pg_graphql,
// pgsodium, vault, …) that will not replay on vanilla Postgres, so we do NOT
// replay the whole migration history. Instead we bootstrap the handful of
// Supabase-isms the notification migrations depend on (the anon/authenticated/
// service_role roles, an auth.uid() stub, pgcrypto) plus minimal users/roadmaps
// tables, then apply the REAL notification migration files on top. That keeps
// the objects under test — the JSONB defaults, the auto-settings trigger, the
// delivery-log/pending/digest constraints — verbatim from production while
// keeping the dependency surface controlled and deterministic.
//
// COVERAGE BOUNDARY
// These tests cover the DB-facing notification paths end-to-end against real
// Postgres: the auto-settings trigger + JSONB defaults, the Scanner/merge-on-
// read, RecordDelivery idempotency, the Dispatch send/defer/digest branches,
// and the pending/digest queue upserts. They deliberately do NOT cover:
//   - the cron candidate-query SQL that needs roadmaps/tasks/milestones DATA
//     (missed-task, falling-behind, at-risk, anchor-batch). Those finders are
//     unexported; testing them belongs in an in-package (jobs) integration
//     file seeding those tables — a natural follow-up to this harness.
//   - the actual email send to Resend (external service) — covered by the
//     email package's unit tests and the manual TestFlight pass
//     (notifications-qa.md).
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dr/server/database"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// testDB is the gorm handle bound to the container for the whole package run.
// Also installed as the database package global via SetDBForTest so the real
// database.* functions (which read that global) hit the container.
var testDB *gorm.DB

// bootstrapSQL creates exactly what the notification migrations assume exists:
// the Supabase roles + auth.uid() referenced by GRANTs and RLS policies, and
// the users/roadmaps tables that the settings/override foreign keys point at.
// users intentionally omits `timezone` — the real 20260518000000 migration
// adds it, so applying that migration is itself exercised.
const bootstrapSQL = `
create extension if not exists pgcrypto;

do $$ begin
  if not exists (select from pg_roles where rolname = 'anon') then create role anon; end if;
  if not exists (select from pg_roles where rolname = 'authenticated') then create role authenticated; end if;
  if not exists (select from pg_roles where rolname = 'service_role') then create role service_role; end if;
end $$;

create schema if not exists auth;
create or replace function auth.uid() returns uuid language sql stable as $$
  select nullif(current_setting('request.jwt.claim.sub', true), '')::uuid
$$;

create table if not exists public.users (
  user_id    uuid primary key default gen_random_uuid(),
  email      text not null default '',
  first_name text not null default '',
  last_name  text not null default ''
);

create table if not exists public.roadmaps (
  roadmap_id uuid primary key default gen_random_uuid(),
  user_id    uuid references public.users(user_id) on delete cascade,
  anchor_id  uuid,
  goal       text not null default '',
  archived   boolean not null default false,
  paused_at  timestamptz,
  completed  boolean not null default false,
  start_date timestamptz
);

-- milestones/tasks carry only the columns the tests under this harness touch:
-- the hierarchy keys plus the day/date/completion fields that scheduling and
-- the cron candidate queries read. Not the production schema in full.
create table if not exists public.milestones (
  milestone_id uuid primary key default gen_random_uuid(),
  roadmap_id   uuid references public.roadmaps(roadmap_id) on delete cascade,
  user_id      uuid,
  phase_id     uuid,
  sub_goal     text not null default '',
  day          integer not null default 0,
  date         timestamptz,
  reached      boolean not null default false,
  status       text not null default 'not_started',
  order_index  integer not null default 0
);

create table if not exists public.tasks (
  task_id      uuid primary key default gen_random_uuid(),
  roadmap_id   uuid references public.roadmaps(roadmap_id) on delete cascade,
  milestone_id uuid references public.milestones(milestone_id) on delete cascade,
  user_id      uuid,
  title        text not null default '',
  day          integer not null default 0,
  date         timestamptz,
  completed    boolean not null default false,
  status       text not null default 'not_started'
);
`

// migrationFiles are applied, in order, after the bootstrap. These are the
// real production migrations for the notification subsystem.
var migrationFiles = []string{
	"20260518000000_add_timezone_to_users.sql",
	"20260518000001_add_notification_settings_tables.sql",
	"20260601000000_create_email_log.sql",
	"20260602000000_create_pending_notifications.sql",
	"20260603000000_create_digest_items.sql",
	"20260614000000_add_carryover_notification_prefs.sql",
}

func TestMain(m *testing.M) {
	code, err := runSuite(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// dsnEnv lets a developer without a running Docker daemon point the suite at a
// Postgres they already have. The schema bootstrap is not idempotent (the real
// migration files use bare CREATE TABLE), so it must name an EMPTY database
// that the suite is free to own:
//
//	createdb dr_integration
//	INTEGRATION_DATABASE_URL="host=/tmp dbname=dr_integration sslmode=disable" make integration_test
//
// Unset — the normal case, and what CI uses — the suite starts its own
// throwaway container instead.
const dsnEnv = "INTEGRATION_DATABASE_URL"

func runSuite(m *testing.M) (int, error) {
	ctx := context.Background()

	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("dr_test"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
		)
		if err != nil {
			return 1, fmt.Errorf("start container: %w", err)
		}
		defer func() { _ = container.Terminate(ctx) }()

		if dsn, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			return 1, fmt.Errorf("connection string: %w", err)
		}
	}

	gdb, err := openWithRetry(dsn)
	if err != nil {
		return 1, fmt.Errorf("open db: %w", err)
	}
	testDB = gdb

	if err := applySchema(gdb); err != nil {
		return 1, fmt.Errorf("apply schema: %w", err)
	}

	// Point the database package's global at the container so the real
	// database.* functions exercised by the tests hit it.
	restore := database.SetDBForTest(gdb)
	defer restore()

	return m.Run(), nil
}

// openWithRetry opens gorm against the container, pinging until the server is
// accepting connections (the container may report started a beat before the
// listener is ready). Version-independent of any testcontainers wait strategy.
func openWithRetry(dsn string) (*gorm.DB, error) {
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		gdb, err := gorm.Open(postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true, // mirrors production database.Connect
		}), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		if err == nil {
			var sqlDB *sql.DB
			if sqlDB, err = gdb.DB(); err == nil {
				if err = sqlDB.Ping(); err == nil {
					return gdb, nil
				}
			}
		}
		lastErr = err
		time.Sleep(time.Second)
	}
	return nil, lastErr
}

// applySchema runs the bootstrap then each real migration file in order.
func applySchema(gdb *gorm.DB) error {
	if err := gdb.Exec(bootstrapSQL).Error; err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	for _, name := range migrationFiles {
		path := filepath.Join("..", "..", "supabase", "migrations", name)
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		// PreferSimpleProtocol lets a whole multi-statement file (including
		// dollar-quoted function bodies) run in a single Exec.
		if err := gdb.Exec(string(b)).Error; err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// createUser inserts a user (triggering default-settings creation) and returns
// its id. tz is the IANA timezone stored on the row.
func createUser(t *testing.T, tz string) string {
	t.Helper()
	var id string
	err := testDB.Raw(
		`insert into public.users (email, first_name, last_name, timezone)
		 values (?, ?, ?, ?) returning user_id`,
		fmt.Sprintf("u%d@example.test", time.Now().UnixNano()), "Test", "User", tz,
	).Row().Scan(&id)
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	return id
}

// countRows is a tiny raw-SQL count helper for assertions against the real tables.
func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.Raw(query, args...).Row().Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}
