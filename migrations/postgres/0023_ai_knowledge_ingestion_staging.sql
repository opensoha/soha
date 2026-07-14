CREATE TABLE IF NOT EXISTS public.ai_knowledge_ingestion_document_staging (
    job_id text NOT NULL REFERENCES public.ai_knowledge_ingestion_jobs(id) ON DELETE CASCADE,
    lease_token text NOT NULL,
    document_id text NOT NULL,
    document_payload jsonb NOT NULL,
    chunks_payload jsonb NOT NULL,
    PRIMARY KEY (job_id, document_id)
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_ingestion_staging_lease
    ON public.ai_knowledge_ingestion_document_staging (job_id, lease_token);
