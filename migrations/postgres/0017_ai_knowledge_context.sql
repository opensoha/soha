SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_bases (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT '',
    workspace_id text NOT NULL DEFAULT '',
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    owner_id text NOT NULL,
    scope jsonb NOT NULL DEFAULT '{"visibility":"private"}'::jsonb,
    retrieval_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    deleted_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_sources (
    id text PRIMARY KEY,
    knowledge_base_id text NOT NULL REFERENCES public.ai_knowledge_bases(id) ON DELETE CASCADE,
    name text NOT NULL,
    kind text NOT NULL,
    config_ref text NOT NULL DEFAULT '',
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    sync_policy jsonb NOT NULL DEFAULT '{"mode":"manual"}'::jsonb,
    cursor text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'pending',
    last_error text NOT NULL DEFAULT '',
    last_synced_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_documents (
    id text PRIMARY KEY,
    knowledge_base_id text NOT NULL REFERENCES public.ai_knowledge_bases(id) ON DELETE CASCADE,
    source_id text NOT NULL REFERENCES public.ai_knowledge_sources(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    title text NOT NULL,
    uri text NOT NULL DEFAULT '',
    version text NOT NULL,
    content_hash text NOT NULL,
    acl jsonb NOT NULL DEFAULT '{"visibility":"private"}'::jsonb,
    status text NOT NULL DEFAULT 'pending',
    chunk_count integer NOT NULL DEFAULT 0,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    UNIQUE (source_id, external_id)
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_chunks (
    id text PRIMARY KEY,
    knowledge_base_id text NOT NULL REFERENCES public.ai_knowledge_bases(id) ON DELETE CASCADE,
    document_id text NOT NULL REFERENCES public.ai_knowledge_documents(id) ON DELETE CASCADE,
    document_title text NOT NULL,
    ordinal integer NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    location jsonb NOT NULL DEFAULT '{}'::jsonb,
    token_count integer NOT NULL DEFAULT 0,
    acl jsonb NOT NULL DEFAULT '{"visibility":"private"}'::jsonb,
    created_at timestamp without time zone NOT NULL,
    UNIQUE (document_id, ordinal)
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_sync_runs (
    id text PRIMARY KEY,
    knowledge_base_id text NOT NULL REFERENCES public.ai_knowledge_bases(id) ON DELETE CASCADE,
    source_id text NOT NULL REFERENCES public.ai_knowledge_sources(id) ON DELETE CASCADE,
    status text NOT NULL,
    documents_seen integer NOT NULL DEFAULT 0,
    documents_stored integer NOT NULL DEFAULT 0,
    chunks_stored integer NOT NULL DEFAULT 0,
    error text NOT NULL DEFAULT '',
    started_at timestamp without time zone NOT NULL,
    completed_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_index_revisions (
    id text PRIMARY KEY,
    knowledge_base_id text NOT NULL REFERENCES public.ai_knowledge_bases(id) ON DELETE CASCADE,
    revision bigint NOT NULL,
    embedding_model text NOT NULL DEFAULT '',
    chunker_version text NOT NULL,
    document_count integer NOT NULL DEFAULT 0,
    chunk_count integer NOT NULL DEFAULT 0,
    status text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    activated_at timestamp without time zone,
    UNIQUE (knowledge_base_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_bases_status_updated ON public.ai_knowledge_bases (status, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_bases_scope ON public.ai_knowledge_bases USING gin (scope);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_sources_base_updated ON public.ai_knowledge_sources (knowledge_base_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_documents_base_updated ON public.ai_knowledge_documents (knowledge_base_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_documents_acl ON public.ai_knowledge_documents USING gin (acl);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_chunks_base_document ON public.ai_knowledge_chunks (knowledge_base_id, document_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_sync_runs_base_started ON public.ai_knowledge_sync_runs (knowledge_base_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_index_revisions_base_revision ON public.ai_knowledge_index_revisions (knowledge_base_id, revision DESC);
