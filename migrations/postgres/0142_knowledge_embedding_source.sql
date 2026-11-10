ALTER TABLE ai_knowledge_version_chunks
  ADD COLUMN embedding_upstream_id text NOT NULL DEFAULT '',
  ADD COLUMN embedding_provider_kind text NOT NULL DEFAULT '',
  ADD COLUMN embedding_upstream_model text NOT NULL DEFAULT '';

ALTER TABLE ai_knowledge_index_revisions
  ADD COLUMN embedding_upstream_id text NOT NULL DEFAULT '',
  ADD COLUMN embedding_provider_kind text NOT NULL DEFAULT '',
  ADD COLUMN embedding_upstream_model text NOT NULL DEFAULT '';
