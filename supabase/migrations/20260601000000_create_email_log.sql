-- email_log: append-only audit + dedupe table for transactional emails sent
-- by the backend through Resend. Every send attempt (success or failure)
-- gets a row so we can:
--   * detect duplicates (sent same kind to same user already?)
--   * debug failures (Resend returned an error? which one?)
--   * audit user-facing comms (e.g. for support tickets, GDPR exports)

CREATE TABLE IF NOT EXISTS "public"."email_log" (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES "public"."users"(user_id) ON DELETE CASCADE,

    -- Kind identifies which template was rendered. See email.EmailKind* in
    -- the Go code for the enumeration. Free-form TEXT so adding a new kind
    -- in code does not require a migration.
    kind                TEXT NOT NULL,

    -- Recipient address at send time. Cached because the user's email could
    -- change later and we want to preserve what we actually sent to.
    to_email            TEXT NOT NULL,

    -- Outcome of the send attempt:
    --   'sent'     -> Resend accepted the payload, returned a message ID
    --   'error'    -> Resend or the network failed after all retries
    --   'skipped'  -> we deliberately did not send (e.g. dedupe hit)
    status              TEXT NOT NULL CHECK (status IN ('sent', 'error', 'skipped')),

    -- Resend's message ID for successful sends. Useful when correlating with
    -- Resend dashboard logs or webhook events. NULL on failure / skip.
    provider_message_id TEXT,

    -- Free-form error text. Populated on status='error' or 'skipped' (for
    -- skipped, holds the reason e.g. "already sent within 24h").
    error_text          TEXT,

    -- For dedupe: a stable key the dispatcher can re-derive to ask
    -- "have I already sent this exact thing?". For event-driven emails this
    -- is typically the entitlement_events.event_id; for the welcome email
    -- it is the user_id itself (one welcome per user, ever).
    dedupe_key          TEXT,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS email_log_user_id_idx
    ON "public"."email_log" (user_id);

CREATE INDEX IF NOT EXISTS email_log_kind_idx
    ON "public"."email_log" (kind);

-- Compound index for the dedupe lookup: "has user X already been sent kind
-- Y with dedupe_key Z?". Partial because rows without a dedupe_key cannot
-- collide.
CREATE INDEX IF NOT EXISTS email_log_dedupe_idx
    ON "public"."email_log" (user_id, kind, dedupe_key)
    WHERE dedupe_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS email_log_created_at_idx
    ON "public"."email_log" (created_at DESC);

-- RLS: internal table, no user-facing reads. Enable RLS so default-deny
-- applies for the anon/authenticated roles.
ALTER TABLE "public"."email_log" ENABLE ROW LEVEL SECURITY;
