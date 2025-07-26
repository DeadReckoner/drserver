revoke delete on table "public"."message" from "anon";

revoke insert on table "public"."message" from "anon";

revoke references on table "public"."message" from "anon";

revoke select on table "public"."message" from "anon";

revoke trigger on table "public"."message" from "anon";

revoke truncate on table "public"."message" from "anon";

revoke update on table "public"."message" from "anon";

revoke delete on table "public"."message" from "authenticated";

revoke insert on table "public"."message" from "authenticated";

revoke references on table "public"."message" from "authenticated";

revoke select on table "public"."message" from "authenticated";

revoke trigger on table "public"."message" from "authenticated";

revoke truncate on table "public"."message" from "authenticated";

revoke update on table "public"."message" from "authenticated";

revoke delete on table "public"."message" from "service_role";

revoke insert on table "public"."message" from "service_role";

revoke references on table "public"."message" from "service_role";

revoke select on table "public"."message" from "service_role";

revoke trigger on table "public"."message" from "service_role";

revoke truncate on table "public"."message" from "service_role";

revoke update on table "public"."message" from "service_role";

revoke delete on table "public"."message_thread" from "anon";

revoke insert on table "public"."message_thread" from "anon";

revoke references on table "public"."message_thread" from "anon";

revoke select on table "public"."message_thread" from "anon";

revoke trigger on table "public"."message_thread" from "anon";

revoke truncate on table "public"."message_thread" from "anon";

revoke update on table "public"."message_thread" from "anon";

revoke delete on table "public"."message_thread" from "authenticated";

revoke insert on table "public"."message_thread" from "authenticated";

revoke references on table "public"."message_thread" from "authenticated";

revoke select on table "public"."message_thread" from "authenticated";

revoke trigger on table "public"."message_thread" from "authenticated";

revoke truncate on table "public"."message_thread" from "authenticated";

revoke update on table "public"."message_thread" from "authenticated";

revoke delete on table "public"."message_thread" from "service_role";

revoke insert on table "public"."message_thread" from "service_role";

revoke references on table "public"."message_thread" from "service_role";

revoke select on table "public"."message_thread" from "service_role";

revoke trigger on table "public"."message_thread" from "service_role";

revoke truncate on table "public"."message_thread" from "service_role";

revoke update on table "public"."message_thread" from "service_role";

alter table "public"."message" drop constraint "message_sender_id_fkey";

alter table "public"."message" drop constraint "message_sender_id_key";

alter table "public"."message" drop constraint "message_thread_id_fkey";

alter table "public"."message" drop constraint "message_thread_key";

alter table "public"."tasks" drop constraint "tasks_anchor_fkey";

alter table "public"."tasks" drop constraint "tasks_parent_task_id_fkey";

alter table "public"."message" drop constraint "message_pkey";

alter table "public"."message_thread" drop constraint "message_thread_pkey";

drop index if exists "public"."message_pkey";

drop index if exists "public"."message_sender_id_key";

drop index if exists "public"."message_thread_key";

drop index if exists "public"."message_thread_pkey";

drop table "public"."message";

drop table "public"."message_thread";

create table "public"."milestones" (
    "milestone_id" uuid not null default gen_random_uuid(),
    "created_at" timestamp with time zone not null default now(),
    "sub_goal" character varying not null,
    "description" character varying,
    "day" integer not null,
    "reached" boolean not null default false,
    "roadmap_id" uuid not null,
    "user_id" uuid not null default auth.uid()
);


alter table "public"."milestones" enable row level security;

create table "public"."roadmaps" (
    "roadmap_id" uuid not null default gen_random_uuid(),
    "created_at" timestamp with time zone not null default now(),
    "goal" character varying not null,
    "description" character varying,
    "days" integer not null,
    "completed" boolean not null default false,
    "start_date" date,
    "user_id" uuid not null default auth.uid()
);


alter table "public"."roadmaps" enable row level security;

create table "public"."steps" (
    "step_id" uuid not null default gen_random_uuid(),
    "created_at" timestamp with time zone not null default now(),
    "description" character varying not null,
    "completed" boolean not null default false,
    "number" integer not null,
    "task_id" uuid not null
);


alter table "public"."steps" enable row level security;

alter table "public"."tasks" drop column "anchor";

alter table "public"."tasks" drop column "end_date";

alter table "public"."tasks" drop column "level";

alter table "public"."tasks" drop column "milestone";

alter table "public"."tasks" drop column "parent_task_id";

alter table "public"."tasks" drop column "start_date";

alter table "public"."tasks" drop column "sub_goal";

alter table "public"."tasks" add column "day" integer;

alter table "public"."tasks" add column "milestone_id" uuid not null;

CREATE UNIQUE INDEX milestones_pkey ON public.milestones USING btree (milestone_id);

CREATE UNIQUE INDEX roadmaps_pkey ON public.roadmaps USING btree (roadmap_id);

