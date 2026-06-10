-- =============================================================================
-- Notification settings, overrides, and delivery log
-- =============================================================================
-- Three tables that back the notification settings system:
--
--   notification_settings     — one row per user; global + my-goals + F&A defaults.
--   notification_overrides    — per-person OR per-anchored-goal F&A overrides.
--   notification_delivery_log — idempotency log; one row per delivered notification.
--
-- The JSONB DEFAULTs below MUST match types.DefaultMyGoalsPrefs() and
-- types.DefaultFAndAPrefs() in Go. If you change defaults here, change them
-- there too (and vice versa).
--
-- The "apply same to all friends" toggle (apply_to_all_friends) gates the
-- resolver in code. Per-person overrides live durably regardless of the toggle.

-- =============================================================================
-- notification_settings
-- =============================================================================
create table "public"."notification_settings" (
    "user_id" uuid not null,

    -- Channels
    "push_enabled"  boolean not null default true,
    "email_enabled" boolean not null default true,

    -- Quiet hours (interpreted in the user's timezone — see users.timezone)
    "quiet_hours_enabled" boolean not null default false,
    "quiet_hours_start"   time    not null default '22:00',
    "quiet_hours_end"     time    not null default '08:00',

    -- Digest preferences
    "digest_frequency"            text     not null default 'none',
    "digest_delivery_hour"        smallint not null default 8,
    "digest_delivery_day_of_week" smallint not null default 1,

    -- Global mute — when set in the future, every notification skips
    "global_mute_until" timestamp with time zone,

    -- Per-notification-type prefs for the user's own goals.
    -- Always fully populated; partial overrides live in notification_overrides.
    "my_goals_prefs" jsonb not null default '{
        "daily_task_reminder":       {"enabled": true, "delivery": "immediate"},
        "task_due_reminder":         {"enabled": true, "delivery": "immediate"},
        "missed_task":               {"enabled": true, "delivery": "digest"},
        "falling_behind":            {"enabled": true, "delivery": "digest"},
        "milestone_due":             {"enabled": true, "delivery": "immediate"},
        "milestone_at_risk":         {"enabled": true, "delivery": "immediate"},
        "follow_on_milestone_ready": {"enabled": true, "delivery": "immediate"},
        "goal_milestone_completed":  {"enabled": true, "delivery": "immediate"}
    }'::jsonb,

    -- Friends & Accountability
    "f_and_a_enabled"      boolean not null default true,
    "apply_to_all_friends" boolean not null default true,
    "f_and_a_prefs" jsonb not null default '{
        "friend_request":          {"enabled": true, "delivery": "immediate"},
        "anchor_request":          {"enabled": true, "delivery": "immediate"},
        "anchor_accepted_removed": {"enabled": true, "delivery": "immediate"},
        "missed_progress":         {"enabled": true, "delivery": "digest"},
        "falling_behind":          {"enabled": true, "delivery": "digest"},
        "milestone_at_risk":       {"enabled": true, "delivery": "immediate"},
        "milestone_completed":     {"enabled": true, "delivery": "digest"},
        "goal_completed":          {"enabled": true, "delivery": "immediate"},
        "timeline_updated":        {"enabled": true, "delivery": "digest"}
    }'::jsonb,

    "created_at" timestamp with time zone not null default now(),
    "updated_at" timestamp with time zone not null default now(),

    constraint notification_settings_pkey primary key (user_id),
    constraint notification_settings_user_id_fkey
        foreign key (user_id) references users(user_id) on update cascade on delete cascade,
    constraint notification_settings_digest_frequency_check
        check (digest_frequency in ('none','daily','weekly')),
    constraint notification_settings_digest_delivery_hour_check
        check (digest_delivery_hour between 0 and 23),
    constraint notification_settings_digest_delivery_day_of_week_check
        check (digest_delivery_day_of_week between 0 and 6)
);


