-- Contact Support moves off the client-side mail composer and onto POST
-- /feedback, so every inbound user-contact channel lands in one table (and
-- one inbox). Support requests carry a subject line; the other categories
-- do not.

alter table public.feedback
    add column if not exists subject text;

alter table public.feedback
    drop constraint if exists feedback_category_check;

alter table public.feedback
    add constraint feedback_category_check
    check (category in ('bug', 'feature', 'general', 'support'));
