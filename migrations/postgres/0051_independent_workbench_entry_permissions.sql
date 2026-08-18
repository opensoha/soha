-- Preserve each role's previously reachable workbenches while making every
-- workbench entry independently assignable from this release onward.
UPDATE roles AS r
SET permission_keys = (
        SELECT COALESCE(jsonb_agg(permission_key ORDER BY permission_key), '[]'::jsonb)
        FROM (
          SELECT DISTINCT permission_key
          FROM jsonb_array_elements_text(
            r.permission_keys::jsonb
            || CASE
                 WHEN r.permission_keys::jsonb @> '["identity.portal.view"]'::jsonb
                 THEN '["workbench.home.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN r.permission_keys::jsonb @> '["workspace.resource.view"]'::jsonb
                   AND EXISTS (
                     SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                     WHERE p.permission_key = 'overview.view' OR p.permission_key LIKE 'platform.%'
                   )
                 THEN '["workbench.platform.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN r.permission_keys::jsonb @> '["workspace.resource.view"]'::jsonb
                   AND EXISTS (
                     SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                     WHERE p.permission_key LIKE 'docker.%' OR p.permission_key LIKE 'virtualization.%'
                   )
                 THEN '["workbench.compute.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN r.permission_keys::jsonb @> '["workspace.application.view"]'::jsonb
                   AND EXISTS (
                     SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                     WHERE p.permission_key LIKE 'delivery.%'
                   )
                 THEN '["workbench.delivery.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN r.permission_keys::jsonb @> '["workspace.resource.view"]'::jsonb
                   AND EXISTS (
                     SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                     WHERE p.permission_key LIKE 'ai.%' OR p.permission_key LIKE 'observe.ai.%' OR p.permission_key LIKE 'settings.ai.%'
                   )
                 THEN '["workbench.ai.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN r.permission_keys::jsonb @> '["workspace.resource.view"]'::jsonb
                   AND EXISTS (
                     SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                     WHERE p.permission_key LIKE 'observe.%' AND p.permission_key NOT LIKE 'observe.ai.%'
                   )
                 THEN '["workbench.monitoring.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN EXISTS (
                   SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                   WHERE p.permission_key LIKE 'access.%' OR p.permission_key LIKE 'plugin.%' OR p.permission_key LIKE 'secret.%'
                      OR p.permission_key LIKE 'settings.%' OR p.permission_key LIKE 'system.%'
                 )
                 THEN '["workbench.settings.view"]'::jsonb ELSE '[]'::jsonb
               END
            || CASE
                 WHEN EXISTS (
                   SELECT 1 FROM jsonb_array_elements_text(r.permission_keys::jsonb) AS p(permission_key)
                   WHERE (p.permission_key LIKE 'identity.%' AND p.permission_key <> 'identity.portal.view')
                      OR p.permission_key LIKE 'software.%'
                 )
                 THEN '["workbench.security.view"]'::jsonb ELSE '[]'::jsonb
               END
          ) AS expanded(permission_key)
        ) AS distinct_permissions
      )::json,
    updated_at = NOW();

-- Non-empty token permission lists cap role permissions. Add all entry keys to
-- the cap; the role intersection still decides which entries are effective.
UPDATE personal_access_tokens AS token
SET permission_keys = (
        SELECT jsonb_agg(permission_key ORDER BY permission_key)
        FROM (
          SELECT DISTINCT permission_key
          FROM jsonb_array_elements_text(
            token.permission_keys::jsonb || '["workbench.ai.view","workbench.compute.view","workbench.delivery.view","workbench.home.view","workbench.monitoring.view","workbench.platform.view","workbench.security.view","workbench.settings.view"]'::jsonb
          ) AS expanded(permission_key)
        ) AS distinct_permissions
      )::json,
    updated_at = NOW()
WHERE token.permission_keys::jsonb <> '[]'::jsonb;

UPDATE service_account_tokens AS token
SET permission_keys = (
        SELECT jsonb_agg(permission_key ORDER BY permission_key)
        FROM (
          SELECT DISTINCT permission_key
          FROM jsonb_array_elements_text(
            token.permission_keys::jsonb || '["workbench.ai.view","workbench.compute.view","workbench.delivery.view","workbench.home.view","workbench.monitoring.view","workbench.platform.view","workbench.security.view","workbench.settings.view"]'::jsonb
          ) AS expanded(permission_key)
        ) AS distinct_permissions
      )::json,
    updated_at = NOW()
WHERE token.permission_keys::jsonb <> '[]'::jsonb;
