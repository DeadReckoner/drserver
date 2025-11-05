alter table "public"."tasks" add column "roadmap_id" uuid;

alter table "public"."users" add column "phone_number" text;

alter table "public"."users" alter column "email" set not null;

CREATE UNIQUE INDEX users_phone_number_key ON public.users USING btree (phone_number);

alter table "public"."tasks" add constraint "tasks_roadmap_id_fkey" FOREIGN KEY (roadmap_id) REFERENCES roadmaps(roadmap_id) ON UPDATE CASCADE ON DELETE CASCADE not valid;

alter table "public"."tasks" validate constraint "tasks_roadmap_id_fkey";

alter table "public"."users" add constraint "users_phone_number_key" UNIQUE using index "users_phone_number_key";


