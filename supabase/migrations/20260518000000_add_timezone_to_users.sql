-- Add IANA timezone column to users for server-side scheduling
-- (quiet hours, 9am-local anchor batches, daily/weekly digest delivery).
--
-- Default 'UTC' is safe: any timezone-aware query that joins on this column
-- can omit COALESCE because the column is NOT NULL. Mobile clients are
-- expected to PATCH /users/:id on auth with the device's IANA TZ
-- (e.g. 'America/Los_Angeles').
ALTER TABLE users
  ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';
