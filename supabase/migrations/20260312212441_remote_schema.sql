create type "public"."status" as enum ('not_started', 'in_progress', 'overdue', 'completed');

alter table "public"."milestones" add column "status" public.status not null default 'not_started'::public.status;

alter table "public"."phases" add column "status" public.status not null default 'not_started'::public.status;

alter table "public"."tasks" add column "status" public.status not null default 'not_started'::public.status;


