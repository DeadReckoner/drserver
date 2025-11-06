-- Fix friendships table to properly support friend request CRUD operations
-- This migration adds constraints and indexes to ensure data integrity and performance

-- 1. Add unique constraint to prevent duplicate friend requests
-- Ensures the same pair of users can't have multiple pending/active requests
CREATE UNIQUE INDEX friendships_unique_pair ON public.friendships
USING btree (LEAST(requester_id, addressee_id), GREATEST(requester_id, addressee_id))
WHERE status IN ('pending', 'accepted');

-- 2. Add check constraint to validate status values
-- Ensures only valid status values can be stored
ALTER TABLE public.friendships
ADD CONSTRAINT friendships_status_check
CHECK (status IN ('pending', 'accepted', 'rejected', 'blocked'));

-- 3. Add indexes for efficient querying
-- These dramatically improve query performance when looking up friendships by user

-- Index for finding all friendships where user is the requester
CREATE INDEX idx_friendships_requester_status ON public.friendships
USING btree (requester_id, status);

-- Index for finding all friendships where user is the addressee
CREATE INDEX idx_friendships_addressee_status ON public.friendships
USING btree (addressee_id, status);

-- Composite index for querying accepted friendships (most common query pattern)
CREATE INDEX idx_friendships_accepted ON public.friendships
USING btree (status)
WHERE status = 'accepted';

-- 4. Add index on created_at for ordering results chronologically
CREATE INDEX idx_friendships_created_at ON public.friendships
USING btree (created_at DESC);
