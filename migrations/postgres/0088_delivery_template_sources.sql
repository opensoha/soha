CREATE TABLE delivery_template_sources (
    id TEXT PRIMARY KEY,
    generation BIGINT NOT NULL CHECK (generation > 0),
    definition JSONB NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE delivery_template_sync_runs (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES delivery_template_sources(id),
    actor_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    apply_key TEXT CHECK (length(apply_key) BETWEEN 8 AND 128),
    source_generation BIGINT NOT NULL CHECK (source_generation > 0),
    repository_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'ready', 'invalid', 'failed', 'applied', 'stale')),
    definition JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_id, actor_id, idempotency_key)
);
CREATE UNIQUE INDEX delivery_template_sync_apply_key
    ON delivery_template_sync_runs(source_id, actor_id, apply_key) WHERE apply_key IS NOT NULL;
CREATE INDEX delivery_template_sync_history ON delivery_template_sync_runs(source_id, created_at DESC, id);

CREATE TABLE delivery_template_source_objects (
    source_id TEXT NOT NULL REFERENCES delivery_template_sources(id),
    kind TEXT NOT NULL CHECK (kind IN ('BuildTemplate', 'DeploymentTemplate', 'WorkflowTemplate', 'Workflow')),
    object_id TEXT NOT NULL,
    document_key TEXT NOT NULL,
    association JSONB NOT NULL,
    imported_document JSONB NOT NULL,
    PRIMARY KEY (kind, object_id),
    UNIQUE (source_id, kind, document_key)
);

-- Deliberately no cascading foreign keys: source removal must retain history.
CREATE TABLE delivery_document_provenance (
    kind TEXT NOT NULL CHECK (kind IN ('BuildTemplate', 'DeploymentTemplate', 'WorkflowTemplate', 'Workflow')),
    object_id TEXT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    provenance JSONB NOT NULL,
    PRIMARY KEY (kind, object_id, version)
);
CREATE TRIGGER delivery_document_provenance_immutable BEFORE UPDATE OR DELETE ON delivery_document_provenance
FOR EACH ROW EXECUTE FUNCTION reject_catalog_template_version_mutation();
