-- Fix JSONB columns that store JSON strings (should be TEXT)
-- OpenAI returns these fields as JSON strings, not JSON objects
-- GORM can't insert Go strings into JSONB columns without serialization

-- Change tasks.dependencies from jsonb to text
ALTER TABLE "public"."tasks"
    ALTER COLUMN "dependencies" TYPE text USING dependencies::text;

-- Change macro_milestones.resources_forecast from jsonb to text
ALTER TABLE "public"."macro_milestones"
    ALTER COLUMN "resources_forecast" TYPE text USING resources_forecast::text;