CREATE UNIQUE INDEX steps_pkey ON public.steps USING btree (step_id);

alter table "public"."milestones" add constraint "milestones_pkey" PRIMARY KEY using index "milestones_pkey";

alter table "public"."roadmaps" add constraint "roadmaps_pkey" PRIMARY KEY using index "roadmaps_pkey";

alter table "public"."steps" add constraint "steps_pkey" PRIMARY KEY using index "steps_pkey";

alter table "public"."milestones" add constraint "milestones_roadmap_id_fkey" FOREIGN KEY (roadmap_id) REFERENCES roadmaps(roadmap_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;

alter table "public"."milestones" validate constraint "milestones_roadmap_id_fkey";

alter table "public"."milestones" add constraint "milestones_user_id_fkey" FOREIGN KEY (user_id) REFERENCES users(user_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;

alter table "public"."milestones" validate constraint "milestones_user_id_fkey";

alter table "public"."roadmaps" add constraint "roadmaps_user_id_fkey" FOREIGN KEY (user_id) REFERENCES users(user_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;

alter table "public"."roadmaps" validate constraint "roadmaps_user_id_fkey";

alter table "public"."steps" add constraint "steps_task_id_fkey" FOREIGN KEY (task_id) REFERENCES tasks(task_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;

alter table "public"."steps" validate constraint "steps_task_id_fkey";

alter table "public"."tasks" add constraint "tasks_milestone_id_fkey" FOREIGN KEY (milestone_id) REFERENCES milestones(milestone_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;

alter table "public"."tasks" validate constraint "tasks_milestone_id_fkey";

grant delete on table "public"."milestones" to "anon";

grant insert on table "public"."milestones" to "anon";

grant references on table "public"."milestones" to "anon";

grant select on table "public"."milestones" to "anon";

grant trigger on table "public"."milestones" to "anon";

grant truncate on table "public"."milestones" to "anon";

grant update on table "public"."milestones" to "anon";

grant delete on table "public"."milestones" to "authenticated";

grant insert on table "public"."milestones" to "authenticated";

grant references on table "public"."milestones" to "authenticated";

grant select on table "public"."milestones" to "authenticated";

grant trigger on table "public"."milestones" to "authenticated";

grant truncate on table "public"."milestones" to "authenticated";

grant update on table "public"."milestones" to "authenticated";

grant delete on table "public"."milestones" to "service_role";

grant insert on table "public"."milestones" to "service_role";

grant references on table "public"."milestones" to "service_role";

grant select on table "public"."milestones" to "service_role";

grant trigger on table "public"."milestones" to "service_role";

grant truncate on table "public"."milestones" to "service_role";

grant update on table "public"."milestones" to "service_role";

grant delete on table "public"."roadmaps" to "anon";

grant insert on table "public"."roadmaps" to "anon";

grant references on table "public"."roadmaps" to "anon";

grant select on table "public"."roadmaps" to "anon";

grant trigger on table "public"."roadmaps" to "anon";

grant truncate on table "public"."roadmaps" to "anon";

grant update on table "public"."roadmaps" to "anon";

grant delete on table "public"."roadmaps" to "authenticated";

grant insert on table "public"."roadmaps" to "authenticated";

grant references on table "public"."roadmaps" to "authenticated";

grant select on table "public"."roadmaps" to "authenticated";

grant trigger on table "public"."roadmaps" to "authenticated";

grant truncate on table "public"."roadmaps" to "authenticated";

grant update on table "public"."roadmaps" to "authenticated";

grant delete on table "public"."roadmaps" to "service_role";

grant insert on table "public"."roadmaps" to "service_role";

grant references on table "public"."roadmaps" to "service_role";

grant select on table "public"."roadmaps" to "service_role";

grant trigger on table "public"."roadmaps" to "service_role";

grant truncate on table "public"."roadmaps" to "service_role";

grant update on table "public"."roadmaps" to "service_role";

grant delete on table "public"."steps" to "anon";

grant insert on table "public"."steps" to "anon";

grant references on table "public"."steps" to "anon";

grant select on table "public"."steps" to "anon";

grant trigger on table "public"."steps" to "anon";

grant truncate on table "public"."steps" to "anon";

grant update on table "public"."steps" to "anon";

grant delete on table "public"."steps" to "authenticated";

grant insert on table "public"."steps" to "authenticated";

grant references on table "public"."steps" to "authenticated";

grant select on table "public"."steps" to "authenticated";

grant trigger on table "public"."steps" to "authenticated";

grant truncate on table "public"."steps" to "authenticated";

grant update on table "public"."steps" to "authenticated";

grant delete on table "public"."steps" to "service_role";

grant insert on table "public"."steps" to "service_role";

grant references on table "public"."steps" to "service_role";

grant select on table "public"."steps" to "service_role";

grant trigger on table "public"."steps" to "service_role";

grant truncate on table "public"."steps" to "service_role";

grant update on table "public"."steps" to "service_role";


