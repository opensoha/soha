SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.directory_connection_credentials (
    connection_id text PRIMARY KEY REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    username_encrypted text NOT NULL DEFAULT '', password_encrypted text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
