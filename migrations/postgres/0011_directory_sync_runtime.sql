SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.directory_conflicts (
    id text PRIMARY KEY, connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    object_type text NOT NULL, external_id text NOT NULL DEFAULT '', reason text NOT NULL,
    status text NOT NULL DEFAULT 'open', resolution text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
    resolved_by text NOT NULL DEFAULT '', resolved_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_directory_conflicts_open_object
    ON public.directory_conflicts(connection_id,object_type,external_id,reason) WHERE status='open';

CREATE TABLE IF NOT EXISTS public.directory_webhook_credentials (
    connection_id text PRIMARY KEY REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    verification_token_encrypted text NOT NULL, encrypt_key_encrypted text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.directory_event_inbox (
    id text PRIMARY KEY, connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    provider_event_id text NOT NULL, event_type text NOT NULL, occurred_at timestamptz NOT NULL, received_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'queued', attempts integer NOT NULL DEFAULT 0, error_summary text NOT NULL DEFAULT '', processed_at timestamptz,
    UNIQUE(connection_id,provider_event_id)
);
CREATE INDEX IF NOT EXISTS idx_directory_event_inbox_status_received
    ON public.directory_event_inbox(status,received_at);
