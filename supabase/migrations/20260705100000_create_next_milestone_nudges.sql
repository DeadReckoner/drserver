-- =============================================================================
-- Next-milestone nudges
-- =============================================================================
-- Per-roadmap state for the "set up your next milestone" reminder escalation
-- (drserver#67). Milestones and tasks carry no completion timestamps, so the
-- scan job records WHEN it first noticed a roadmap had run dry (every task
-- completed, roadmap still active) and escalates reminders at 24h / 72h / 1
-- week from that anchor. `stage` is the highest reminder already sent
-- (0 = none, 1 = 24h, 2 = 72h, 3 = 1 week).
--
-- Rows are pruned as soon as the roadmap stops matching (next milestone
-- generated, roadmap archived/paused/completed), so a later dry spell starts
-- a fresh cycle.
--
-- INTERNAL table: RLS enabled with NO policies (default-deny for anon/
-- authenticated). The Go backend connects via DATABASE_URL and is unaffected.

create table if not exists "public"."next_milestone_nudges" (
    "roadmap_id"  uuid not null,
    "user_id"     uuid not null,
    "detected_at" timestamptz not null default now(),
    "stage"       integer not null default 0,

    constraint next_milestone_nudges_pkey primary key ("roadmap_id"),
    constraint next_milestone_nudges_roadmap_id_fkey
        foreign key (roadmap_id) references roadmaps(roadmap_id)
        on update cascade on delete cascade,
    constraint next_milestone_nudges_user_id_fkey
        foreign key (user_id) references users(user_id)
        on update cascade on delete cascade
);

alter table "public"."next_milestone_nudges" enable row level security;

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."next_milestone_nudges" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."next_milestone_nudges" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."next_milestone_nudges" to "service_role";

comment on table public.next_milestone_nudges is 'Scan-job state for escalating next-milestone reminders (24h/72h/1wk after a roadmap runs out of incomplete tasks).';
comment on column public.next_milestone_nudges.detected_at is 'When the scan first noticed the roadmap had no incomplete tasks; anchor for the escalation clock.';
comment on column public.next_milestone_nudges.stage is 'Highest reminder stage already sent: 0 none, 1 = 24h, 2 = 72h, 3 = 1 week.';
