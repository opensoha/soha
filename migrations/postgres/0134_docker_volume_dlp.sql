CREATE TABLE public.docker_volume_dlp_policies (
    project_id TEXT PRIMARY KEY REFERENCES public.docker_projects(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL CHECK (revision >= 1),
    draft JSONB NOT NULL CHECK (jsonb_typeof(draft) = 'object'),
    published JSONB,
    published_version BIGINT NOT NULL DEFAULT 0 CHECK (published_version >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((published IS NULL AND published_version = 0) OR
           (published IS NOT NULL AND jsonb_typeof(published) = 'object' AND published_version > 0 AND published_version <= revision))
);

-- Intentionally independent of the project FK: deleting a project preserves
-- content-free security history. No file paths, content or matched excerpts.
CREATE TABLE public.docker_volume_dlp_events (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('transfer','policy_saved','policy_published')),
    metadata JSONB NOT NULL CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX docker_volume_dlp_events_project_time ON public.docker_volume_dlp_events(project_id,created_at DESC,id DESC);
