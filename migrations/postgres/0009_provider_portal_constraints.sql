SELECT pg_catalog.set_config('search_path', '', false);

UPDATE public.identity_oidc_clients
SET
    allowed_grant_types = '["authorization_code"]'::jsonb,
    refresh_token_ttl_seconds = 0,
    updated_at = now()
WHERE allowed_grant_types <> '["authorization_code"]'::jsonb
   OR refresh_token_ttl_seconds <> 0;

UPDATE public.identity_applications a
SET
    provider_id = NULL,
    updated_by = 'system',
    updated_at = now()
WHERE provider_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM public.identity_providers p
      WHERE p.id = a.provider_id
        AND p.application_id = a.id
        AND p.type = a.provider_type
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_providers_application_unique
    ON public.identity_providers (application_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_providers_id_application_type
    ON public.identity_providers (id, application_id, type);

WITH ranked_active_keys AS (
    SELECT
        id,
        ROW_NUMBER() OVER (PARTITION BY provider_id ORDER BY created_at DESC, id DESC) AS active_rank
    FROM public.identity_provider_signing_keys
    WHERE active = TRUE
)
UPDATE public.identity_provider_signing_keys k
SET
    active = FALSE,
    rotated_at = COALESCE(k.rotated_at, now())
FROM ranked_active_keys r
WHERE k.id = r.id
  AND r.active_rank > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_provider_signing_keys_one_active
    ON public.identity_provider_signing_keys (provider_id)
    WHERE active = TRUE;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'identity_applications_provider_binding_fk'
    ) THEN
        ALTER TABLE public.identity_applications
            ADD CONSTRAINT identity_applications_provider_binding_fk
            FOREIGN KEY (provider_id, id, provider_type)
            REFERENCES public.identity_providers (id, application_id, type);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'identity_oidc_clients_grant_types_reserved_check'
    ) THEN
        ALTER TABLE public.identity_oidc_clients
            ADD CONSTRAINT identity_oidc_clients_grant_types_reserved_check
            CHECK (allowed_grant_types = '["authorization_code"]'::jsonb);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'identity_oidc_clients_refresh_reserved_check'
    ) THEN
        ALTER TABLE public.identity_oidc_clients
            ADD CONSTRAINT identity_oidc_clients_refresh_reserved_check
            CHECK (refresh_token_ttl_seconds = 0);
    END IF;
END $$;
