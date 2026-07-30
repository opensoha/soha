CREATE TABLE IF NOT EXISTS public.manifest_packages (
    id text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    application_id text NOT NULL REFERENCES public.applications(id) ON DELETE RESTRICT,
    business_line_id text NOT NULL DEFAULT '',
    renderer text NOT NULL DEFAULT 'raw_yaml',
    status text NOT NULL DEFAULT 'draft',
    current_revision integer NOT NULL DEFAULT 0,
    files jsonb NOT NULL DEFAULT '[]'::jsonb,
    bindings jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by text NOT NULL DEFAULT '',
    updated_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT manifest_packages_renderer_check CHECK (renderer IN ('raw_yaml', 'kustomize')),
    CONSTRAINT manifest_packages_status_check CHECK (status IN ('draft', 'published'))
);

CREATE INDEX IF NOT EXISTS idx_manifest_packages_application ON public.manifest_packages(application_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_manifest_packages_bindings ON public.manifest_packages USING gin(bindings);

CREATE TABLE IF NOT EXISTS public.manifest_revisions (
    id text PRIMARY KEY,
    package_id text NOT NULL REFERENCES public.manifest_packages(id) ON DELETE CASCADE,
    version integer NOT NULL,
    digest text NOT NULL,
    note text NOT NULL DEFAULT '',
    files jsonb NOT NULL DEFAULT '[]'::jsonb,
    bindings jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(package_id, version)
);

CREATE INDEX IF NOT EXISTS idx_manifest_revisions_package ON public.manifest_revisions(package_id, version DESC);
