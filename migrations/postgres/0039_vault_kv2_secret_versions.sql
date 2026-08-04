ALTER TABLE secret_versions ALTER COLUMN ciphertext DROP NOT NULL;
ALTER TABLE secret_versions ADD COLUMN source_type TEXT NOT NULL DEFAULT 'local';
ALTER TABLE secret_versions ADD COLUMN vault_mount TEXT;
ALTER TABLE secret_versions ADD COLUMN vault_path TEXT;
ALTER TABLE secret_versions ADD COLUMN vault_key TEXT;
ALTER TABLE secret_versions ADD COLUMN vault_version INTEGER;

ALTER TABLE secret_versions ADD CONSTRAINT secret_versions_source_check CHECK (
    (source_type = 'local' AND ciphertext IS NOT NULL AND vault_mount IS NULL AND vault_path IS NULL AND vault_key IS NULL AND vault_version IS NULL)
    OR
    (source_type = 'vault_kv2' AND ciphertext IS NULL AND vault_mount IS NOT NULL AND vault_path IS NOT NULL AND vault_key IS NOT NULL AND vault_version > 0)
);
