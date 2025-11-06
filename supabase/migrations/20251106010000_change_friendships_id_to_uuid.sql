-- Change friendships table ID from bigint to UUID
-- This migration converts the primary key to use UUIDs for consistency with other tables

-- Step 1: Drop the existing primary key constraint
ALTER TABLE public.friendships DROP CONSTRAINT friendships_pkey;

-- Step 2: Drop the old id column
ALTER TABLE public.friendships DROP COLUMN id;

-- Step 3: Add new UUID id column with default value
ALTER TABLE public.friendships ADD COLUMN id uuid DEFAULT gen_random_uuid() NOT NULL;

-- Step 4: Set it as the primary key
ALTER TABLE public.friendships ADD CONSTRAINT friendships_pkey PRIMARY KEY (id);

-- Step 5: Create index on id for performance
CREATE INDEX idx_friendships_id ON public.friendships USING btree (id);
