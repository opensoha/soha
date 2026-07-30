CREATE TABLE IF NOT EXISTS public.manifest_sources (
    id text PRIMARY KEY,
    package_id text NOT NULL UNIQUE REFERENCES public.manifest_packages(id) ON DELETE CASCADE,
    mode text NOT NULL DEFAULT 'soha_managed',
    repository_id text REFERENCES public.repositories(id) ON DELETE RESTRICT,
    ref_type text,
    ref_value text NOT NULL DEFAULT '',
    source_path text NOT NULL DEFAULT '',
    include_patterns jsonb NOT NULL DEFAULT '[]'::jsonb,
    exclude_patterns jsonb NOT NULL DEFAULT '[]'::jsonb,
    sync_policy text NOT NULL DEFAULT 'manual',
    poll_interval_seconds integer,
    auto_publish boolean NOT NULL DEFAULT false,
    generation bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT manifest_sources_mode_check CHECK (mode IN ('soha_managed', 'git_synced')),
    CONSTRAINT manifest_sources_ref_type_check CHECK (ref_type IS NULL OR ref_type IN ('branch', 'tag', 'commit')),
    CONSTRAINT manifest_sources_sync_policy_check CHECK (sync_policy IN ('manual', 'webhook', 'poll')),
    CONSTRAINT manifest_sources_poll_interval_check CHECK (
        (sync_policy = 'poll' AND poll_interval_seconds >= 30) OR
        (sync_policy <> 'poll' AND poll_interval_seconds IS NULL)
    ),
    CONSTRAINT manifest_sources_generation_check CHECK (generation >= 1),
    CONSTRAINT manifest_sources_pattern_shape_check CHECK (
        jsonb_typeof(include_patterns) = 'array' AND jsonb_typeof(exclude_patterns) = 'array'
    ),
    CONSTRAINT manifest_sources_mode_fields_check CHECK (
        (mode = 'soha_managed' AND repository_id IS NULL AND ref_type IS NULL AND ref_value = '' AND source_path = '' AND sync_policy = 'manual' AND auto_publish = false) OR
        (mode = 'git_synced' AND repository_id IS NOT NULL AND ref_type IS NOT NULL AND ref_value <> '' AND source_path <> '')
    )
);

CREATE TABLE IF NOT EXISTS public.manifest_source_status (
    source_id text PRIMARY KEY REFERENCES public.manifest_sources(id) ON DELETE CASCADE,
    observed_generation bigint NOT NULL DEFAULT 0,
    last_resolved_commit text NOT NULL DEFAULT '',
    last_successful_sync_at timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT manifest_source_status_generation_check CHECK (observed_generation >= 0)
);

CREATE OR REPLACE FUNCTION public.ensure_manifest_package_source()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO public.manifest_sources (id, package_id, mode, sync_policy, auto_publish, generation, created_at, updated_at)
    VALUES ('manifest-source-' || NEW.id, NEW.id, 'soha_managed', 'manual', false, 1, NEW.created_at, NEW.updated_at)
    ON CONFLICT (package_id) DO NOTHING;
    INSERT INTO public.manifest_source_status (source_id)
    SELECT source.id FROM public.manifest_sources source WHERE source.package_id = NEW.id
    ON CONFLICT (source_id) DO NOTHING;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS manifest_packages_ensure_source ON public.manifest_packages;
CREATE TRIGGER manifest_packages_ensure_source
AFTER INSERT ON public.manifest_packages
FOR EACH ROW EXECUTE FUNCTION public.ensure_manifest_package_source();

INSERT INTO public.manifest_sources (id, package_id, mode, sync_policy, auto_publish, generation, created_at, updated_at)
SELECT 'manifest-source-' || package.id, package.id, 'soha_managed', 'manual', false, 1, package.created_at, package.updated_at
FROM public.manifest_packages package
ON CONFLICT (package_id) DO NOTHING;

