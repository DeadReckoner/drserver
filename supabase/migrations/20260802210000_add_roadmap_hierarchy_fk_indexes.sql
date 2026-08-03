-- Foreign-key indexes for the Roadmap -> Milestone -> Task -> Step hierarchy.
--
-- Postgres indexes primary keys automatically but NOT foreign keys, and these
-- four tables predate the convention the later tables follow (phases, spikes,
-- risks, habits and notifications all got explicit FK indexes). Until now every
-- "children of X" lookup — which is most of the read traffic on this hierarchy,
-- including every GORM Preload — planned as a sequential scan whose cost grows
-- with the total row count of the table rather than with the rows the caller
-- actually wants.
--
-- Column order in the composite indexes puts the FK first so they also serve
-- the plain "WHERE <fk> = ?" lookups; the trailing column covers the filtered
-- variants in the database package (steps by status/completed, tasks by
-- completed for the overdue and cascade checks).
--
-- These are written as plain CREATE INDEX to match the rest of the migrations
-- and because the Supabase CLI runs each migration in a transaction, which
-- CONCURRENTLY cannot join. Each one takes a brief ACCESS EXCLUSIVE lock on
-- write. If these tables have grown enough for that pause to matter, run the
-- CONCURRENTLY form by hand against the database first — the IF NOT EXISTS
-- guards then make this migration a no-op.

-- steps: read on every task detail fetch, and three times per step update.
CREATE INDEX IF NOT EXISTS steps_task_id_idx ON public.steps (task_id);
CREATE INDEX IF NOT EXISTS steps_task_status_idx ON public.steps (task_id, status);

-- tasks: preloaded per milestone, scanned per roadmap, listed per user.
CREATE INDEX IF NOT EXISTS tasks_milestone_id_idx ON public.tasks (milestone_id);
CREATE INDEX IF NOT EXISTS tasks_roadmap_id_idx ON public.tasks (roadmap_id);
CREATE INDEX IF NOT EXISTS tasks_milestone_completed_idx ON public.tasks (milestone_id, completed);
CREATE INDEX IF NOT EXISTS tasks_user_completed_date_idx ON public.tasks (user_id, completed, date);

-- milestones: preloaded per roadmap; the phase_id index already exists.
CREATE INDEX IF NOT EXISTS milestones_roadmap_id_idx ON public.milestones (roadmap_id);
CREATE INDEX IF NOT EXISTS milestones_roadmap_reached_idx ON public.milestones (roadmap_id, reached);
CREATE INDEX IF NOT EXISTS milestones_user_id_idx ON public.milestones (user_id);

ANALYZE public.steps;
ANALYZE public.tasks;
ANALYZE public.milestones;
