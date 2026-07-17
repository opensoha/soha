CREATE TABLE IF NOT EXISTS public.platform_resource_creation_batches (
    id text PRIMARY KEY,
    actor_id text NOT NULL,
    cluster_id text NOT NULL,
    idempotency_key text NOT NULL,
    content_hash text NOT NULL,
    status text NOT NULL DEFAULT 'running',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CONSTRAINT platform_resource_creation_batches_identity_unique
        UNIQUE (actor_id, cluster_id, idempotency_key),
    CONSTRAINT platform_resource_creation_batches_status_check
        CHECK (status IN ('running', 'succeeded', 'failed')),
    CONSTRAINT platform_resource_creation_batches_content_hash_check
        CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT platform_resource_creation_batches_identity_check
        CHECK (btrim(actor_id) <> '' AND btrim(cluster_id) <> '' AND btrim(idempotency_key) <> '')
);

CREATE TABLE IF NOT EXISTS public.platform_resource_creation_documents (
    batch_id text NOT NULL REFERENCES public.platform_resource_creation_batches(id) ON DELETE CASCADE,
    document_index integer NOT NULL,
    api_version text NOT NULL,
    kind text NOT NULL,
    resource_name text NOT NULL DEFAULT '',
    namespace text NOT NULL DEFAULT '',
    namespaced boolean NOT NULL,
    status text NOT NULL DEFAULT 'not_started',
    error_code text NOT NULL DEFAULT '',
    error_summary text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (batch_id, document_index),
    CONSTRAINT platform_resource_creation_documents_status_check
		CHECK (status IN ('not_started', 'succeeded', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_platform_resource_creation_batches_actor_created
    ON public.platform_resource_creation_batches (actor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_platform_resource_creation_batches_cluster_created
    ON public.platform_resource_creation_batches (cluster_id, created_at DESC);
