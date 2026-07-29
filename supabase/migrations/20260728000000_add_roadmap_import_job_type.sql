-- Plan import (drserver): uploading an existing plan/roadmap PDF and having
-- Claude transpose it onto the Roadmap → Phase → Milestone → Task → Step model
-- runs as a background job, so job_type needs a third value.
--
-- ALTER TYPE ... ADD VALUE cannot be used in the same transaction that adds it
-- on PostgreSQL, so this lives in its own migration with nothing else in it.
ALTER TYPE job_type ADD VALUE IF NOT EXISTS 'roadmap_import';
