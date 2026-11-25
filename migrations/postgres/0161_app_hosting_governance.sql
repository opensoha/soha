ALTER TABLE public.app_hosting_settings
    ADD COLUMN IF NOT EXISTS governance jsonb
        CHECK (jsonb_typeof(governance) = 'object');

ALTER TABLE public.hosted_app_revisions
    ADD COLUMN IF NOT EXISTS runtime_limits jsonb
        CHECK (runtime_limits IS NULL OR (kind = 'dynamic' AND jsonb_typeof(runtime_limits) = 'object'));

CREATE TABLE IF NOT EXISTS public.hosted_app_data_usage_checks (
    app_id text PRIMARY KEY REFERENCES public.hosted_apps(id) ON DELETE CASCADE,
    claim_id text NOT NULL,
    next_check_at timestamptz NOT NULL,
    warning_id text NOT NULL
);
