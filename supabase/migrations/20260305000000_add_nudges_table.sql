-- Create nudges table for accountability partner nudging feature
-- Nudges are lightweight "hey, I'm watching" signals from anchors to roadmap/habit owners

CREATE TABLE IF NOT EXISTS nudges (
    nudge_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_id UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    receiver_id UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    target_type VARCHAR(20) NOT NULL CHECK (target_type IN ('roadmap', 'habit')),
    target_id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for rate limit checks: find nudges by sender for a specific target in last 24h
CREATE INDEX idx_nudges_rate_limit ON nudges (sender_id, target_type, target_id, created_at DESC);

-- Index for viewing received nudges
CREATE INDEX idx_nudges_receiver ON nudges (receiver_id, created_at DESC);

-- Index for viewing sent nudges
CREATE INDEX idx_nudges_sender ON nudges (sender_id, created_at DESC);

-- Enable RLS
ALTER TABLE nudges ENABLE ROW LEVEL SECURITY;

-- Policy: Users can see nudges they sent or received
CREATE POLICY "Users can view their own nudges"
    ON nudges
    FOR SELECT
    USING (
        auth.uid() = sender_id OR auth.uid() = receiver_id
    );

-- Policy: Users can create nudges (validation of anchor relationship is done in application)
CREATE POLICY "Users can create nudges"
    ON nudges
    FOR INSERT
    WITH CHECK (
        auth.uid() = sender_id
    );

-- Policy: Users cannot update nudges (nudges are immutable)
-- No UPDATE policy needed

-- Policy: Users can delete their own sent nudges (optional, for cleanup)
CREATE POLICY "Users can delete nudges they sent"
    ON nudges
    FOR DELETE
    USING (
        auth.uid() = sender_id
    );

COMMENT ON TABLE nudges IS 'Accountability nudges from anchors to roadmap/habit owners. Rate limited to 2 per day per target.';
COMMENT ON COLUMN nudges.sender_id IS 'The anchor (accountability partner) sending the nudge';
COMMENT ON COLUMN nudges.receiver_id IS 'The roadmap/habit owner receiving the nudge';
COMMENT ON COLUMN nudges.target_type IS 'Type of target: roadmap or habit';
COMMENT ON COLUMN nudges.target_id IS 'UUID of the roadmap or habit being nudged about';
