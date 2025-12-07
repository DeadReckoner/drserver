-- Create anchor_requests table
create table "public"."anchor_requests" (
    "request_id" uuid not null default gen_random_uuid(),
    "roadmap_id" uuid not null,
    "requester_id" uuid not null,
    "anchor_id" uuid not null,
    "status" text not null default 'pending',
    "created_at" timestamp with time zone not null default now(),
    "updated_at" timestamp with time zone,
    constraint anchor_requests_pkey primary key (request_id),
    constraint anchor_requests_roadmap_id_fkey foreign key (roadmap_id) references roadmaps(roadmap_id) on update cascade on delete cascade,
    constraint anchor_requests_requester_id_fkey foreign key (requester_id) references users(user_id) on update cascade on delete cascade,
    constraint anchor_requests_anchor_id_fkey foreign key (anchor_id) references users(user_id) on update cascade on delete cascade,
    constraint anchor_requests_status_check check (status in ('pending', 'accepted', 'rejected'))
);

-- Enable RLS
alter table "public"."anchor_requests" enable row level security;

-- Create index for faster lookups
create index anchor_requests_roadmap_id_idx on public.anchor_requests using btree (roadmap_id);
create index anchor_requests_anchor_id_idx on public.anchor_requests using btree (anchor_id);
create index anchor_requests_status_idx on public.anchor_requests using btree (status);

-- Create unique constraint to prevent duplicate pending requests
create unique index anchor_requests_unique_pending_idx on public.anchor_requests (roadmap_id, anchor_id) where status = 'pending';

-- Grant permissions
grant delete on table "public"."anchor_requests" to "anon";
grant insert on table "public"."anchor_requests" to "anon";
grant references on table "public"."anchor_requests" to "anon";
grant select on table "public"."anchor_requests" to "anon";
grant trigger on table "public"."anchor_requests" to "anon";
grant truncate on table "public"."anchor_requests" to "anon";
grant update on table "public"."anchor_requests" to "anon";

grant delete on table "public"."anchor_requests" to "authenticated";
grant insert on table "public"."anchor_requests" to "authenticated";
grant references on table "public"."anchor_requests" to "authenticated";
grant select on table "public"."anchor_requests" to "authenticated";
grant trigger on table "public"."anchor_requests" to "authenticated";
grant truncate on table "public"."anchor_requests" to "authenticated";
grant update on table "public"."anchor_requests" to "authenticated";

grant delete on table "public"."anchor_requests" to "service_role";
grant insert on table "public"."anchor_requests" to "service_role";
grant references on table "public"."anchor_requests" to "service_role";
grant select on table "public"."anchor_requests" to "service_role";
grant trigger on table "public"."anchor_requests" to "service_role";
grant truncate on table "public"."anchor_requests" to "service_role";
grant update on table "public"."anchor_requests" to "service_role";

-- RLS policies
-- Users can view anchor requests where they are the requester or the anchor
create policy "Users can view their own anchor requests"
on public.anchor_requests for select
using (auth.uid() = requester_id::uuid or auth.uid() = anchor_id::uuid);

-- Users can create anchor requests for their own roadmaps
create policy "Users can create anchor requests for their roadmaps"
on public.anchor_requests for insert
with check (auth.uid() = requester_id::uuid);

-- Anchors can update requests where they are the anchor
create policy "Anchors can update their requests"
on public.anchor_requests for update
using (auth.uid() = anchor_id::uuid);

-- Requesters can delete their pending requests
create policy "Requesters can delete their pending requests"
on public.anchor_requests for delete
using (auth.uid() = requester_id::uuid and status = 'pending');
