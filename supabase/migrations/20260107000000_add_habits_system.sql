-- Migration: Create habits system tables
-- Description: Habits and habit completions for recurring todos and habit tracking

-- =============================================================================
-- CREATE ENUM TYPE FOR RECURRENCE
-- =============================================================================

create type recurrence_type as enum ('none', 'daily', 'weekly', 'monthly');

-- =============================================================================
-- CREATE HABITS TABLE
-- =============================================================================

create table "public"."habits" (
    "habit_id" uuid not null default gen_random_uuid(),
    "user_id" uuid not null,

    -- Content
    "title" text not null,
    "description" text,

    -- Scheduling - determines when this habit is due
    "recurrence_type" recurrence_type not null default 'none',
    "days_of_week" integer not null default 0,   -- Bitmask: 1=Sun, 2=Mon, 4=Tue, 8=Wed, 16=Thu, 32=Fri, 64=Sat
    "day_of_month" integer,                       -- 1-31 for monthly recurrence
    "due_date" timestamp with time zone,          -- For one-time todos

    -- Active period - when this habit is active
    "start_date" timestamp with time zone not null default now(),
    "end_date" timestamp with time zone,

    -- Status
    "archived" boolean not null default false,

    -- Metadata
    "created_at" timestamp with time zone not null default now(),
    "updated_at" timestamp with time zone not null default now(),

    constraint habits_pkey primary key (habit_id),
    constraint habits_user_id_fkey foreign key (user_id) references users(user_id) on update cascade on delete cascade,

    -- Validation constraints
    constraint habits_valid_days_of_week check (days_of_week >= 0 and days_of_week <= 127),
    constraint habits_valid_day_of_month check (day_of_month is null or (day_of_month >= 1 and day_of_month <= 31)),
    constraint habits_valid_date_range check (end_date is null or end_date >= start_date),

    -- Recurrence-specific validation
    constraint habits_weekly_requires_days check (
        recurrence_type != 'weekly' or days_of_week > 0
    ),
    constraint habits_monthly_requires_day check (
        recurrence_type != 'monthly' or day_of_month is not null
    ),
    constraint habits_one_time_requires_due_date check (
        recurrence_type != 'none' or due_date is not null
    )
);

-- Create indexes for efficient queries
create index habits_user_id_idx on public.habits using btree (user_id);
create index habits_user_archived_idx on public.habits using btree (user_id, archived);
create index habits_user_active_idx on public.habits using btree (user_id) where archived = false;

-- Enable RLS
alter table "public"."habits" enable row level security;

-- =============================================================================
-- CREATE HABIT COMPLETIONS TABLE
-- =============================================================================

create table "public"."habit_completions" (
    "completion_id" uuid not null default gen_random_uuid(),
    "habit_id" uuid not null,
    "user_id" uuid not null,  -- Denormalized for easier queries

    "completed_at" timestamp with time zone not null default now(),
    "date" date not null,      -- The date this completion is "for"
    "notes" text,

    "created_at" timestamp with time zone not null default now(),

    constraint habit_completions_pkey primary key (completion_id),
    constraint habit_completions_habit_id_fkey foreign key (habit_id) references habits(habit_id) on update cascade on delete cascade,
    constraint habit_completions_user_id_fkey foreign key (user_id) references users(user_id) on update cascade on delete cascade,

    -- Prevent duplicate completions for the same habit on the same date
    constraint habit_completions_unique_habit_date unique (habit_id, date)
);

-- Create indexes for efficient queries
create index habit_completions_habit_id_idx on public.habit_completions using btree (habit_id);
create index habit_completions_user_id_idx on public.habit_completions using btree (user_id);
create index habit_completions_date_idx on public.habit_completions using btree (date);
create index habit_completions_user_date_idx on public.habit_completions using btree (user_id, date);

-- Enable RLS
alter table "public"."habit_completions" enable row level security;

-- =============================================================================
-- CREATE TRIGGER FOR UPDATED_AT
-- =============================================================================

-- Create or replace function to update updated_at timestamp
create or replace function update_habits_updated_at()
returns trigger as $$
begin
    new.updated_at = now();
    return new;
end;
$$ language plpgsql;

-- Create trigger on habits table
create trigger habits_updated_at
    before update on public.habits
    for each row
    execute function update_habits_updated_at();

-- =============================================================================
-- GRANT PERMISSIONS - HABITS TABLE
-- =============================================================================

grant delete on table "public"."habits" to "anon";
grant insert on table "public"."habits" to "anon";
grant references on table "public"."habits" to "anon";
grant select on table "public"."habits" to "anon";
grant trigger on table "public"."habits" to "anon";
grant truncate on table "public"."habits" to "anon";
grant update on table "public"."habits" to "anon";

grant delete on table "public"."habits" to "authenticated";
grant insert on table "public"."habits" to "authenticated";
grant references on table "public"."habits" to "authenticated";
grant select on table "public"."habits" to "authenticated";
grant trigger on table "public"."habits" to "authenticated";
grant truncate on table "public"."habits" to "authenticated";
grant update on table "public"."habits" to "authenticated";

grant delete on table "public"."habits" to "service_role";
grant insert on table "public"."habits" to "service_role";
grant references on table "public"."habits" to "service_role";
grant select on table "public"."habits" to "service_role";
grant trigger on table "public"."habits" to "service_role";
grant truncate on table "public"."habits" to "service_role";
grant update on table "public"."habits" to "service_role";

-- =============================================================================
-- GRANT PERMISSIONS - HABIT COMPLETIONS TABLE
-- =============================================================================

