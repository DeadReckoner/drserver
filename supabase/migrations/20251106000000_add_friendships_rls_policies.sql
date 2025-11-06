-- Add RLS policies for friendships table
-- Allows users to manage their own friend requests and friendships

-- Policy: Users can view friendships where they are either requester or addressee
CREATE POLICY "Users can view their own friendships"
ON public.friendships
FOR SELECT
TO authenticated
USING (
  auth.uid() = requester_id OR auth.uid() = addressee_id
);

-- Policy: Users can create friend requests (as requester)
CREATE POLICY "Users can create friend requests"
ON public.friendships
FOR INSERT
TO authenticated
WITH CHECK (
  auth.uid() = requester_id
);

-- Policy: Users can update friendships where they are the addressee (to accept/reject)
-- or where they are involved (to update restrictions)
CREATE POLICY "Users can update their friendships"
ON public.friendships
FOR UPDATE
TO authenticated
USING (
  auth.uid() = requester_id OR auth.uid() = addressee_id
)
WITH CHECK (
  auth.uid() = requester_id OR auth.uid() = addressee_id
);

-- Policy: Users can delete friendships where they are involved
CREATE POLICY "Users can delete their friendships"
ON public.friendships
FOR DELETE
TO authenticated
USING (
  auth.uid() = requester_id OR auth.uid() = addressee_id
);
