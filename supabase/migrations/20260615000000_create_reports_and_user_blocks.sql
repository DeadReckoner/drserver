-- =============================================================================
-- Reports + user blocks
-- =============================================================================
-- Two tables backing the UGC safety feature (App Store Guideline 1.2):
--
--   reports      - a single, type-discriminated moderation record. type='user'
--                  reports another member (optionally a specific message);
--                  type='ai' reports AI-generated content (roadmap, milestone,
--                  task, habit suggestion, revision, accountability message...).
--                  Written by the backend on POST /api/v1/reports; also emailed
--                  to the support address. The row is the durable artifact even
--                  if the email send fails.
--
--   user_blocks  - a directional block (blocker_id -> blocked_id). Created by
--                  "Remove & Block". Enforced at the friend-request and
--                  message-send gates. Only the blocker can lift it (and then
--                  re-invite); the blocked user cannot.
--
-- Both tables are INTERNAL: RLS is enabled with NO policies so anon/authenticated
-- roles are default-deny (mirrors email_log / entitlement_events). The Go backend
-- connects via DATABASE_URL and is unaffected by RLS.

create table if not exists "public"."reports" (
    "report_id"        uuid not null default gen_random_uuid(),

    -- The member who filed the report. Nullable + ON DELETE SET NULL so the
    -- moderation record (and its context snapshot) survives account deletion.
    "reporter_id"      uuid,

    "type"             text not null,
    "reason"           text not null,
    "note"             text,
    "status"           text not null default 'open',

    -- type='user' fields. reported_user_id is ON DELETE SET NULL so a report
    -- about an abusive user outlives that user deleting their account.
    "reported_user_id" uuid,
    "thread_id"        uuid,
    "message_id"       uuid,

    -- type='ai' fields. No FKs: output_id is polymorphic across surfaces and
    -- the referenced content may be an unsaved preview, so the substance lives
    -- in the context snapshot below rather than a foreign-key join.
    "output_type"      text,
    "output_id"        uuid,
    "roadmap_id"       uuid,
    "job_id"           uuid,

    -- Snapshot of the reported content (AI output text, message body, etc.)
    -- captured at report time, plus optional client metadata. This is what
    -- makes a report actionable when the underlying entity is gone or was
    -- never persisted.
    "context"          jsonb,
    "app_version"      text,
    "os"               text,
    "device"           text,

    "created_at"       timestamp with time zone not null default now(),
    "updated_at"       timestamp with time zone,

    constraint reports_pkey primary key (report_id),
    constraint reports_type_check check (type in ('user', 'ai')),
    constraint reports_status_check check (status in ('open', 'reviewing', 'actioned', 'dismissed')),
    constraint reports_reporter_id_fkey
        foreign key (reporter_id) references users(user_id) on update cascade on delete set null,
    constraint reports_reported_user_id_fkey
        foreign key (reported_user_id) references users(user_id) on update cascade on delete set null
);

-- Admin triage queries by status + recency; rate-limiting queries by reporter.
create index if not exists reports_status_created
    on public.reports (status, created_at);
create index if not exists reports_reporter_created
    on public.reports (reporter_id, created_at);
create index if not exists reports_reported_user
    on public.reports (reported_user_id) where reported_user_id is not null;

alter table "public"."reports" enable row level security;

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."reports" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."reports" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."reports" to "service_role";

comment on table public.reports is 'User and AI-content moderation reports (App Store UGC Guideline 1.2). Type-discriminated single table; emailed to support and retained for review.';
comment on column public.reports.reporter_id is 'Member who filed the report; SET NULL on account deletion to preserve the record.';
comment on column public.reports.type is 'Discriminator: user (report a member/message) or ai (report AI-generated content).';
comment on column public.reports.context is 'Snapshot of the reported content captured at report time; durable even if the source is unsaved or deleted.';

create table if not exists "public"."user_blocks" (
    "block_id"   uuid not null default gen_random_uuid(),
    "blocker_id" uuid not null,
    "blocked_id" uuid not null,
    "created_at" timestamp with time zone not null default now(),

    constraint user_blocks_pkey primary key (block_id),
    constraint user_blocks_not_self check (blocker_id <> blocked_id),
    constraint user_blocks_uniq unique (blocker_id, blocked_id),
    constraint user_blocks_blocker_id_fkey
        foreign key (blocker_id) references users(user_id) on update cascade on delete cascade,
    constraint user_blocks_blocked_id_fkey
        foreign key (blocked_id) references users(user_id) on update cascade on delete cascade
);

-- Enforcement checks "is there a block in either direction"; the unique index
-- covers blocker_id, this covers the reverse lookup on blocked_id.
create index if not exists user_blocks_blocked
    on public.user_blocks (blocked_id);

alter table "public"."user_blocks" enable row level security;

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."user_blocks" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."user_blocks" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."user_blocks" to "service_role";

comment on table public.user_blocks is 'Directional user blocks (blocker_id -> blocked_id). Created by Remove & Block; only the blocker can lift it.';
