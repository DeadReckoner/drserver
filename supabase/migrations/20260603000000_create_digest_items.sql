-- =============================================================================
-- Digest items queue
-- =============================================================================
-- Holds notifications whose resolver decision was "digest" delivery — these
-- are batched into the user's daily or weekly digest email instead of firing
-- as a push.
--
-- When notifications.Dispatch sees a delivery=digest decision, it INSERTs
-- here (with ON CONFLICT DO NOTHING) instead of calling SendPushNotification.
-- The DeliverDueDigests cron drains the table per-user when their digest
-- hour matches their TZ.
--
-- The unique constraint on (user_id, notification_type, dedup_key) mirrors
-- notification_delivery_log so duplicate scans during the digest window
-- don't produce duplicate items in the same email.

create table "public"."digest_items" (
    "id"                uuid not null default gen_random_uuid(),
    "user_id"           uuid not null,

    "notification_type" text not null,
    "dedup_key"         text not null,

    -- Captured at queue time so the email body can render without going
    -- back to the source tables. If the underlying state changes (e.g.
    -- task gets renamed) between queue and send, the user sees what was
    -- true when the digest item was queued.
    "title"             text not null,
    "body"              text not null,
    "data"              jsonb not null default '{}'::jsonb,

    "created_at"        timestamp with time zone not null default now(),

    constraint digest_items_pkey primary key (id),
    constraint digest_items_user_id_fkey
        foreign key (user_id) references users(user_id) on update cascade on delete cascade,
    constraint digest_items_uniq
        unique (user_id, notification_type, dedup_key)
);

-- Cron drains by user_id; index for the per-user query.
create index digest_items_user_id_created_at
    on public.digest_items (user_id, created_at);

alter table "public"."digest_items" enable row level security;

create policy "Users can view their digest items"
    on public.digest_items for select
    using (auth.uid() = user_id);

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."digest_items" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."digest_items" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."digest_items" to "service_role";
