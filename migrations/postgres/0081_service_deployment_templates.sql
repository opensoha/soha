ALTER TABLE catalog_template_versions DROP CONSTRAINT catalog_template_versions_kind_check;
ALTER TABLE catalog_template_versions ADD CONSTRAINT catalog_template_versions_kind_check
    CHECK (kind IN ('build', 'workflow', 'deployment'));

CREATE TABLE deployment_templates (
    id TEXT PRIMARY KEY,
    template_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    spec JSONB NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    published_version BIGINT NOT NULL DEFAULT 0 CHECK (published_version >= 0),
    publication_state TEXT NOT NULL DEFAULT 'draft' CHECK (publication_state IN ('draft', 'published', 'deprecated')),
    content_digest TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE application_services
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN deployment_template JSONB;
ALTER TABLE manifest_bindings ADD COLUMN template_parameters JSONB NOT NULL DEFAULT '{}';
