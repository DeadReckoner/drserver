-- =============================================================================
-- Carry-over notification prefs: agent_updates (my-goals), nudges + messages (F&A)
-- =============================================================================
-- Wiring the last direct-push call sites through notifications.Dispatch requires
-- three new per-type toggles so the resolver's whitelist (MyGoalsPrefs.Lookup /
-- FAndAPrefs.Lookup) recognizes them instead of dropping as unknown types:
--
--   my_goals_prefs.agent_updates  — AI generation outcomes (roadmap + milestone,
--                                   success + failure all share this toggle)
--   f_and_a_prefs.nudges          — roadmap + habit nudges from a partner
--   f_and_a_prefs.messages        — new-message pushes
--
-- The JSONB DEFAULTs here MUST stay in sync with types.DefaultMyGoalsPrefs()
-- and types.DefaultFAndAPrefs(). Go's Scan() also merge-fills missing keys at
-- read time, so this migration is belt-and-suspenders (it covers direct SQL
-- inserts and keeps the stored shape current); neither layer can silently
-- opt an existing user out of a new type.

-- 1. New-row defaults: append the new keys to the column DEFAULTs.
alter table public.notification_settings
    alter column my_goals_prefs set default '{
        "daily_task_reminder":       {"enabled": true, "delivery": "immediate"},
        "task_due_reminder":         {"enabled": true, "delivery": "immediate"},
        "missed_task":               {"enabled": true, "delivery": "digest"},
        "falling_behind":            {"enabled": true, "delivery": "digest"},
        "milestone_due":             {"enabled": true, "delivery": "immediate"},
        "milestone_at_risk":         {"enabled": true, "delivery": "immediate"},
        "follow_on_milestone_ready": {"enabled": true, "delivery": "immediate"},
        "goal_milestone_completed":  {"enabled": true, "delivery": "immediate"},
        "agent_updates":             {"enabled": true, "delivery": "immediate"}
    }'::jsonb;

alter table public.notification_settings
    alter column f_and_a_prefs set default '{
        "friend_request":          {"enabled": true, "delivery": "immediate"},
        "anchor_request":          {"enabled": true, "delivery": "immediate"},
        "anchor_accepted_removed": {"enabled": true, "delivery": "immediate"},
        "missed_progress":         {"enabled": true, "delivery": "digest"},
        "falling_behind":          {"enabled": true, "delivery": "digest"},
        "milestone_at_risk":       {"enabled": true, "delivery": "immediate"},
        "milestone_completed":     {"enabled": true, "delivery": "digest"},
        "goal_completed":          {"enabled": true, "delivery": "immediate"},
        "timeline_updated":        {"enabled": true, "delivery": "digest"},
        "nudges":                  {"enabled": true, "delivery": "immediate"},
        "messages":                {"enabled": true, "delivery": "immediate"}
    }'::jsonb;

-- 2. Backfill existing rows that predate the new keys.
--    `defaults || stored` puts the existing value on the RIGHT, so any key the
--    user already has wins; only the genuinely-absent new key is added. The
--    WHERE guard keeps this idempotent and touches only rows that need it.
update public.notification_settings
set my_goals_prefs =
        '{"agent_updates": {"enabled": true, "delivery": "immediate"}}'::jsonb
        || my_goals_prefs
where not (my_goals_prefs ? 'agent_updates');

update public.notification_settings
set f_and_a_prefs =
        '{
            "nudges":   {"enabled": true, "delivery": "immediate"},
            "messages": {"enabled": true, "delivery": "immediate"}
        }'::jsonb
        || f_and_a_prefs
where not (f_and_a_prefs ? 'nudges')
   or not (f_and_a_prefs ? 'messages');
