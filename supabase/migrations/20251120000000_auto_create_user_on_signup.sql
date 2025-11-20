-- Function to automatically create a user record when someone signs up
CREATE OR REPLACE FUNCTION public.handle_new_user()
RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO public.users (user_id, email, first_name, last_name, created_at)
  VALUES (
    NEW.id,
    NEW.email,
    NEW.raw_user_meta_data->>'firstName',
    NEW.raw_user_meta_data->>'lastName',
    NOW()
  )
  ON CONFLICT (user_id) DO NOTHING;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Trigger to call the function when a new user is created in auth.users
CREATE TRIGGER on_auth_user_created
  AFTER INSERT ON auth.users
  FOR EACH ROW EXECUTE FUNCTION public.handle_new_user();

-- Backfill existing auth users that don't have a profile
INSERT INTO public.users (user_id, email, first_name, last_name, created_at)
SELECT
  au.id,
  au.email,
  au.raw_user_meta_data->>'firstName',
  au.raw_user_meta_data->>'lastName',
  au.created_at
FROM auth.users au
LEFT JOIN public.users u ON au.id = u.user_id
WHERE u.user_id IS NULL
ON CONFLICT (user_id) DO NOTHING;
