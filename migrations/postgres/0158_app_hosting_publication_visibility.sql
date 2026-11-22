SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.hosted_app_revisions
    ADD COLUMN IF NOT EXISTS publish_visibility jsonb
        CHECK (publish_visibility IS NULL OR jsonb_typeof(publish_visibility) IN ('object', 'null'));