-- =============================================================================
-- notification_overrides
-- =============================================================================
create table "public"."notification_overrides" (
    "id"      uuid not null default gen_random_uuid(),
    "user_id" uuid not null,

    -- Exactly ONE of these is non-null. Enforced by CHECK below.
    "friend_id"           uuid,
    "anchored_roadmap_id" uuid,

    -- Partial F&A prefs (only fields that override the user's f_and_a_prefs).
    -- Shape: { "<type>": { "enabled": bool, "delivery": "immediate"|"digest" } }
    "prefs" jsonb not null default '{}'::jsonb,

    -- Per-target mute window
    "mute_until" timestamp with time zone,

    "created_at" timestamp with time zone not null default now(),
    "updated_at" timestamp with time zone not null default now(),

    constraint notification_overrides_pkey primary key (id),
    constraint notification_overrides_user_id_fkey
        foreign key (user_id) references users(user_id) on update cascade on delete cascade,
    constraint notification_overrides_friend_id_fkey
        foreign key (friend_id) references users(user_id) on update cascade on delete cascade,
    constraint notification_overrides_anchored_roadmap_id_fkey
        foreign key (anchored_roadmap_id) references roadmaps(roadmap_id) on update cascade on delete cascade,
    constraint notification_overrides_target_xor check (
        (friend_id is not null and anchored_roadmap_id is null)
     or (friend_id is null     and anchored_roadmap_id is not null)
    )
);

-- At most one override per (user, friend) and per (user, anchored goal)
create unique index notification_overrides_user_friend_uniq
    on public.notification_overrides (user_id, friend_id)
    where friend_id is not null;

create unique index notification_overrides_user_roadmap_uniq
    on public.notification_overrides (user_id, anchored_roadmap_id)
    where anchored_roadmap_id is not null;


-- =============================================================================
-- notification_delivery_log
-- =============================================================================
-- Senders do INSERT ... ON CONFLICT DO NOTHING; the unique constraint on
-- (user_id, notification_type, dedup_key, channel) is the entire idempotency
-- guarantee. If a row was inserted, send. If not, someone already delivered.
create table "public"."notification_delivery_log" (
    "id"                uuid not null default gen_random_uuid(),
    "user_id"           uuid not null,
    "notification_type" text not null,
    "dedup_key"         text not null,
    "channel"           text not null,
    "delivered_at"      timestamp with time zone not null default now(),

    constraint notification_delivery_log_pkey primary key (id),
    constraint notification_delivery_log_user_id_fkey
        foreign key (user_id) references users(user_id) on update cascade on delete cascade,
    constraint notification_delivery_log_channel_check
        check (channel in ('push','email')),
    constraint notification_delivery_log_uniq
        unique (user_id, notification_type, dedup_key, channel)
);

-- Frequency-cap and "did I send X recently?" queries scan by user + time.
create index notification_delivery_log_user_time
    on public.notification_delivery_log (user_id, delivered_at desc);


-- =============================================================================
-- Trigger: auto-create a default settings row when a new user is inserted.
-- =============================================================================
create or replace function public.create_default_notification_settings()
returns trigger as $$
begin
    insert into public.notification_settings (user_id)
    values (new.user_id)
    on conflict (user_id) do nothing;
    return new;
end;
$$ language plpgsql security definer;

create trigger on_user_created_create_notification_settings
    after insert on public.users
    for each row execute function public.create_default_notification_settings();


-- =============================================================================
-- Backfill: create default settings for all existing users.
-- =============================================================================
insert into public.notification_settings (user_id)
select user_id from public.users
on conflict (user_id) do nothing;


-- =============================================================================
-- RLS + grants (matches anchor_requests pattern)
-- =============================================================================
alter table "public"."notification_settings"     enable row level security;
alter table "public"."notification_overrides"    enable row level security;
alter table "public"."notification_delivery_log" enable row level security;

-- notification_settings: user reads/updates their own row only
create policy "Users can view their notification settings"
    on public.notification_settings for select
    using (auth.uid() = user_id);

create policy "Users can update their notification settings"
    on public.notification_settings for update
    using (auth.uid() = user_id);

-- notification_overrides: full CRUD on own rows
create policy "Users can view their notification overrides"
    on public.notification_overrides for select
    using (auth.uid() = user_id);

create policy "Users can insert their notification overrides"
    on public.notification_overrides for insert
    with check (auth.uid() = user_id);

create policy "Users can update their notification overrides"
    on public.notification_overrides for update
    using (auth.uid() = user_id);

create policy "Users can delete their notification overrides"
    on public.notification_overrides for delete
    using (auth.uid() = user_id);

-- notification_delivery_log: users can read their own log (transparency).
-- All writes go through service_role.
create policy "Users can view their notification delivery log"
    on public.notification_delivery_log for select
    using (auth.uid() = user_id);


-- Grants
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_settings" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_settings" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_settings" to "service_role";

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_overrides" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_overrides" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_overrides" to "service_role";

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_delivery_log" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_delivery_log" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."notification_delivery_log" to "service_role";
