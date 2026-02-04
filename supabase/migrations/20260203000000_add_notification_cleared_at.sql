-- Add cleared_at column to notifications table
ALTER TABLE notifications ADD COLUMN cleared_at TIMESTAMPTZ;
