-- entitlement_events: append-only audit + idempotency log for entitlement
-- changes driven by external webhooks (RevenueCat today; Apple/Google direct
-- or Stripe direct later). Every successful webhook apply records exactly one
-- row here so that retries of the same event are no-ops (PK conflict on
-- event_id) and so we have raw payloads available for debugging without
-- re-querying the provider.

CREATE TABLE IF NOT EXISTS "public"."entitlement_events" (
    -- The provider's unique event ID. Used as the idempotency key. RC's
    -- "id" field on the outer event envelope; Apple's notificationUUID;
    -- Stripe's event.id. Must be unique per logical occurrence.
    event_id        TEXT PRIMARY KEY,

    -- The DR user the event applies to. Nullable because RC can occasionally
    -- emit events (e.g. SUBSCRIBER_ALIAS, TRANSFER) where we may not have
    -- resolved the user yet; we still record them for audit even when we
    -- could not apply.
    user_id         UUID REFERENCES "public"."users"(user_id) ON DELETE CASCADE,

    -- Provenance.
    provider        TEXT NOT NULL,            -- 'revenuecat' for now; reserves room for direct-Apple/etc.
    store           TEXT,                     -- 'app_store' | 'play_store' | 'stripe' | 'promotional'
    event_type      TEXT NOT NULL,            -- INITIAL_PURCHASE, RENEWAL, ...
    product_id      TEXT,

    -- The provider's event timestamp (ms since epoch in RC's payload). Used
    -- for stale-event detection: if a CANCELLATION arrives with a timestamp
    -- older than the entitlements.updated_at the apply path already recorded,
    -- we skip applying it.
    event_timestamp_ms BIGINT NOT NULL,

    -- When we received it (server clock).
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Full raw payload as we received it. Useful for replaying a failed apply
    -- or reconstructing what happened after the fact.
    payload         JSONB NOT NULL,

    -- Outcome of the apply attempt - free-form for now. Values we use today:
    --   'applied'        - entitlements row was updated
    --   'noop_stale'     - skipped because event_timestamp_ms < entitlements.updated_at
    --   'noop_unknown'   - event_type we don't handle yet (still recorded for audit)
    --   'noop_unmapped'  - user_id could not be resolved from RC payload
    --   'error'          - apply path errored; row is still recorded so we know it happened
    apply_status    TEXT NOT NULL DEFAULT 'applied',
    apply_error     TEXT
);

CREATE INDEX IF NOT EXISTS entitlement_events_user_id_idx
    ON "public"."entitlement_events" (user_id);

CREATE INDEX IF NOT EXISTS entitlement_events_received_at_idx
    ON "public"."entitlement_events" (received_at DESC);

CREATE INDEX IF NOT EXISTS entitlement_events_event_type_idx
    ON "public"."entitlement_events" (event_type);

-- RLS: webhook writes happen as the service role and the table is internal,
-- so no user-facing SELECT policy. Enable RLS so default-deny applies for
-- anyone touching the table through the anon/authenticated roles.
ALTER TABLE "public"."entitlement_events" ENABLE ROW LEVEL SECURITY;
