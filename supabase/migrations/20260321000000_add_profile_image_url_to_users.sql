-- Add profile_image_url column to users table
ALTER TABLE users ADD COLUMN profile_image_url TEXT;

-- Create avatars storage bucket (public for profile images)
INSERT INTO storage.buckets (id, name, public)
VALUES ('avatars', 'avatars', true)
ON CONFLICT (id) DO NOTHING;

-- RLS policies are in 20260321010000_add_avatar_rls_policies.sql
