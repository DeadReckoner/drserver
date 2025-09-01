create table public.threads (
  thread_id uuid not null default gen_random_uuid (),
  created_at timestamp with time zone not null default now(),
  updated_at timestamp with time zone not null default now(),
  name character varying null,
  constraint thread_pkey primary key (thread_id)
) TABLESPACE pg_default;

create table public.thread_participants (
  thread_p_id uuid not null default gen_random_uuid (),
  thread_id uuid not null,
  user_id uuid not null,
  joined_at timestamp with time zone not null,
  left_at timestamp with time zone null,
  role character varying null default 'member'::character varying,
  UNIQUE(thread_id, user_id),
  constraint thread_participants_pkey primary key (thread_p_id),
  constraint thread_participants_user_id_fkey foreign KEY (user_id) references users (user_id),
  constraint thread_participants_thread_id_fkey foreign KEY (thread_id) references threads (thread_id)
) TABLESPACE pg_default;
