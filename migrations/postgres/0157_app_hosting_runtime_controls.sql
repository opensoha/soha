SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.hosted_app_runtime_configs (
    app_id text PRIMARY KEY REFERENCES public.hosted_apps(id) ON DELETE CASCADE,
    environment_ciphertexts jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(environment_ciphertexts) = 'object'),
    data_warning_bytes bigint NOT NULL DEFAULT 0 CHECK (data_warning_bytes >= 0),
    visitor_grant jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(visitor_grant) = 'object'),
    visitor_request jsonb CHECK (jsonb_typeof(visitor_request) = 'object'),
    sensitive_publisher_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(sensitive_publisher_ids) = 'array'),
    sensitive_publisher_owner_id text NOT NULL DEFAULT ''
);

ALTER TABLE public.hosted_app_revisions
    ADD COLUMN IF NOT EXISTS runtime_environment jsonb
        CHECK (runtime_environment IS NULL OR (kind = 'dynamic' AND jsonb_typeof(runtime_environment) = 'object'));