INSERT INTO public.manifest_source_status (source_id)
SELECT source.id FROM public.manifest_sources source
ON CONFLICT (source_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.manifest_binding_migration_issues (
    package_id text NOT NULL REFERENCES public.manifest_packages(id) ON DELETE CASCADE,
    binding jsonb NOT NULL,
    reason text NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.manifest_bindings (
    id text PRIMARY KEY,
    package_id text NOT NULL REFERENCES public.manifest_packages(id) ON DELETE CASCADE,
    application_environment_id text NOT NULL REFERENCES public.application_environments(id) ON DELETE RESTRICT,
    environment_key text NOT NULL,
    cluster_id text NOT NULL REFERENCES public.clusters(id) ON DELETE RESTRICT,
    namespace text NOT NULL,
    overlay jsonb NOT NULL DEFAULT '{}'::jsonb,
    rollout_strategy_id text NOT NULL DEFAULT '',
    verification_policy_id text NOT NULL DEFAULT '',
    drift_policy text NOT NULL DEFAULT 'report',
    deletion_policy text NOT NULL DEFAULT 'orphan',
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT manifest_bindings_drift_policy_check CHECK (drift_policy IN ('report', 'repair', 'adopt')),
    CONSTRAINT manifest_bindings_deletion_policy_check CHECK (deletion_policy IN ('orphan', 'delete_managed')),
    CONSTRAINT manifest_bindings_overlay_shape_check CHECK (jsonb_typeof(overlay) = 'object'),
    CONSTRAINT manifest_bindings_version_check CHECK (version >= 1),
    UNIQUE(id, package_id),
    UNIQUE(package_id, application_environment_id, cluster_id, namespace)
);

CREATE INDEX IF NOT EXISTS idx_manifest_bindings_package ON public.manifest_bindings(package_id, created_at);
CREATE INDEX IF NOT EXISTS idx_manifest_bindings_target ON public.manifest_bindings(cluster_id, namespace, enabled);

INSERT INTO public.manifest_binding_migration_issues (package_id, binding, reason)
SELECT package.id, package.bindings, 'legacy bindings payload is not an array'
FROM public.manifest_packages package
WHERE COALESCE(jsonb_typeof(package.bindings), 'null') <> 'array';

WITH legacy AS (
    SELECT package.id AS package_id, binding.value AS binding
    FROM public.manifest_packages package
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(package.bindings) = 'array' THEN package.bindings ELSE '[]'::jsonb END
    ) AS binding(value)
), valid AS (
    SELECT legacy.*
    FROM legacy
    JOIN public.application_environments environment ON environment.id = legacy.binding->>'applicationEnvironmentId'
    JOIN public.clusters cluster ON cluster.id = legacy.binding->>'clusterId'
    WHERE jsonb_typeof(legacy.binding) = 'object'
      AND COALESCE(legacy.binding->>'namespace', '') <> ''
      AND (legacy.binding->'overlay' IS NULL OR jsonb_typeof(legacy.binding->'overlay') = 'object')
)
INSERT INTO public.manifest_bindings (
    id, package_id, application_environment_id, environment_key, cluster_id, namespace, overlay,
    drift_policy, deletion_policy, enabled
)
SELECT
    COALESCE(NULLIF(binding->>'id', ''), 'manifest-binding-' || substr(md5(package_id || ':' || binding::text), 1, 24)),
    package_id,
    binding->>'applicationEnvironmentId',
    COALESCE(NULLIF(binding->>'environmentKey', ''), binding->>'applicationEnvironmentId'),
    binding->>'clusterId',
    binding->>'namespace',
    COALESCE(binding->'overlay', '{}'::jsonb),
    'report',
    'orphan',
    true
FROM valid
ON CONFLICT DO NOTHING;

WITH legacy AS (
    SELECT package.id AS package_id, binding.value AS binding
    FROM public.manifest_packages package
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(package.bindings) = 'array' THEN package.bindings ELSE '[]'::jsonb END
    ) AS binding(value)
), valid AS (
    SELECT
        legacy.package_id,
        legacy.binding,
        COALESCE(NULLIF(legacy.binding->>'id', ''), 'manifest-binding-' || substr(md5(legacy.package_id || ':' || legacy.binding::text), 1, 24)) AS binding_id,
        legacy.binding->>'applicationEnvironmentId' AS application_environment_id,
        legacy.binding->>'clusterId' AS cluster_id,
        legacy.binding->>'namespace' AS namespace
    FROM legacy
    JOIN public.application_environments environment ON environment.id = legacy.binding->>'applicationEnvironmentId'
    JOIN public.clusters cluster ON cluster.id = legacy.binding->>'clusterId'
    WHERE jsonb_typeof(legacy.binding) = 'object'
      AND COALESCE(legacy.binding->>'namespace', '') <> ''
      AND (legacy.binding->'overlay' IS NULL OR jsonb_typeof(legacy.binding->'overlay') = 'object')
), conflicts AS (
    SELECT
        valid.*,
        count(*) OVER (PARTITION BY binding_id) AS id_count,
        count(*) OVER (PARTITION BY package_id, application_environment_id, cluster_id, namespace) AS target_count
    FROM valid
)
INSERT INTO public.manifest_binding_migration_issues (package_id, binding, reason)
SELECT conflicts.package_id, conflicts.binding, 'duplicate binding id or package target'
FROM conflicts
WHERE conflicts.id_count > 1
   OR conflicts.target_count > 1
   OR NOT EXISTS (
       SELECT 1
       FROM public.manifest_bindings binding
       WHERE binding.id = conflicts.binding_id
         AND binding.package_id = conflicts.package_id
         AND binding.application_environment_id = conflicts.application_environment_id
         AND binding.cluster_id = conflicts.cluster_id
         AND binding.namespace = conflicts.namespace
   );