grant delete on table "public"."habit_completions" to "anon";
grant insert on table "public"."habit_completions" to "anon";
grant references on table "public"."habit_completions" to "anon";
grant select on table "public"."habit_completions" to "anon";
grant trigger on table "public"."habit_completions" to "anon";
grant truncate on table "public"."habit_completions" to "anon";
grant update on table "public"."habit_completions" to "anon";

grant delete on table "public"."habit_completions" to "authenticated";
grant insert on table "public"."habit_completions" to "authenticated";
grant references on table "public"."habit_completions" to "authenticated";
grant select on table "public"."habit_completions" to "authenticated";
grant trigger on table "public"."habit_completions" to "authenticated";
grant truncate on table "public"."habit_completions" to "authenticated";
grant update on table "public"."habit_completions" to "authenticated";

grant delete on table "public"."habit_completions" to "service_role";
grant insert on table "public"."habit_completions" to "service_role";
grant references on table "public"."habit_completions" to "service_role";
grant select on table "public"."habit_completions" to "service_role";
grant trigger on table "public"."habit_completions" to "service_role";
grant truncate on table "public"."habit_completions" to "service_role";
grant update on table "public"."habit_completions" to "service_role";

-- =============================================================================
-- ROW LEVEL SECURITY POLICIES - HABITS
-- =============================================================================

-- Users can view their own habits
create policy "Users can view their own habits"
on public.habits for select
using (auth.uid() = user_id::uuid);

-- Users can create their own habits
create policy "Users can create their own habits"
on public.habits for insert
with check (auth.uid() = user_id::uuid);

-- Users can update their own habits
create policy "Users can update their own habits"
on public.habits for update
using (auth.uid() = user_id::uuid)
with check (auth.uid() = user_id::uuid);

-- Users can delete their own habits
create policy "Users can delete their own habits"
on public.habits for delete
using (auth.uid() = user_id::uuid);

-- =============================================================================
-- ROW LEVEL SECURITY POLICIES - HABIT COMPLETIONS
-- =============================================================================

-- Users can view their own completions
create policy "Users can view their own completions"
on public.habit_completions for select
using (auth.uid() = user_id::uuid);

-- Users can create their own completions
create policy "Users can create their own completions"
on public.habit_completions for insert
with check (auth.uid() = user_id::uuid);

-- Users can update their own completions
create policy "Users can update their own completions"
on public.habit_completions for update
using (auth.uid() = user_id::uuid)
with check (auth.uid() = user_id::uuid);

-- Users can delete their own completions
create policy "Users can delete their own completions"
on public.habit_completions for delete
using (auth.uid() = user_id::uuid);

-- =============================================================================
-- HELPER FUNCTIONS
-- =============================================================================

-- Function to check if a habit is due on a specific date
create or replace function is_habit_due(
    p_habit_id uuid,
    p_date date
) returns boolean as $$
declare
    v_habit habits%rowtype;
    v_day_bit integer;
begin
    select * into v_habit from habits where habit_id = p_habit_id;

    if not found then
        return false;
    end if;

    -- Check if date is within active period
    if p_date < v_habit.start_date::date then
        return false;
    end if;

    if v_habit.end_date is not null and p_date > v_habit.end_date::date then
        return false;
    end if;

    -- Check recurrence type
    case v_habit.recurrence_type
        when 'none' then
            return v_habit.due_date::date = p_date;
        when 'daily' then
            return true;
        when 'weekly' then
            -- PostgreSQL: 0=Sunday, 1=Monday, etc. matches our bitmask
            v_day_bit := 1 << extract(dow from p_date)::integer;
            return (v_habit.days_of_week & v_day_bit) > 0;
        when 'monthly' then
            return extract(day from p_date) = v_habit.day_of_month;
        else
            return false;
    end case;
end;
$$ language plpgsql stable;

-- Function to get habits due on a specific date with completion status
create or replace function get_habits_for_date(
    p_user_id uuid,
    p_date date
) returns table (
    habit_id uuid,
    user_id uuid,
    title text,
    description text,
    recurrence_type recurrence_type,
    days_of_week integer,
    day_of_month integer,
    due_date timestamp with time zone,
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    archived boolean,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    is_completed boolean,
    completed_at timestamp with time zone
) as $$
begin
    return query
    select
        h.habit_id,
        h.user_id,
        h.title,
        h.description,
        h.recurrence_type,
        h.days_of_week,
        h.day_of_month,
        h.due_date,
        h.start_date,
        h.end_date,
        h.archived,
        h.created_at,
        h.updated_at,
        (hc.completion_id is not null) as is_completed,
        hc.completed_at
    from habits h
    left join habit_completions hc
        on h.habit_id = hc.habit_id
        and hc.date = p_date
    where h.user_id = p_user_id
        and h.archived = false
        and is_habit_due(h.habit_id, p_date);
end;
$$ language plpgsql stable security definer;

-- =============================================================================
-- COMMENTS FOR DOCUMENTATION
-- =============================================================================

comment on table habits is 'User habits/todos with optional recurrence patterns';
comment on table habit_completions is 'Tracks when habits were completed';
comment on column habits.days_of_week is 'Bitmask: 1=Sun, 2=Mon, 4=Tue, 8=Wed, 16=Thu, 32=Fri, 64=Sat. 127=every day, 62=weekdays';
comment on column habits.day_of_month is 'Day of month (1-31) for monthly recurring habits';
comment on column habit_completions.date is 'The calendar date this completion counts for (independent of completed_at timestamp)';
