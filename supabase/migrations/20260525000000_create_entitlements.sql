-- Entitlements table: one row per user, holds plan/subscription state and usage counters.
-- Step 1 of the Free/Paid plan rollout. Backend foundation only - no enforcement yet.

CREATE TABLE IF NOT EXISTS "public"."entitlements" (
    "user_id"                     uuid PRIMARY KEY REFERENCES "public"."users"("user_id") ON DELETE CASCADE,
    "plan"                        text NOT NULL DEFAULT 'free',
    "source"                      text NOT NULL DEFAULT 'default',

    -- Subscription state (null for default/free users)
    "status"                      text,
    "current_period_start"        timestamp with time zone,
    "current_period_end"          timestamp with time zone,
    "trial_ends_at"               timestamp with time zone,
    "cancelled_at"                timestamp with time zone,

    -- Provider linkage
    "provider_customer_id"        text,
    "provider_subscription_id"    text,
    "product_id"                  text,

    -- Usage counters (reset by app logic on calendar month boundary)
    "ai_generations_used"         integer NOT NULL DEFAULT 0,
    "ai_generations_period_start" timestamp with time zone NOT NULL DEFAULT now(),

    -- Audit
    "created_at"                  timestamp with time zone NOT NULL DEFAULT now(),
    "updated_at"                  timestamp with time zone NOT NULL DEFAULT now()
);

-- Validate enum values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'entitlements_valid_plan'
    ) THEN
        ALTER TABLE "public"."entitlements" ADD CONSTRAINT entitlements_valid_plan
            CHECK (plan IN ('free', 'paid'));
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'entitlements_valid_source'
    ) THEN
        ALTER TABLE "public"."entitlements" ADD CONSTRAINT entitlements_valid_source
            CHECK (source IN ('default', 'apple', 'google', 'stripe', 'comp'));
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'entitlements_valid_status'
    ) THEN
        ALTER TABLE "public"."entitlements" ADD CONSTRAINT entitlements_valid_status
            CHECK (status IS NULL OR status IN ('active', 'trialing', 'in_grace', 'cancelled', 'expired'));
    END IF;
END $$;

-- Indexes for common lookups
CREATE INDEX IF NOT EXISTS entitlements_plan_idx ON "public"."entitlements" (plan);
CREATE INDEX IF NOT EXISTS entitlements_provider_customer_id_idx
    ON "public"."entitlements" (provider_customer_id)
    WHERE provider_customer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS entitlements_current_period_end_idx
    ON "public"."entitlements" (current_period_end)
    WHERE current_period_end IS NOT NULL;

-- Backfill: one row per existing user, all free
INSERT INTO "public"."entitlements" (user_id)
SELECT user_id FROM "public"."users"
ON CONFLICT (user_id) DO NOTHING;

-- Trigger to auto-create an entitlements row on user insert.
-- Ensures every user always has exactly one entitlements row - no NULL handling in app code.
CREATE OR REPLACE FUNCTION public.create_default_entitlement()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO "public"."entitlements" (user_id) VALUES (NEW.user_id)
    ON CONFLICT (user_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS users_create_entitlement ON "public"."users";
CREATE TRIGGER users_create_entitlement
    AFTER INSERT ON "public"."users"
    FOR EACH ROW EXECUTE FUNCTION public.create_default_entitlement();

-- Trigger to keep updated_at fresh
CREATE OR REPLACE FUNCTION public.entitlements_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS entitlements_updated_at ON "public"."entitlements";
CREATE TRIGGER entitlements_updated_at
    BEFORE UPDATE ON "public"."entitlements"
    FOR EACH ROW EXECUTE FUNCTION public.entitlements_set_updated_at();

-- RLS: users can read their own entitlements row; writes are server-side only.
ALTER TABLE "public"."entitlements" ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'public' AND tablename = 'entitlements' AND policyname = 'entitlements_select_own'
    ) THEN
        CREATE POLICY entitlements_select_own ON "public"."entitlements"
            FOR SELECT USING (auth.uid() = user_id);
    END IF;
END $$;
