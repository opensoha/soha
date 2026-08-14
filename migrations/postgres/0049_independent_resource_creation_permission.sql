-- Retire the coarse legacy resource-create grant. The global YAML creator is
-- independently assigned; each document still requires its exact create grant.
UPDATE roles
SET permission_keys = (
        (permission_keys::jsonb - 'platform.resource.create') ||
        CASE
          WHEN scope = 'system' AND id IN ('admin', 'ops')
            AND NOT (permission_keys::jsonb @> '["platform.resource-creation.use"]'::jsonb)
          THEN '["platform.resource-creation.use"]'::jsonb
          ELSE '[]'::jsonb
        END
      )::json,
    updated_at = NOW()
WHERE permission_keys::jsonb @> '["platform.resource.create"]'::jsonb
   OR (scope = 'system' AND id IN ('admin', 'ops')
       AND NOT (permission_keys::jsonb @> '["platform.resource-creation.use"]'::jsonb));

-- Empty token permission lists mean uncapped role permissions. Revoke tokens
-- carrying the retired key instead of removing the key and widening access.
UPDATE personal_access_tokens
SET revoked_at = NOW(), updated_at = NOW()
WHERE revoked_at IS NULL
  AND permission_keys::jsonb @> '["platform.resource.create"]'::jsonb;

UPDATE service_account_tokens
SET revoked_at = NOW(), updated_at = NOW()
WHERE revoked_at IS NULL
  AND permission_keys::jsonb @> '["platform.resource.create"]'::jsonb;

DELETE FROM mcp_tool_grants
WHERE permission_keys::jsonb @> '["platform.resource.create"]'::jsonb;
