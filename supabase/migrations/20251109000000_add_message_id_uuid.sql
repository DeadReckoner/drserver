-- Migrate messages table from bigint id to UUID message_id as primary key
-- This aligns the database schema with the Go types

-- Step 1: Add message_id UUID column
ALTER TABLE public.messages
ADD COLUMN message_id UUID DEFAULT gen_random_uuid() NOT NULL;

-- Step 2: Drop the old primary key constraint
ALTER TABLE public.messages
DROP CONSTRAINT IF EXISTS messages_pkey;

-- Step 3: Drop the old index
DROP INDEX IF EXISTS messages_pkey;

-- Step 4: Make message_id the new primary key
ALTER TABLE public.messages
ADD CONSTRAINT messages_pkey PRIMARY KEY (message_id);

-- Step 5: Drop the old bigint id column (no longer needed)
ALTER TABLE public.messages
DROP COLUMN IF EXISTS id;

-- Add comment
COMMENT ON COLUMN public.messages.message_id IS 'UUID primary key for messages';
