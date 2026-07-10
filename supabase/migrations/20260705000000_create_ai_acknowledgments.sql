-- =============================================================================
-- AI acknowledgments
-- =============================================================================
-- One row per (user, acknowledgment version): the user's acceptance of the
-- "AI-assisted planning is not professional advice" acknowledgment
-- (drserver#65). The backend refuses AI roadmap generation until the row for
-- the current version (types.CurrentAIAckVersion) exists. Bumping that
-- version forces re-acceptance; older rows are kept as an audit trail of
-- which copy the user agreed to and when.
--
-- INTERNAL table: RLS is enabled with NO policies so anon/authenticated roles
-- are default-deny (mirrors reports / email_log). The Go backend connects via
-- DATABASE_URL and is unaffected by RLS.

create table if not exists "public"."ai_acknowledgments" (
    "user_id"     uuid not null,
    "version"     integer not null,
    "app_version" text not null default '',
    "accepted_at" timestamptz not null default now(),

    constraint ai_acknowledgments_pkey primary key ("user_id", "version"),
    constraint ai_acknowledgments_user_id_fkey
        foreign key (user_id) references users(user_id)
        on update cascade on delete cascade
);

alter table "public"."ai_acknowledgments" enable row level security;

grant select, insert, update, delete, references, trigger, truncate
    on table "public"."ai_acknowledgments" to "anon";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."ai_acknowledgments" to "authenticated";
grant select, insert, update, delete, references, trigger, truncate
    on table "public"."ai_acknowledgments" to "service_role";

comment on table public.ai_acknowledgments is 'Per-version acceptance of the AI & professional advice acknowledgment; AI generation is blocked until the current version is accepted.';
comment on column public.ai_acknowledgments.version is 'Acknowledgment copy version (types.CurrentAIAckVersion at acceptance time). New versions require re-acceptance.';
comment on column public.ai_acknowledgments.app_version is 'Mobile app version that displayed the acknowledgment, as reported by the client.';
