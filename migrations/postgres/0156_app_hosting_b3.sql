SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.hosted_app_revisions
    ADD COLUMN IF NOT EXISTS source_key text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS build_template text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS image_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS build_phase text NOT NULL DEFAULT ''
        CHECK (build_phase IN ('', 'queued', 'building', 'starting')),
    ADD COLUMN IF NOT EXISTS database_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS data_schema integer NOT NULL DEFAULT 0
        CHECK ((database_enabled AND data_schema IN (1, 2)) OR (NOT database_enabled AND data_schema = 0)),
    ADD COLUMN IF NOT EXISTS claim_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS next_check_at timestamptz,
    ADD COLUMN IF NOT EXISTS claim_expires_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_hosted_app_revisions_dynamic_due
    ON public.hosted_app_revisions (next_check_at, id)
    WHERE kind = 'dynamic' AND status IN ('validating', 'failed', 'evicted') AND next_check_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS public.hosted_app_deleted_runtime_cleanup (
    revision_id text PRIMARY KEY,
    app_id text NOT NULL,
    storage_integration_id text NOT NULL,
    storage_prefix text NOT NULL,
    source_key text NOT NULL,
    database_enabled boolean NOT NULL,
    next_check_at timestamptz NOT NULL,
    claim_id text NOT NULL DEFAULT '',
    claim_expires_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_hosted_app_deleted_runtime_cleanup_due
    ON public.hosted_app_deleted_runtime_cleanup (next_check_at, revision_id);
