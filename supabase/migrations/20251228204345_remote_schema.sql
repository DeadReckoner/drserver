alter table "public"."threads" add column "last_sent_message" uuid;

alter table "public"."threads" add column "total_unread" integer default 0;


