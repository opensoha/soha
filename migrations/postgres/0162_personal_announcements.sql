CREATE TABLE IF NOT EXISTS public.personal_announcements (
    id text PRIMARY KEY,
    recipient_user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    title text NOT NULL,
    content text NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    read_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_personal_announcements_recipient
    ON public.personal_announcements (recipient_user_id, read_at, created_at DESC);
