-- Safe migration to fix messages table structure
-- Handles all possible current states

-- Step 1: Add message_id column if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
        AND table_name = 'messages'
        AND column_name = 'message_id'
    ) THEN
        ALTER TABLE public.messages
        ADD COLUMN message_id UUID DEFAULT gen_random_uuid() NOT NULL;
    END IF;
END $$;

-- Step 2: Drop old primary key if it exists
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'messages_pkey'
        AND table_name = 'messages'
        AND constraint_type = 'PRIMARY KEY'
    ) THEN
        ALTER TABLE public.messages DROP CONSTRAINT messages_pkey;
    END IF;
END $$;

-- Step 3: Drop old id column if it exists
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
        AND table_name = 'messages'
        AND column_name = 'id'
    ) THEN
        ALTER TABLE public.messages DROP COLUMN id;
    END IF;
END $$;

-- Step 4: Create new primary key on message_id
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'messages_pkey'
        AND table_name = 'messages'
    ) THEN
        ALTER TABLE public.messages ADD CONSTRAINT messages_pkey PRIMARY KEY (message_id);
    END IF;
END $$;

-- Verify the result
COMMENT ON COLUMN public.messages.message_id IS 'UUID primary key for messages';
