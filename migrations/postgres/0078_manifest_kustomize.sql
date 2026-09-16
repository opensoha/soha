SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.manifest_bindings
  ADD COLUMN kustomize JSONB;

ALTER TABLE public.manifest_bindings
  ADD CONSTRAINT manifest_bindings_kustomize_object
    CHECK (kustomize IS NULL OR jsonb_typeof(kustomize) = 'object');
ALTER TABLE public.delivery_plans
  ADD COLUMN IF NOT EXISTS manifest_snapshots JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.manifest_deployments
  ADD COLUMN IF NOT EXISTS delivery_snapshot JSONB;
