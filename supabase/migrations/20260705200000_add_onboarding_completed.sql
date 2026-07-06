-- =============================================================================
-- users.onboarding_completed
-- =============================================================================
-- Persists onboarding completion server-side (drmobile#185): the mobile app
-- stored it locally, so logging out (or logging in on a new device) replayed
-- onboarding for existing accounts. The client sets it via the existing
-- PATCH /users/:id once onboarding finishes and reads it from GET /users on
-- login.

alter table "public"."users"
    add column if not exists "onboarding_completed" boolean not null default false;

comment on column public.users.onboarding_completed is 'True once the user has completed first-run onboarding; set by the client so onboarding is not replayed on re-login or new devices (drmobile#185).';
