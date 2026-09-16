-- Legacy repositories remain usable without a provider API connection.
-- Mapping is explicit; never select credentials by URL or repository name.
ALTER TABLE repositories ADD COLUMN source_connection_id TEXT;
ALTER TABLE repositories ADD COLUMN provider_repository_id TEXT;
ALTER TABLE repositories ADD CONSTRAINT repository_source_mapping_pair CHECK (
    (source_connection_id IS NULL AND provider_repository_id IS NULL) OR
    (length(source_connection_id) BETWEEN 1 AND 200 AND length(provider_repository_id) BETWEEN 1 AND 512
     AND source_connection_id IS NOT NULL AND provider_repository_id IS NOT NULL)
);
