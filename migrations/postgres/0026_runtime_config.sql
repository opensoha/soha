CREATE TABLE IF NOT EXISTS public.runtime_config_state (
    id text PRIMARY KEY,
    version bigint DEFAULT 0 NOT NULL,
    active_revision_id text,
    overrides jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_by text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

INSERT INTO public.runtime_config_state (id, version, overrides, updated_by)
VALUES ('default', 0, '{}'::jsonb, 'system')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.runtime_config_revisions (
    id text PRIMARY KEY,
    version bigint UNIQUE NOT NULL,
    status text NOT NULL,
    changes jsonb DEFAULT '[]'::jsonb NOT NULL,
    snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    actor text NOT NULL,
    reason text,
    rollback_of_revision_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_config_revisions_created_at
    ON public.runtime_config_revisions (created_at DESC);

CREATE TABLE IF NOT EXISTS public.runtime_config_applications (
    id text PRIMARY KEY,
    revision_id text NOT NULL REFERENCES public.runtime_config_revisions(id) ON DELETE CASCADE,
    version bigint NOT NULL,
    status text NOT NULL,
    items jsonb DEFAULT '[]'::jsonb NOT NULL,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_config_applications_revision
    ON public.runtime_config_applications (revision_id);
