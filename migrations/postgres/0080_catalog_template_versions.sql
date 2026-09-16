ALTER TABLE build_templates
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN published_version BIGINT NOT NULL DEFAULT 0 CHECK (published_version >= 0),
    ADD COLUMN publication_state TEXT NOT NULL DEFAULT 'draft' CHECK (publication_state IN ('draft', 'published', 'deprecated')),
    ADD COLUMN content_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_templates
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN published_version BIGINT NOT NULL DEFAULT 0 CHECK (published_version >= 0),
    ADD COLUMN publication_state TEXT NOT NULL DEFAULT 'draft' CHECK (publication_state IN ('draft', 'published', 'deprecated')),
    ADD COLUMN content_digest TEXT NOT NULL DEFAULT '';

CREATE TABLE catalog_template_versions (
    kind TEXT NOT NULL CHECK (kind IN ('build', 'workflow')),
    template_id TEXT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    snapshot JSONB NOT NULL,
    content_digest TEXT NOT NULL CHECK (content_digest ~ '^sha256:[a-f0-9]{64}$'),
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (kind, template_id, version)
);

-- Use the same PostgreSQL JSONB representation for migrated and new snapshots.
CREATE FUNCTION catalog_template_content_digest(snapshot JSONB) RETURNS TEXT
LANGUAGE SQL IMMUTABLE STRICT AS $$
    SELECT 'sha256:' || encode(sha256(convert_to((snapshot - ARRAY[
        'revision', 'publishedVersion', 'publicationState', 'contentDigest', 'createdAt', 'updatedAt'
    ])::TEXT, 'UTF8')), 'hex');
$$;

INSERT INTO catalog_template_versions (kind, template_id, version, snapshot, content_digest, published_at)
SELECT 'build', id, 1, body, catalog_template_content_digest(body), updated_at
FROM build_templates b CROSS JOIN LATERAL (
    SELECT jsonb_strip_nulls(jsonb_build_object(
        'id', b.id, 'key', b.template_key, 'name', b.name, 'description', b.description,
        'builderKind', b.builder_kind, 'dockerfileTemplate', b.dockerfile_template,
        'buildCommands', b.build_commands, 'variableSchema', b.variable_schema,
        'defaultVariables', b.default_variables, 'enabled', b.enabled,
        'createdAt', b.created_at, 'updatedAt', b.updated_at,
        'revision', 1, 'publishedVersion', 1, 'publicationState', 'published'
    )) AS body
) s;
INSERT INTO catalog_template_versions (kind, template_id, version, snapshot, content_digest, published_at)
SELECT 'workflow', id, 1, body, catalog_template_content_digest(body), updated_at
FROM workflow_templates w CROSS JOIN LATERAL (
    SELECT jsonb_strip_nulls(jsonb_build_object(
        'id', w.id, 'key', w.template_key, 'name', w.name, 'description', w.description,
        'category', w.category, 'definition', w.definition, 'enabled', w.enabled,
        'createdAt', w.created_at AT TIME ZONE 'UTC', 'updatedAt', w.updated_at AT TIME ZONE 'UTC',
        'revision', 1, 'publishedVersion', 1, 'publicationState', 'published'
    )) AS body
) s;
UPDATE build_templates b SET published_version = 1, publication_state = 'published', content_digest = v.content_digest
FROM catalog_template_versions v WHERE v.kind = 'build' AND v.template_id = b.id;
UPDATE workflow_templates w SET published_version = 1, publication_state = 'published', content_digest = v.content_digest
FROM catalog_template_versions v WHERE v.kind = 'workflow' AND v.template_id = w.id;

ALTER TABLE application_environments ADD COLUMN workflow_template_version BIGINT NOT NULL DEFAULT 0 CHECK (workflow_template_version >= 0);
UPDATE application_environments SET workflow_template_version = 1 WHERE workflow_template_id IS NOT NULL;
UPDATE application_build_sources SET config = jsonb_set(COALESCE(config, '{}'::jsonb), '{buildTemplateVersion}', '1')
WHERE NULLIF(config->>'buildTemplateId', '') IS NOT NULL;

-- History is append-only, including for administrative catalog deletion.
CREATE FUNCTION reject_catalog_template_version_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'published template versions are immutable';
END;
$$;
CREATE TRIGGER catalog_template_version_immutable BEFORE UPDATE OR DELETE ON catalog_template_versions
FOR EACH ROW EXECUTE FUNCTION reject_catalog_template_version_mutation();