WITH legacy AS (
    SELECT package.id AS package_id, binding.value AS binding
    FROM public.manifest_packages package
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(package.bindings) = 'array' THEN package.bindings ELSE '[]'::jsonb END
    ) AS binding(value)
)
INSERT INTO public.manifest_binding_migration_issues (package_id, binding, reason)
SELECT legacy.package_id, legacy.binding, 'malformed binding or missing application environment, cluster, namespace, or object overlay'
FROM legacy
LEFT JOIN public.application_environments environment ON environment.id = legacy.binding->>'applicationEnvironmentId'
LEFT JOIN public.clusters cluster ON cluster.id = legacy.binding->>'clusterId'
WHERE jsonb_typeof(legacy.binding) <> 'object'
   OR environment.id IS NULL
   OR cluster.id IS NULL
   OR COALESCE(legacy.binding->>'namespace', '') = ''
   OR (legacy.binding->'overlay' IS NOT NULL AND jsonb_typeof(legacy.binding->'overlay') <> 'object');

CREATE TABLE IF NOT EXISTS public.manifest_deployments (
    id text PRIMARY KEY,
    package_id text NOT NULL REFERENCES public.manifest_packages(id) ON DELETE CASCADE,
    binding_id text NOT NULL UNIQUE,
    desired_revision integer NOT NULL,
    desired_digest text NOT NULL,
    generation bigint NOT NULL DEFAULT 1,
    reconcile_policy text NOT NULL DEFAULT 'manual',
    drift_policy text NOT NULL DEFAULT 'report',
    deletion_policy text NOT NULL DEFAULT 'orphan',
    next_reconcile_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT manifest_deployments_revision_check CHECK (desired_revision >= 1),
    CONSTRAINT manifest_deployments_generation_check CHECK (generation >= 1),
    CONSTRAINT manifest_deployments_reconcile_policy_check CHECK (reconcile_policy IN ('manual', 'continuous')),
    CONSTRAINT manifest_deployments_drift_policy_check CHECK (drift_policy IN ('report', 'repair', 'adopt')),
    CONSTRAINT manifest_deployments_deletion_policy_check CHECK (deletion_policy IN ('orphan', 'delete_managed')),
    CONSTRAINT manifest_deployments_binding_package_fk FOREIGN KEY (binding_id, package_id)
        REFERENCES public.manifest_bindings(id, package_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_manifest_deployments_dirty ON public.manifest_deployments(next_reconcile_at, updated_at) WHERE next_reconcile_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS public.manifest_deployment_status (
    deployment_id text PRIMARY KEY REFERENCES public.manifest_deployments(id) ON DELETE CASCADE,
    observed_generation bigint NOT NULL DEFAULT 0,
    applied_revision integer,
    applied_digest text NOT NULL DEFAULT '',
    last_known_good_revision integer,
    phase text NOT NULL DEFAULT 'pending',
    last_reconciled_at timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT manifest_deployment_status_generation_check CHECK (observed_generation >= 0),
    CONSTRAINT manifest_deployment_status_phase_check CHECK (phase IN ('pending', 'waiting_approval', 'reconciling', 'converged', 'drifted', 'degraded', 'deleting'))
);

CREATE TABLE IF NOT EXISTS public.manifest_deployment_conditions (
    deployment_id text NOT NULL REFERENCES public.manifest_deployments(id) ON DELETE CASCADE,
    condition_type text NOT NULL,
    status text NOT NULL,
    reason text NOT NULL DEFAULT '',
    message text NOT NULL DEFAULT '',
    observed_generation bigint NOT NULL DEFAULT 0,
    last_transition_at timestamptz NOT NULL DEFAULT now(),
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (deployment_id, condition_type),
    CONSTRAINT manifest_deployment_condition_status_check CHECK (status IN ('true', 'false', 'unknown')),
    CONSTRAINT manifest_deployment_condition_evidence_shape_check CHECK (jsonb_typeof(evidence_refs) = 'array')
);

CREATE TABLE IF NOT EXISTS public.manifest_resource_inventory (
    deployment_id text NOT NULL REFERENCES public.manifest_deployments(id) ON DELETE CASCADE,
    generation bigint NOT NULL,
    api_version text NOT NULL,
    kind text NOT NULL,
    namespace text NOT NULL DEFAULT '',
    name text NOT NULL,
    uid text NOT NULL DEFAULT '',
    resource_version text NOT NULL DEFAULT '',
    desired_object_digest text NOT NULL DEFAULT '',
    observed_object_digest text NOT NULL DEFAULT '',
    health text NOT NULL DEFAULT '',
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (deployment_id, generation, api_version, kind, namespace, name)
);

CREATE TABLE IF NOT EXISTS public.manifest_validation_records (
    id text PRIMARY KEY,
    revision_id text NOT NULL REFERENCES public.manifest_revisions(id) ON DELETE CASCADE,
    environment_key text NOT NULL DEFAULT '',
    status text NOT NULL,
    summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    artifact_ref text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT manifest_validation_records_status_check CHECK (status IN ('pending', 'passed', 'failed')),
    CONSTRAINT manifest_validation_records_summary_shape_check CHECK (jsonb_typeof(summary) = 'object')
);

ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS origin text NOT NULL DEFAULT 'soha';
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS schema_version text NOT NULL DEFAULT 'v1alpha1';
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS source_commit text NOT NULL DEFAULT '';
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS source_tree_digest text NOT NULL DEFAULT '';
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS source_path text NOT NULL DEFAULT '';
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS renderer text NOT NULL DEFAULT 'raw_yaml';
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS overlay_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE public.manifest_revisions ADD COLUMN IF NOT EXISTS validation_summary jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE public.manifest_revisions ADD CONSTRAINT manifest_revisions_origin_check CHECK (origin IN ('soha', 'git'));
ALTER TABLE public.manifest_revisions ADD CONSTRAINT manifest_revisions_overlay_snapshot_shape_check CHECK (jsonb_typeof(overlay_snapshot) = 'object');
ALTER TABLE public.manifest_revisions ADD CONSTRAINT manifest_revisions_validation_summary_shape_check CHECK (jsonb_typeof(validation_summary) = 'object');

CREATE INDEX IF NOT EXISTS idx_manifest_revisions_digest ON public.manifest_revisions(package_id, digest);
