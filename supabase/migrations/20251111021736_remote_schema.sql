alter table "public"."messages" drop constraint "messages_pkey";

drop index if exists "public"."messages_pkey";

alter table "public"."messages" drop column "id";

alter table "public"."messages" add column "message_id" uuid not null default gen_random_uuid();

CREATE UNIQUE INDEX messages_pkey ON public.messages USING btree (message_id);

alter table "public"."messages" add constraint "messages_pkey" PRIMARY KEY using index "messages_pkey";


