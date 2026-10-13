-- Optional frozen proof preserves all Docker rows and lease/data identities.
ALTER TABLE ai_personal_environments ADD COLUMN vm_identity JSONB;
ALTER TABLE ai_personal_environments ADD CONSTRAINT personal_vm_identity_backend
    CHECK (backend='vm' OR vm_identity IS NULL);
-- Retain ownership through unknown/deletion states; reassigning data is separate.
CREATE UNIQUE INDEX personal_vm_data_single_owner
    ON ai_personal_environments ((vm_identity->>'dataIdentityHash')) WHERE backend='vm';
CREATE UNIQUE INDEX personal_vm_data_ref_single_owner
    ON ai_personal_environments (data_ref) WHERE backend='vm';
