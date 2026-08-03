-- Form-based goal discovery: generating the three candidate goals for a user
-- who does not yet know what to pursue runs as a background job, so job_type
-- needs a fourth value. The roadmap that flow eventually produces is still a
-- normal 'roadmap_generation' job — this value covers only the options step.
--
-- ALTER TYPE ... ADD VALUE cannot be used in the same transaction that adds it
-- on PostgreSQL, so this lives in its own migration with nothing else in it.
ALTER TYPE job_type ADD VALUE IF NOT EXISTS 'goal_discovery';
