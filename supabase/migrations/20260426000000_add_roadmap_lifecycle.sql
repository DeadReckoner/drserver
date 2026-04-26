-- Roadmap lifecycle fields
ALTER TABLE "public"."roadmaps" ADD COLUMN IF NOT EXISTS "archived" boolean NOT NULL DEFAULT false;
ALTER TABLE "public"."roadmaps" ADD COLUMN IF NOT EXISTS "paused_at" timestamp with time zone;
ALTER TABLE "public"."roadmaps" ADD COLUMN IF NOT EXISTS "share_token" text UNIQUE;

-- Indexes for efficient lookups
CREATE INDEX IF NOT EXISTS roadmaps_user_archived_idx ON public.roadmaps (user_id, archived);
CREATE INDEX IF NOT EXISTS roadmaps_share_token_idx ON public.roadmaps (share_token) WHERE share_token IS NOT NULL;

-- Step status field
ALTER TABLE "public"."steps" ADD COLUMN IF NOT EXISTS "status" text NOT NULL DEFAULT 'incomplete';

-- Migrate existing completed steps to 'completed' status
UPDATE "public"."steps" SET status = 'completed' WHERE completed = true AND status = 'incomplete';

-- Add constraint for valid status values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'steps_valid_status'
    ) THEN
        ALTER TABLE "public"."steps" ADD CONSTRAINT steps_valid_status
            CHECK (status IN ('incomplete', 'completed', 'skipped'));
    END IF;
END $$;
