-- Only the built-in platform administrator receives the new right on upgrade.
UPDATE roles
SET permission_keys = (permission_keys::jsonb || '["access.users.inherit-heya-assets"]'::jsonb)::json,
    updated_at = now()
WHERE id = 'admin' AND scope = 'system'
  AND NOT (permission_keys::jsonb ? 'access.users.inherit-heya-assets');
