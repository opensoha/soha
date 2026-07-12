SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.directory_event_inbox ADD COLUMN IF NOT EXISTS claimed_at timestamptz;
ALTER TABLE public.directory_event_inbox ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz;
CREATE INDEX IF NOT EXISTS idx_directory_event_inbox_retry
    ON public.directory_event_inbox(status,next_attempt_at,received_at);

ALTER TABLE public.directory_scim_tokens
    ADD COLUMN IF NOT EXISTS scopes jsonb NOT NULL DEFAULT '["scim.read","scim.write"]'::jsonb;
