-- Add async processing support for long-running roadmap generation
-- Creates background_jobs, push_tokens, and notifications tables
-- Updates roadmaps table to track generation jobs

-- Create enum types for type safety
CREATE TYPE job_status AS ENUM ('pending', 'processing', 'completed', 'failed');
CREATE TYPE job_type AS ENUM ('roadmap_generation', 'milestone_generation');
CREATE TYPE platform_type AS ENUM ('ios', 'android');
CREATE TYPE notification_status AS ENUM ('pending', 'sent', 'failed', 'read');
CREATE TYPE generation_status AS ENUM ('pending', 'processing', 'completed', 'failed');

-- Background jobs tracking table
CREATE TABLE "public"."background_jobs" (
    "job_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" uuid NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    "type" job_type NOT NULL,
    "status" job_status DEFAULT 'pending',
    "progress" integer DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),

    -- Input payload (types.GoalRequest as JSONB)
    "payload" jsonb NOT NULL,

    -- Result data (roadmap_id on success)
    "result" jsonb,

    -- Error information
    "error" text,

    -- Timing
    "started_at" timestamp with time zone,
    "completed_at" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL DEFAULT now(),
    "updated_at" timestamp with time zone NOT NULL DEFAULT now()
);

ALTER TABLE "public"."background_jobs" ENABLE ROW LEVEL SECURITY;

-- Push notification tokens
CREATE TABLE "public"."push_tokens" (
    "token_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" uuid NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    "expo_token" text NOT NULL UNIQUE,
    "platform" platform_type,
    "device_id" text,
    "is_active" boolean DEFAULT true,
    "last_used_at" timestamp with time zone DEFAULT now(),
    "created_at" timestamp with time zone DEFAULT now()
);

ALTER TABLE "public"."push_tokens" ENABLE ROW LEVEL SECURITY;

-- Notification log
CREATE TABLE "public"."notifications" (
    "notification_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" uuid NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    "type" character varying(50) NOT NULL,
    "title" text NOT NULL,
    "body" text NOT NULL,
    "data" jsonb,

    -- Status tracking
    "status" notification_status DEFAULT 'pending',
    "sent_at" timestamp with time zone,
    "read_at" timestamp with time zone,
    "error" text,

    "created_at" timestamp with time zone DEFAULT now()
);

ALTER TABLE "public"."notifications" ENABLE ROW LEVEL SECURITY;

-- Update roadmaps table to track generation jobs
ALTER TABLE "public"."roadmaps"
    ADD COLUMN IF NOT EXISTS "job_id" uuid REFERENCES background_jobs(job_id),
    ADD COLUMN IF NOT EXISTS "generation_status" generation_status DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS "type" character varying;

-- Create indexes for performance
CREATE INDEX idx_background_jobs_user_status ON background_jobs(user_id, status);
CREATE INDEX idx_background_jobs_status_created ON background_jobs(status, created_at DESC);
CREATE INDEX idx_push_tokens_user_active ON push_tokens(user_id, is_active) WHERE is_active = true;
CREATE INDEX idx_notifications_user_unread ON notifications(user_id, read_at) WHERE read_at IS NULL;
CREATE INDEX idx_roadmaps_job_id ON roadmaps(job_id);

-- Grant permissions for background_jobs
GRANT DELETE ON TABLE "public"."background_jobs" TO "anon";
GRANT INSERT ON TABLE "public"."background_jobs" TO "anon";
GRANT REFERENCES ON TABLE "public"."background_jobs" TO "anon";
GRANT SELECT ON TABLE "public"."background_jobs" TO "anon";
GRANT TRIGGER ON TABLE "public"."background_jobs" TO "anon";
GRANT TRUNCATE ON TABLE "public"."background_jobs" TO "anon";
GRANT UPDATE ON TABLE "public"."background_jobs" TO "anon";

GRANT DELETE ON TABLE "public"."background_jobs" TO "authenticated";
GRANT INSERT ON TABLE "public"."background_jobs" TO "authenticated";
GRANT REFERENCES ON TABLE "public"."background_jobs" TO "authenticated";
GRANT SELECT ON TABLE "public"."background_jobs" TO "authenticated";
GRANT TRIGGER ON TABLE "public"."background_jobs" TO "authenticated";
GRANT TRUNCATE ON TABLE "public"."background_jobs" TO "authenticated";
GRANT UPDATE ON TABLE "public"."background_jobs" TO "authenticated";

