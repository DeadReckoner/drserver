-- Fix left_at values in thread_participants table
-- Set zero time values to NULL for proper querying

UPDATE public.thread_participants
SET left_at = NULL
WHERE left_at = '0001-01-01 00:00:00+00'::timestamptz;

-- Also fix any messages that have zero time for deleted_at
UPDATE public.messages
SET deleted_at = NULL
WHERE deleted_at = '0001-01-01 00:00:00+00'::timestamptz;

-- Add comment
COMMENT ON COLUMN public.thread_participants.left_at IS 'Timestamp when user left the thread (NULL if still active)';
COMMENT ON COLUMN public.messages.deleted_at IS 'Timestamp when message was deleted (NULL if not deleted)';
