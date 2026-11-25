SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.hosted_app_managed_images (
    revision_id text PRIMARY KEY,
    image_ref text NOT NULL UNIQUE CHECK (image_ref <> ''),
    cleanup_ready_at timestamptz,
    delete_started boolean NOT NULL DEFAULT false,
    claim_id text NOT NULL DEFAULT '',
    claim_expires_at timestamptz,
    deleted_at timestamptz CHECK (deleted_at IS NULL OR delete_started)
);

CREATE INDEX IF NOT EXISTS idx_hosted_app_managed_images_pending
    ON public.hosted_app_managed_images (cleanup_ready_at, revision_id)
    WHERE deleted_at IS NULL AND cleanup_ready_at IS NOT NULL;

ALTER TABLE public.hosted_app_deleted_runtime_cleanup
    ADD COLUMN IF NOT EXISTS image_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS image_key text GENERATED ALWAYS AS (
        regexp_replace(lower(split_part(image_ref, '/', 1)) || substr(image_ref, strpos(image_ref, '/')), ':[^/@]+@', '@')
    ) STORED;

ALTER TABLE public.hosted_app_revisions
    ADD COLUMN IF NOT EXISTS image_key text GENERATED ALWAYS AS (
        regexp_replace(lower(split_part(image_ref, '/', 1)) || substr(image_ref, strpos(image_ref, '/')), ':[^/@]+@', '@')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_hosted_app_deleted_runtime_cleanup_image
    ON public.hosted_app_deleted_runtime_cleanup (image_key);

CREATE INDEX IF NOT EXISTS idx_hosted_app_revisions_image_key
    ON public.hosted_app_revisions (image_key);
