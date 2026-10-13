-- Keep the existing current-document/chunk tables readable by older programs.
-- Immutable snapshots and the current pointer are published in the same transaction.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE ai_knowledge_documents ADD COLUMN IF NOT EXISTS current_version_id text;
CREATE TABLE IF NOT EXISTS ai_knowledge_document_versions (
    id text PRIMARY KEY,
    document_id text NOT NULL REFERENCES ai_knowledge_documents(id) ON DELETE CASCADE,
    index_revision_id text NOT NULL DEFAULT '',
    title text NOT NULL,
    uri text NOT NULL DEFAULT '',
    version text NOT NULL,
    content_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_knowledge_document_versions_document
ON ai_knowledge_document_versions(document_id,created_at);

CREATE TABLE IF NOT EXISTS ai_knowledge_version_chunks (
    id text PRIMARY KEY,
    document_version_id text NOT NULL REFERENCES ai_knowledge_document_versions(id) ON DELETE CASCADE,
    ordinal integer NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    location jsonb NOT NULL,
    token_count integer NOT NULL,
    embedding_model text NOT NULL DEFAULT '',
    embedding_route text NOT NULL DEFAULT '',
    embedding vector,
    embedding_dimension integer GENERATED ALWAYS AS (vector_dims(embedding)) STORED,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple',content)) STORED,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(document_version_id,ordinal),
    CHECK (embedding IS NULL OR vector_dims(embedding) BETWEEN 1 AND 4096)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_version_chunks_lexical ON ai_knowledge_version_chunks USING gin(search_vector);
CREATE INDEX IF NOT EXISTS idx_knowledge_version_chunks_trigram ON ai_knowledge_version_chunks USING gin(content gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_knowledge_version_chunks_vector1536 ON ai_knowledge_version_chunks
USING hnsw ((embedding::vector(1536)) vector_cosine_ops) WHERE embedding_dimension=1536;
CREATE INDEX IF NOT EXISTS idx_knowledge_version_chunks_vector3072 ON ai_knowledge_version_chunks
USING hnsw ((embedding::halfvec(3072)) halfvec_cosine_ops) WHERE embedding_dimension=3072;

CREATE OR REPLACE FUNCTION reject_knowledge_version_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'knowledge versions are immutable; publish a new version';
END;
$$;
DROP TRIGGER IF EXISTS knowledge_document_version_immutable ON ai_knowledge_document_versions;
CREATE TRIGGER knowledge_document_version_immutable BEFORE UPDATE ON ai_knowledge_document_versions
FOR EACH ROW EXECUTE FUNCTION reject_knowledge_version_update();
DROP TRIGGER IF EXISTS knowledge_version_chunk_immutable ON ai_knowledge_version_chunks;
CREATE TRIGGER knowledge_version_chunk_immutable BEFORE UPDATE ON ai_knowledge_version_chunks
FOR EACH ROW EXECUTE FUNCTION reject_knowledge_version_update();

-- Historical records stay on lexical retrieval until the existing ingestion queue rebuilds them.
