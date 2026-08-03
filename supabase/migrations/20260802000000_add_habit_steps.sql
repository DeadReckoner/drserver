-- Multi-step recurring habits/tasks (drserver: multi-step recurring items).
--
-- Mirrors the roadmap task->steps model (public.steps) for the recurring-item
-- model (public.habits). Two tables, because a habit is a TEMPLATE that
-- produces many OCCURRENCES, and an occurrence is only ever identified by
-- (habit_id, date) -- it is never materialised as its own row (see
-- habit_completions, which exists only once an occurrence is complete).
--
--   habit_steps            -- the step DEFINITIONS, owned by the template
--   habit_step_completions -- per-OCCURRENCE step state + historical snapshot
--
-- habit_step_completions carries a snapshot of the step's description and
-- ordering so a completed/missed occurrence keeps historically accurate labels
-- even after the template is later edited. Rows are materialised lazily on the
-- first write to an occurrence; an occurrence with no rows is projected from
-- the current template as all-incomplete.
--
-- ROLLBACK
--   drop table if exists public.habit_step_completions;
--   drop table if exists public.habit_steps;
--   drop function if exists update_habit_steps_updated_at();
-- Both tables are additive and nullable-free only in their own columns; no
-- existing table is altered, so dropping them fully restores prior behaviour.
-- Existing habits (which have zero rows in habit_steps) are unaffected and
-- keep the single-action completion path.

-- =============================================================================
-- CREATE HABIT STEPS TABLE (template definitions)
-- =============================================================================

create table if not exists "public"."habit_steps" (
    "habit_step_id" uuid not null default gen_random_uuid(),
    "habit_id"      uuid not null,
    "user_id"       uuid not null,  -- Denormalised, matching habit_completions

    "description"   text not null,
    "number"        integer not null default 0,  -- Explicit display order

    "created_at"    timestamp with time zone not null default now(),
    "updated_at"    timestamp with time zone not null default now(),

    constraint habit_steps_pkey primary key (habit_step_id),
    constraint habit_steps_habit_id_fkey foreign key (habit_id)
        references habits(habit_id) on update cascade on delete cascade,
    constraint habit_steps_user_id_fkey foreign key (user_id)
        references users(user_id) on update cascade on delete cascade
);

create index if not exists habit_steps_habit_id_idx
    on public.habit_steps using btree (habit_id);
create index if not exists habit_steps_habit_number_idx
    on public.habit_steps using btree (habit_id, number);
create index if not exists habit_steps_user_id_idx
    on public.habit_steps using btree (user_id);

alter table "public"."habit_steps" enable row level security;

-- =============================================================================
-- CREATE HABIT STEP COMPLETIONS TABLE (per-occurrence state + snapshot)
-- =============================================================================

create table if not exists "public"."habit_step_completions" (
    "habit_step_completion_id" uuid not null default gen_random_uuid(),
    "habit_id"                 uuid not null,
    "user_id"                  uuid not null,

    -- The occurrence this state belongs to. (habit_id, date) is the occurrence
    -- identity used everywhere else in the habit model.
    "date"                     date not null,

    -- Template step this row descends from. ON DELETE SET NULL: deleting a
    -- template step must not erase the historical record that it existed and
    -- was completed on a past occurrence.
    "habit_step_id"            uuid,

    -- Historical snapshot: what the step said and where it sat, at the time
    -- this occurrence materialised. Never rewritten for a completed occurrence.
    "description"              text not null,
    "number"                   integer not null default 0,

    "status"                   text not null default 'incomplete',
    "completed"                boolean not null default false,
    "completed_at"             timestamp with time zone,

    "created_at"               timestamp with time zone not null default now(),
    "updated_at"               timestamp with time zone not null default now(),

    constraint habit_step_completions_pkey primary key (habit_step_completion_id),
    constraint habit_step_completions_habit_id_fkey foreign key (habit_id)
        references habits(habit_id) on update cascade on delete cascade,
    constraint habit_step_completions_user_id_fkey foreign key (user_id)
        references users(user_id) on update cascade on delete cascade,
    constraint habit_step_completions_step_fkey foreign key (habit_step_id)
        references habit_steps(habit_step_id) on update cascade on delete set null
);

-- One row per (occurrence, template step). This is the idempotency guard that
-- makes rapid taps / retries / multi-device writes converge instead of
-- duplicating step state. Mirrors habit_completions_unique_habit_date, which
-- plays the same role for the parent occurrence.
create unique index if not exists habit_step_completions_unique_occurrence_step
    on public.habit_step_completions (habit_id, date, habit_step_id)
    where habit_step_id is not null;

