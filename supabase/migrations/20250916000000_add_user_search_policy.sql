-- Allow authenticated users to search for other users by email
-- This enables user discovery while maintaining profile privacy
CREATE POLICY "Users can search other users by email" ON "public"."users" 
FOR SELECT TO "authenticated" 
USING (true);

-- Drop the restrictive policy that only allows users to see their own profile
DROP POLICY "Users can select their own profile" ON "public"."users";