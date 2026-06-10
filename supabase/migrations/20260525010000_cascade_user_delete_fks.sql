-- Allow users to be deleted by cascading dependent rows.
-- The original FKs in 20250901145225_remote_schema.sql were created without an
-- ON DELETE clause, so they default to NO ACTION and block account deletion as
-- soon as a friendship, message, or anchored roadmap exists. This migration
-- swaps them for CASCADE (and SET NULL for roadmaps.anchor_id, since losing an
-- anchor must not delete the addressee's own roadmap).

ALTER TABLE public.friendships
    DROP CONSTRAINT IF EXISTS friendships_requester_id_fkey;
ALTER TABLE public.friendships
    ADD CONSTRAINT friendships_requester_id_fkey
    FOREIGN KEY (requester_id) REFERENCES public.users(user_id) ON DELETE CASCADE;

ALTER TABLE public.friendships
    DROP CONSTRAINT IF EXISTS friendships_addressee_id_fkey;
ALTER TABLE public.friendships
    ADD CONSTRAINT friendships_addressee_id_fkey
    FOREIGN KEY (addressee_id) REFERENCES public.users(user_id) ON DELETE CASCADE;

ALTER TABLE public.messages
    DROP CONSTRAINT IF EXISTS messages_user_id_fkey;
ALTER TABLE public.messages
    ADD CONSTRAINT messages_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE CASCADE;

ALTER TABLE public.roadmaps
    DROP CONSTRAINT IF EXISTS roadmaps_anchor_id_fkey;
ALTER TABLE public.roadmaps
    ADD CONSTRAINT roadmaps_anchor_id_fkey
    FOREIGN KEY (anchor_id) REFERENCES public.users(user_id) ON DELETE SET NULL;
