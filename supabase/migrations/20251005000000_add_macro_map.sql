-- Create macro_maps table for strategic overview
create table "public"."macro_maps" (
    "macro_map_id" uuid not null default gen_random_uuid(),
    "roadmap_id" uuid not null,
    "created_at" timestamp with time zone not null default now()
);

alter table "public"."macro_maps" enable row level security;

CREATE UNIQUE INDEX macro_maps_pkey ON public.macro_maps USING btree (macro_map_id);
CREATE UNIQUE INDEX macro_maps_roadmap_id_key ON public.macro_maps USING btree (roadmap_id);

alter table "public"."macro_maps" add constraint "macro_maps_pkey" PRIMARY KEY using index "macro_maps_pkey";

alter table "public"."macro_maps" add constraint "macro_maps_roadmap_id_fkey" FOREIGN KEY (roadmap_id) REFERENCES roadmaps(roadmap_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;
alter table "public"."macro_maps" validate constraint "macro_maps_roadmap_id_fkey";

-- Create phases table
create table "public"."phases" (
    "phase_id" uuid not null default gen_random_uuid(),
    "macro_map_id" uuid not null,
    "name" character varying not null,
    "description" character varying,
    "order_index" integer not null,
    "duration_weeks" character varying,
    "created_at" timestamp with time zone not null default now()
);

alter table "public"."phases" enable row level security;

CREATE UNIQUE INDEX phases_pkey ON public.phases USING btree (phase_id);

alter table "public"."phases" add constraint "phases_pkey" PRIMARY KEY using index "phases_pkey";

alter table "public"."phases" add constraint "phases_macro_map_id_fkey" FOREIGN KEY (macro_map_id) REFERENCES macro_maps(macro_map_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;
alter table "public"."phases" validate constraint "phases_macro_map_id_fkey";

-- Create macro_milestones table
create table "public"."macro_milestones" (
    "macro_milestone_id" uuid not null default gen_random_uuid(),
    "phase_id" uuid not null,
    "name" character varying not null,
    "acceptance_test" character varying,
    "duration_days_min" integer,
    "duration_days_max" integer,
    "order_index" integer not null,
    "resources_forecast" jsonb,
    "detailed_milestone_id" uuid,
    "created_at" timestamp with time zone not null default now()
);

alter table "public"."macro_milestones" enable row level security;

CREATE UNIQUE INDEX macro_milestones_pkey ON public.macro_milestones USING btree (macro_milestone_id);

alter table "public"."macro_milestones" add constraint "macro_milestones_pkey" PRIMARY KEY using index "macro_milestones_pkey";

alter table "public"."macro_milestones" add constraint "macro_milestones_phase_id_fkey" FOREIGN KEY (phase_id) REFERENCES phases(phase_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;
alter table "public"."macro_milestones" validate constraint "macro_milestones_phase_id_fkey";

alter table "public"."macro_milestones" add constraint "macro_milestones_detailed_milestone_id_fkey" FOREIGN KEY (detailed_milestone_id) REFERENCES milestones(milestone_id) ON UPDATE CASCADE ON DELETE SET NULL not valid;
alter table "public"."macro_milestones" validate constraint "macro_milestones_detailed_milestone_id_fkey";

-- Create spikes table
create table "public"."spikes" (
    "spike_id" uuid not null default gen_random_uuid(),
    "macro_milestone_id" uuid not null,
    "description" character varying not null,
    "timebox_days" integer not null,
    "created_at" timestamp with time zone not null default now()
);

alter table "public"."spikes" enable row level security;

CREATE UNIQUE INDEX spikes_pkey ON public.spikes USING btree (spike_id);

alter table "public"."spikes" add constraint "spikes_pkey" PRIMARY KEY using index "spikes_pkey";

alter table "public"."spikes" add constraint "spikes_macro_milestone_id_fkey" FOREIGN KEY (macro_milestone_id) REFERENCES macro_milestones(macro_milestone_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;
alter table "public"."spikes" validate constraint "spikes_macro_milestone_id_fkey";

-- Create risks table
create table "public"."risks" (
    "risk_id" uuid not null default gen_random_uuid(),
    "macro_milestone_id" uuid not null,
    "description" character varying not null,
    "severity" character varying not null,
    "created_at" timestamp with time zone not null default now()
);

alter table "public"."risks" enable row level security;

CREATE UNIQUE INDEX risks_pkey ON public.risks USING btree (risk_id);

alter table "public"."risks" add constraint "risks_pkey" PRIMARY KEY using index "risks_pkey";

alter table "public"."risks" add constraint "risks_macro_milestone_id_fkey" FOREIGN KEY (macro_milestone_id) REFERENCES macro_milestones(macro_milestone_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;
alter table "public"."risks" validate constraint "risks_macro_milestone_id_fkey";

-- Add new columns to milestones table
alter table "public"."milestones" add column "macro_milestone_id" uuid;
alter table "public"."milestones" add column "date" timestamp with time zone;

alter table "public"."milestones" add constraint "milestones_macro_milestone_id_fkey" FOREIGN KEY (macro_milestone_id) REFERENCES macro_milestones(macro_milestone_id) ON UPDATE CASCADE ON DELETE SET NULL not valid;
alter table "public"."milestones" validate constraint "milestones_macro_milestone_id_fkey";

-- Add new columns to tasks table
alter table "public"."tasks" add column "rationale" character varying;
alter table "public"."tasks" add column "est_minutes" integer;
alter table "public"."tasks" add column "dependencies" jsonb;
alter table "public"."tasks" add column "confidence" character varying;

-- Grant permissions for macro_maps
grant delete on table "public"."macro_maps" to "anon";
grant insert on table "public"."macro_maps" to "anon";
grant references on table "public"."macro_maps" to "anon";
grant select on table "public"."macro_maps" to "anon";
grant trigger on table "public"."macro_maps" to "anon";
grant truncate on table "public"."macro_maps" to "anon";
grant update on table "public"."macro_maps" to "anon";

grant delete on table "public"."macro_maps" to "authenticated";
grant insert on table "public"."macro_maps" to "authenticated";
grant references on table "public"."macro_maps" to "authenticated";
grant select on table "public"."macro_maps" to "authenticated";
grant trigger on table "public"."macro_maps" to "authenticated";
grant truncate on table "public"."macro_maps" to "authenticated";
grant update on table "public"."macro_maps" to "authenticated";

grant delete on table "public"."macro_maps" to "service_role";
grant insert on table "public"."macro_maps" to "service_role";
grant references on table "public"."macro_maps" to "service_role";
grant select on table "public"."macro_maps" to "service_role";
grant trigger on table "public"."macro_maps" to "service_role";
grant truncate on table "public"."macro_maps" to "service_role";
grant update on table "public"."macro_maps" to "service_role";

-- Grant permissions for phases
grant delete on table "public"."phases" to "anon";
grant insert on table "public"."phases" to "anon";
grant references on table "public"."phases" to "anon";
grant select on table "public"."phases" to "anon";
grant trigger on table "public"."phases" to "anon";
grant truncate on table "public"."phases" to "anon";
grant update on table "public"."phases" to "anon";

grant delete on table "public"."phases" to "authenticated";
grant insert on table "public"."phases" to "authenticated";
grant references on table "public"."phases" to "authenticated";
grant select on table "public"."phases" to "authenticated";
grant trigger on table "public"."phases" to "authenticated";
grant truncate on table "public"."phases" to "authenticated";
grant update on table "public"."phases" to "authenticated";

grant delete on table "public"."phases" to "service_role";
grant insert on table "public"."phases" to "service_role";
grant references on table "public"."phases" to "service_role";
grant select on table "public"."phases" to "service_role";
grant trigger on table "public"."phases" to "service_role";
grant truncate on table "public"."phases" to "service_role";
grant update on table "public"."phases" to "service_role";

-- Grant permissions for macro_milestones
grant delete on table "public"."macro_milestones" to "anon";
grant insert on table "public"."macro_milestones" to "anon";
grant references on table "public"."macro_milestones" to "anon";
grant select on table "public"."macro_milestones" to "anon";
grant trigger on table "public"."macro_milestones" to "anon";
grant truncate on table "public"."macro_milestones" to "anon";
grant update on table "public"."macro_milestones" to "anon";

grant delete on table "public"."macro_milestones" to "authenticated";
grant insert on table "public"."macro_milestones" to "authenticated";
grant references on table "public"."macro_milestones" to "authenticated";
grant select on table "public"."macro_milestones" to "authenticated";
grant trigger on table "public"."macro_milestones" to "authenticated";
grant truncate on table "public"."macro_milestones" to "authenticated";
grant update on table "public"."macro_milestones" to "authenticated";

grant delete on table "public"."macro_milestones" to "service_role";
grant insert on table "public"."macro_milestones" to "service_role";
grant references on table "public"."macro_milestones" to "service_role";
grant select on table "public"."macro_milestones" to "service_role";
grant trigger on table "public"."macro_milestones" to "service_role";
grant truncate on table "public"."macro_milestones" to "service_role";
grant update on table "public"."macro_milestones" to "service_role";

-- Grant permissions for spikes
grant delete on table "public"."spikes" to "anon";
grant insert on table "public"."spikes" to "anon";
grant references on table "public"."spikes" to "anon";
grant select on table "public"."spikes" to "anon";
grant trigger on table "public"."spikes" to "anon";
grant truncate on table "public"."spikes" to "anon";
grant update on table "public"."spikes" to "anon";

grant delete on table "public"."spikes" to "authenticated";
grant insert on table "public"."spikes" to "authenticated";
grant references on table "public"."spikes" to "authenticated";
grant select on table "public"."spikes" to "authenticated";
grant trigger on table "public"."spikes" to "authenticated";
grant truncate on table "public"."spikes" to "authenticated";
grant update on table "public"."spikes" to "authenticated";

grant delete on table "public"."spikes" to "service_role";
grant insert on table "public"."spikes" to "service_role";
grant references on table "public"."spikes" to "service_role";
grant select on table "public"."spikes" to "service_role";
grant trigger on table "public"."spikes" to "service_role";
grant truncate on table "public"."spikes" to "service_role";
grant update on table "public"."spikes" to "service_role";

-- Grant permissions for risks
grant delete on table "public"."risks" to "anon";
grant insert on table "public"."risks" to "anon";
grant references on table "public"."risks" to "anon";
grant select on table "public"."risks" to "anon";
grant trigger on table "public"."risks" to "anon";
grant truncate on table "public"."risks" to "anon";
grant update on table "public"."risks" to "anon";

grant delete on table "public"."risks" to "authenticated";
grant insert on table "public"."risks" to "authenticated";
grant references on table "public"."risks" to "authenticated";
grant select on table "public"."risks" to "authenticated";
grant trigger on table "public"."risks" to "authenticated";
grant truncate on table "public"."risks" to "authenticated";
grant update on table "public"."risks" to "authenticated";

grant delete on table "public"."risks" to "service_role";
grant insert on table "public"."risks" to "service_role";
grant references on table "public"."risks" to "service_role";
grant select on table "public"."risks" to "service_role";
grant trigger on table "public"."risks" to "service_role";
grant truncate on table "public"."risks" to "service_role";
grant update on table "public"."risks" to "service_role";