create index if not exists habit_step_completions_occurrence_idx
    on public.habit_step_completions using btree (habit_id, date);
create index if not exists habit_step_completions_user_date_idx
    on public.habit_step_completions using btree (user_id, date);

do $$
begin
    if not exists (
        select 1 from pg_constraint where conname = 'habit_step_completions_valid_status'
    ) then
        alter table "public"."habit_step_completions"
            add constraint habit_step_completions_valid_status
            check (status in ('incomplete', 'completed', 'skipped'));
    end if;
end $$;

alter table "public"."habit_step_completions" enable row level security;

-- =============================================================================
-- UPDATED_AT TRIGGERS
-- =============================================================================

create or replace function update_habit_steps_updated_at()
returns trigger as $$
begin
    new.updated_at = now();
    return new;
end;
$$ language plpgsql;

drop trigger if exists habit_steps_updated_at on public.habit_steps;
create trigger habit_steps_updated_at
    before update on public.habit_steps
    for each row execute function update_habit_steps_updated_at();

drop trigger if exists habit_step_completions_updated_at on public.habit_step_completions;
create trigger habit_step_completions_updated_at
    before update on public.habit_step_completions
    for each row execute function update_habit_steps_updated_at();

-- =============================================================================
-- GRANTS
-- =============================================================================

grant delete, insert, references, select, trigger, truncate, update
    on table "public"."habit_steps" to "anon";
grant delete, insert, references, select, trigger, truncate, update
    on table "public"."habit_steps" to "authenticated";
grant delete, insert, references, select, trigger, truncate, update
    on table "public"."habit_steps" to "service_role";

grant delete, insert, references, select, trigger, truncate, update
    on table "public"."habit_step_completions" to "anon";
grant delete, insert, references, select, trigger, truncate, update
    on table "public"."habit_step_completions" to "authenticated";
grant delete, insert, references, select, trigger, truncate, update
    on table "public"."habit_step_completions" to "service_role";

-- =============================================================================
-- ROW LEVEL SECURITY POLICIES - HABIT STEPS
-- =============================================================================

drop policy if exists "Users can view their own habit steps" on public.habit_steps;
create policy "Users can view their own habit steps"
on public.habit_steps for select
using (auth.uid() = user_id::uuid);

drop policy if exists "Users can create their own habit steps" on public.habit_steps;
create policy "Users can create their own habit steps"
on public.habit_steps for insert
with check (auth.uid() = user_id::uuid);

drop policy if exists "Users can update their own habit steps" on public.habit_steps;
create policy "Users can update their own habit steps"
on public.habit_steps for update
using (auth.uid() = user_id::uuid)
with check (auth.uid() = user_id::uuid);

drop policy if exists "Users can delete their own habit steps" on public.habit_steps;
create policy "Users can delete their own habit steps"
on public.habit_steps for delete
using (auth.uid() = user_id::uuid);

-- =============================================================================
-- ROW LEVEL SECURITY POLICIES - HABIT STEP COMPLETIONS
-- =============================================================================

drop policy if exists "Users can view their own habit step completions" on public.habit_step_completions;
create policy "Users can view their own habit step completions"
on public.habit_step_completions for select
using (auth.uid() = user_id::uuid);

drop policy if exists "Users can create their own habit step completions" on public.habit_step_completions;
create policy "Users can create their own habit step completions"
on public.habit_step_completions for insert
with check (auth.uid() = user_id::uuid);

drop policy if exists "Users can update their own habit step completions" on public.habit_step_completions;
create policy "Users can update their own habit step completions"
on public.habit_step_completions for update
using (auth.uid() = user_id::uuid)
with check (auth.uid() = user_id::uuid);

drop policy if exists "Users can delete their own habit step completions" on public.habit_step_completions;
create policy "Users can delete their own habit step completions"
on public.habit_step_completions for delete
using (auth.uid() = user_id::uuid);

-- =============================================================================
-- COMMENTS
-- =============================================================================

comment on table public.habit_steps is
    'Step DEFINITIONS belonging to a recurring habit/task template. Mirrors public.steps for roadmap tasks.';
comment on table public.habit_step_completions is
    'Per-OCCURRENCE step state for a recurring habit/task, keyed by (habit_id, date), with a historical snapshot of the step label and order.';
comment on column public.habit_step_completions.description is
    'Snapshot of habit_steps.description when this occurrence materialised; keeps history accurate after template edits.';
comment on column public.habit_step_completions.habit_step_id is
    'Template step this descends from. NULL once the template step is deleted, preserving historical completion records.';
