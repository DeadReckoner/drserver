-- Add habit anchor support to anchor_requests table
-- This makes anchor_requests polymorphic to support both roadmaps and habits

-- Step 1: Add new columns to anchor_requests
ALTER TABLE "public"."anchor_requests"
ADD COLUMN "target_type" text NOT NULL DEFAULT 'roadmap',
ADD COLUMN "habit_id" uuid;

-- Step 2: Make roadmap_id nullable (existing data has roadmap_id, so this is safe)
ALTER TABLE "public"."anchor_requests"
ALTER COLUMN "roadmap_id" DROP NOT NULL;

-- Step 3: Add foreign key for habit_id
ALTER TABLE "public"."anchor_requests"
ADD CONSTRAINT anchor_requests_habit_id_fkey
FOREIGN KEY (habit_id) REFERENCES habits(habit_id) ON UPDATE CASCADE ON DELETE CASCADE;

-- Step 4: Add check constraint for target_type values
ALTER TABLE "public"."anchor_requests"
ADD CONSTRAINT anchor_requests_target_type_check
CHECK (target_type IN ('roadmap', 'habit'));

-- Step 5: Add check constraint to ensure exactly one of roadmap_id/habit_id is set based on target_type
ALTER TABLE "public"."anchor_requests"
ADD CONSTRAINT anchor_requests_target_consistency_check
CHECK (
    (target_type = 'roadmap' AND roadmap_id IS NOT NULL AND habit_id IS NULL) OR
    (target_type = 'habit' AND habit_id IS NOT NULL AND roadmap_id IS NULL)
);

-- Step 6: Create index for habit_id lookups
CREATE INDEX anchor_requests_habit_id_idx ON public.anchor_requests USING btree (habit_id);

-- Step 7: Create unique constraint to prevent duplicate pending habit anchor requests
CREATE UNIQUE INDEX anchor_requests_unique_pending_habit_idx
ON public.anchor_requests (habit_id, anchor_id)
WHERE status = 'pending' AND target_type = 'habit';

-- Step 8: Add anchor_id column to habits table
ALTER TABLE "public"."habits"
ADD COLUMN "anchor_id" uuid;

-- Step 9: Add foreign key for habits.anchor_id
ALTER TABLE "public"."habits"
ADD CONSTRAINT habits_anchor_id_fkey
FOREIGN KEY (anchor_id) REFERENCES users(user_id) ON UPDATE CASCADE ON DELETE SET NULL;

-- Step 10: Create index for anchor lookups on habits
CREATE INDEX habits_anchor_id_idx ON public.habits USING btree (anchor_id);

-- Step 11: Add RLS policy for anchors to view habits they anchor
CREATE POLICY "Anchors can view habits they anchor"
ON public.habits FOR SELECT
USING (auth.uid()::text = anchor_id::text);
