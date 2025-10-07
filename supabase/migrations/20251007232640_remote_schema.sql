create type "public"."roadmap_type" as enum ('ai_generated', 'custom_made');

alter table "public"."roadmaps" add column "type" roadmap_type not null default 'custom_made'::roadmap_type;


