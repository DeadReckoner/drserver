-- One-off data fix: convert milestone-relative task days to roadmap-global ones.
--
-- Deliberately NOT under supabase/migrations, so `supabase db push` won't run
-- it. Run it by hand against one environment at a time, after the server
-- change that stops writing relative days has been deployed:
--
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f supabase/scripts/globalize_task_days.sql
--
-- Background: the AI next-milestone flow (database.SaveNextMilestone) used to
-- store each task's Day as the model returned it, an offset from its
-- milestone's first day (0, 1, 2, ...). Plan import stores roadmap-global
-- days, and restart/start re-dates tasks from Day, so the relative rows all
-- landed in the first couple of weeks of the plan on restart. The server now
-- writes global days and dates legacy rows from their milestone, so this
-- script only tidies the stored data; nothing breaks if it never runs.
--
-- Detection is the same rule as types.TaskScheduleOffset: a global task can't
-- sit before its milestone, so a milestone whose minimum task day is below
-- its own day holds relative days. Those are rebased to
--   milestone.day + (task.day - min task day)
-- which is the day the server already schedules them on. A milestone at day 0
-- is ambiguous but needs no change (relative and global coincide).
--
-- Idempotent: after one run every converted milestone's minimum task day
-- equals its own day, so a second run matches nothing.

BEGIN;

CREATE TEMP TABLE task_day_fix ON COMMIT DROP AS
SELECT t.task_id,
       t.milestone_id,
       t.day                          AS old_day,
       m.day + (t.day - s.min_day)    AS new_day
FROM tasks AS t
JOIN milestones AS m ON m.milestone_id = t.milestone_id
JOIN (
    SELECT milestone_id, MIN(day) AS min_day
    FROM tasks
    GROUP BY milestone_id
) AS s ON s.milestone_id = t.milestone_id
WHERE s.min_day < m.day;

-- Preview: how much will change, and a sample to eyeball.
SELECT COUNT(*)                     AS tasks_to_update,
       COUNT(DISTINCT milestone_id) AS milestones_affected
FROM task_day_fix;

SELECT * FROM task_day_fix ORDER BY milestone_id, old_day LIMIT 20;

UPDATE tasks AS t
SET day = f.new_day
FROM task_day_fix AS f
WHERE t.task_id = f.task_id;

-- Swap to COMMIT once the preview looks right.
ROLLBACK;
