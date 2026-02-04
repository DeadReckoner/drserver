create table if not exists public.feedback (
    feedback_id uuid primary key default gen_random_uuid(),
    user_id uuid not null references auth.users(id) on delete cascade,
    category text not null check (category in ('bug', 'feature', 'general')),
    message text not null,
    app_version text,
    os text,
    device text,
    created_at timestamptz not null default now()
);

alter table public.feedback enable row level security;

create policy "Users can insert their own feedback"
    on public.feedback for insert
    with check (auth.uid() = user_id);

create policy "Users can view their own feedback"
    on public.feedback for select
    using (auth.uid() = user_id);
