CREATE UNIQUE INDEX messages_pkey ON public.messages USING btree (message_id);

alter table "public"."messages" add constraint "messages_pkey" PRIMARY KEY using index "messages_pkey";


