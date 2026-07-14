SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_ingestion_jobs (
    id text PRIMARY KEY,
    knowledge_base_id text NOT NULL REFERENCES public.ai_knowledge_bases(id) ON DELETE CASCADE,
    source_id text NOT NULL REFERENCES public.ai_knowledge_sources(id) ON DELETE CASCADE,
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    stage text NOT NULL CHECK (stage IN (
        'discovering', 'fetching', 'parsing', 'chunking', 'embedding',
        'indexing', 'verifying', 'publishing'
    )),
    status text NOT NULL CHECK (status IN (
        'queued', 'running', 'retry_wait', 'cancelling', 'cancelled', 'failed', 'succeeded'
    )),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 10),
    cancel_requested boolean NOT NULL DEFAULT false,
    checkpoint jsonb NOT NULL,
    principal_snapshot jsonb NOT NULL,
    error_code text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    next_attempt_at timestamp without time zone,
    lease_token text NOT NULL DEFAULT '',
    lease_expires_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    completed_at timestamp without time zone,
    CONSTRAINT chk_ai_knowledge_ingestion_terminal CHECK (
        (status IN ('cancelled', 'failed', 'succeeded') AND completed_at IS NOT NULL) OR
        (status NOT IN ('cancelled', 'failed', 'succeeded') AND completed_at IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_ingestion_claim
    ON public.ai_knowledge_ingestion_jobs (status, next_attempt_at, lease_expires_at, created_at, id)
    WHERE status IN ('queued', 'retry_wait', 'running');

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_ingestion_base_created
    ON public.ai_knowledge_ingestion_jobs (knowledge_base_id, created_at DESC, id);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_ingestion_stages (
    job_id text NOT NULL REFERENCES public.ai_knowledge_ingestion_jobs(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence >= 0),
    stage text NOT NULL,
    status text NOT NULL,
    checkpoint jsonb NOT NULL,
    error_code text NOT NULL DEFAULT '',
    started_at timestamp without time zone NOT NULL,
    completed_at timestamp without time zone,
    PRIMARY KEY (job_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_ingestion_stages_job
    ON public.ai_knowledge_ingestion_stages (job_id, sequence ASC);