GRANT DELETE ON TABLE "public"."background_jobs" TO "service_role";
GRANT INSERT ON TABLE "public"."background_jobs" TO "service_role";
GRANT REFERENCES ON TABLE "public"."background_jobs" TO "service_role";
GRANT SELECT ON TABLE "public"."background_jobs" TO "service_role";
GRANT TRIGGER ON TABLE "public"."background_jobs" TO "service_role";
GRANT TRUNCATE ON TABLE "public"."background_jobs" TO "service_role";
GRANT UPDATE ON TABLE "public"."background_jobs" TO "service_role";

-- Grant permissions for push_tokens
GRANT DELETE ON TABLE "public"."push_tokens" TO "anon";
GRANT INSERT ON TABLE "public"."push_tokens" TO "anon";
GRANT REFERENCES ON TABLE "public"."push_tokens" TO "anon";
GRANT SELECT ON TABLE "public"."push_tokens" TO "anon";
GRANT TRIGGER ON TABLE "public"."push_tokens" TO "anon";
GRANT TRUNCATE ON TABLE "public"."push_tokens" TO "anon";
GRANT UPDATE ON TABLE "public"."push_tokens" TO "anon";

GRANT DELETE ON TABLE "public"."push_tokens" TO "authenticated";
GRANT INSERT ON TABLE "public"."push_tokens" TO "authenticated";
GRANT REFERENCES ON TABLE "public"."push_tokens" TO "authenticated";
GRANT SELECT ON TABLE "public"."push_tokens" TO "authenticated";
GRANT TRIGGER ON TABLE "public"."push_tokens" TO "authenticated";
GRANT TRUNCATE ON TABLE "public"."push_tokens" TO "authenticated";
GRANT UPDATE ON TABLE "public"."push_tokens" TO "authenticated";

GRANT DELETE ON TABLE "public"."push_tokens" TO "service_role";
GRANT INSERT ON TABLE "public"."push_tokens" TO "service_role";
GRANT REFERENCES ON TABLE "public"."push_tokens" TO "service_role";
GRANT SELECT ON TABLE "public"."push_tokens" TO "service_role";
GRANT TRIGGER ON TABLE "public"."push_tokens" TO "service_role";
GRANT TRUNCATE ON TABLE "public"."push_tokens" TO "service_role";
GRANT UPDATE ON TABLE "public"."push_tokens" TO "service_role";

-- Grant permissions for notifications
GRANT DELETE ON TABLE "public"."notifications" TO "anon";
GRANT INSERT ON TABLE "public"."notifications" TO "anon";
GRANT REFERENCES ON TABLE "public"."notifications" TO "anon";
GRANT SELECT ON TABLE "public"."notifications" TO "anon";
GRANT TRIGGER ON TABLE "public"."notifications" TO "anon";
GRANT TRUNCATE ON TABLE "public"."notifications" TO "anon";
GRANT UPDATE ON TABLE "public"."notifications" TO "anon";

GRANT DELETE ON TABLE "public"."notifications" TO "authenticated";
GRANT INSERT ON TABLE "public"."notifications" TO "authenticated";
GRANT REFERENCES ON TABLE "public"."notifications" TO "authenticated";
GRANT SELECT ON TABLE "public"."notifications" TO "authenticated";
GRANT TRIGGER ON TABLE "public"."notifications" TO "authenticated";
GRANT TRUNCATE ON TABLE "public"."notifications" TO "authenticated";
GRANT UPDATE ON TABLE "public"."notifications" TO "authenticated";

GRANT DELETE ON TABLE "public"."notifications" TO "service_role";
GRANT INSERT ON TABLE "public"."notifications" TO "service_role";
GRANT REFERENCES ON TABLE "public"."notifications" TO "service_role";
GRANT SELECT ON TABLE "public"."notifications" TO "service_role";
GRANT TRIGGER ON TABLE "public"."notifications" TO "service_role";
GRANT TRUNCATE ON TABLE "public"."notifications" TO "service_role";
GRANT UPDATE ON TABLE "public"."notifications" TO "service_role";
