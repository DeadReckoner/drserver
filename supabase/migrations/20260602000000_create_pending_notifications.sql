-- =============================================================================
-- Pending notifications queue
-- =============================================================================
-- Holds notifications that the resolver said to DEFER (e.g. quiet hours
-- active for an immediate-delivery push). The DeliverPendingNotifications
-- cron job picks rows where scheduled_for <= now() and re-runs Dispatch on
-- them; if the resolver now says send, the push goes out and the row is
-- deleted. If it says snooze again (e.g. user is still in quiet hours),
-- the row's scheduled_for is updated to the new target via UPSERT.
--
-- The unique constraint on (user_id, notification_type, dedup_key, channel)
-- matches the shape on notification_delivery_log. This means duplicate
-- triggers during a deferral don't pile up — they update the existing
-- row's scheduled_for rather than inserting a second.

create table "public"."pending_notifications" (
    "id"                uuid not null default gen_random_uuid(),
    "user_id"           uuid not null,

    "notification_type" text not null,
    "dedup_key"         text not null,
    "channel"           text not null,

    -- The full DispatchOptions payload, serialized for re-evaluation when
    -- the cron picks this row up. Title/body might be templated to current
    -- state at delivery time in a future iteration; for now we send what
    -- was captured at queue time.
    "category"            text not null,
    "friend_id"           uuid,
    "anchored_roadmap_id" uuid,
    "title"               text not null,
    "body"                text not null,
    "data"                jsonb not null default '{}'::jsonb,

    -- When the cron should try to deliver. Set to the resolver's
    -- SnoozeUntil at insert; updated on each re-snooze.
    "scheduled_for"     timestamp with time zone not null,

    -- Soft attempt counter for observability + infinite-loop backstop.
    -- The cron drops rows whose attempts exceed a threshold (defined in
    -- Go) to prevent a misconfigured TZ or stuck setting from holding a
    -- row forever.
    "attempts"          integer not null default 0,
    "created_at"        timestamp with time zone not null default now(),
    "updated_at"        timestamp with time zone not null default now(),

    constraint pending_notifications_pkey primary key (id),
    constraint pending_notifications_user_id_fkey
        foreign key (user_id) references users(user_id) on update cascade on delete cascade,
    constraint pending_notifications_channel_check
        check (channel in ('push', 'email')),
    constraint pending_notifications_category_check
        check (category in ('my_goals', 'f_and_a')),
    constraint pending_notifications_uniq
        unique (user_id, notification_type, dedup_key, channel)
);

-- The cron scans by scheduled_for, so index it. Includes user_id second
-- so per-user dashboards (if added later) stay snappy.
create index pending_notifications_scheduled_for
    on public.pending_notifications (scheduled_for, user_id);

-- RLS + grants (matches the notification_delivery_log pattern from Step 2)
alter table "public"."pending_notifications" enable row level security;

create policy "Users can view their pending notifications"
    on public.pending_notifications for select
    using (auth.uid() = user_id);

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."pending_notifications" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."pending_notifications" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."pending_notifications" to "service_role";
