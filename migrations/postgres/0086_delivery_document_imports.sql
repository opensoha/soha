CREATE TABLE IF NOT EXISTS delivery_document_imports (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    candidate_digest TEXT NOT NULL CHECK (candidate_digest ~ '^sha256:[a-f0-9]{64}$'),
    candidates JSONB NOT NULL CHECK (jsonb_typeof(candidates) = 'array'),
    expires_at TIMESTAMPTZ NOT NULL,
    idempotency_key TEXT,
    result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    CHECK ((result IS NULL AND applied_at IS NULL AND idempotency_key IS NULL)
        OR (result IS NOT NULL AND applied_at IS NOT NULL AND length(idempotency_key) BETWEEN 8 AND 128))
);
CREATE UNIQUE INDEX IF NOT EXISTS delivery_document_import_actor_key
    ON delivery_document_imports(actor_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS delivery_document_import_expiry
    ON delivery_document_imports(expires_at) WHERE result IS NULL;
