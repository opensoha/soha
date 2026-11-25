SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.hosted_app_static_artifact_cleanup (
    revision_id text PRIMARY KEY,
    app_id text NOT NULL,
    storage_integration_id text NOT NULL,
    storage_prefix text NOT NULL,
    manifest_json jsonb NOT NULL,
    queued_at timestamptz NOT NULL,
    available_at timestamptz,
    claim_id text NOT NULL DEFAULT '',
    claim_expires_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_hosted_app_static_artifact_cleanup_due
    ON public.hosted_app_static_artifact_cleanup (queued_at, revision_id);

CREATE INDEX IF NOT EXISTS idx_hosted_app_static_upload_expiry
    ON public.hosted_app_revisions (created_at, id)
    WHERE kind = 'static' AND status = 'uploading';

INSERT INTO public.hosted_app_static_artifact_cleanup (
    revision_id, app_id, storage_integration_id, storage_prefix, manifest_json, queued_at, available_at
)
SELECT rev.id, rev.app_id, rev.storage_integration_id, rev.storage_prefix, rev.manifest_json, rev.created_at,
    CASE WHEN rev.status = 'failed' THEN rev.created_at + interval '6 minutes' END
FROM public.hosted_app_revisions rev JOIN public.hosted_apps app ON app.id = rev.app_id
WHERE rev.kind = 'static' AND rev.status IN ('evicted', 'failed') AND rev.storage_integration_id <> ''
    AND rev.manifest_json <> '[]'::jsonb AND rev.id <> app.active_revision_id
ON CONFLICT (revision_id) DO NOTHING;
