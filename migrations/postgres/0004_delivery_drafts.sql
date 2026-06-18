CREATE TABLE IF NOT EXISTS public.delivery_drafts (
    id text NOT NULL PRIMARY KEY,
    source text DEFAULT 'manual'::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    application_draft jsonb DEFAULT '{}'::jsonb NOT NULL,
    services jsonb DEFAULT '[]'::jsonb NOT NULL,
    build_sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    environment_bindings jsonb DEFAULT '[]'::jsonb NOT NULL,
    file_templates jsonb DEFAULT '[]'::jsonb NOT NULL,
    execution_hints jsonb DEFAULT '{}'::jsonb NOT NULL,
    post_create_actions jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_by text,
    confirmed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_delivery_drafts_source_status_updated_at
    ON public.delivery_drafts USING btree (source, status, updated_at DESC);
