ALTER TABLE platform_resource_creation_documents ADD COLUMN resource_uid TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN platform_resource_creation_documents.resource_uid IS 'UID from the original Kubernetes create response; empty means unconfirmed or legacy.';
