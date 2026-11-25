SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.app_hosting_settings
    ADD COLUMN IF NOT EXISTS organization_visibility_policy text;

UPDATE public.app_hosting_settings
SET organization_visibility_policy = CASE
    WHEN allow_organization_visibility THEN 'allow_direct'
    ELSE 'disabled'
END
WHERE organization_visibility_policy IS NULL;

ALTER TABLE public.app_hosting_settings
    ALTER COLUMN organization_visibility_policy SET DEFAULT 'allow_direct';

ALTER TABLE public.app_hosting_settings
    ALTER COLUMN organization_visibility_policy SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_constraint
        WHERE conname = 'app_hosting_settings_org_visibility_policy_check'
    ) THEN
        ALTER TABLE public.app_hosting_settings
            ADD CONSTRAINT app_hosting_settings_org_visibility_policy_check
            CHECK (organization_visibility_policy IN ('allow_direct', 'require_approval', 'disabled'));
    END IF;
END
$$;

ALTER TABLE public.app_hosting_settings
    ADD COLUMN IF NOT EXISTS policy_version bigint NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_constraint
        WHERE conname = 'app_hosting_settings_policy_version_check'
    ) THEN
        ALTER TABLE public.app_hosting_settings
            ADD CONSTRAINT app_hosting_settings_policy_version_check
            CHECK (policy_version >= 1);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS public.hosted_app_visibility_approvals (
    id text PRIMARY KEY,
    app_id text NOT NULL REFERENCES public.hosted_apps(id) ON DELETE CASCADE,
    app_name text NOT NULL,
    app_dns_label text NOT NULL,
    requester_user_id text NOT NULL,
    current_visibility jsonb NOT NULL,
    requested_visibility jsonb NOT NULL,
    base_route_generation bigint NOT NULL CHECK (base_route_generation >= 1),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    status text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn', 'invalidated')),
    reason text NOT NULL DEFAULT '',
    reviewer_user_id text NOT NULL DEFAULT '',
    review_comment text NOT NULL DEFAULT '',
    invalidation_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_hosted_app_visibility_approvals_one_pending
    ON public.hosted_app_visibility_approvals (app_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_hosted_app_visibility_approvals_status_created_at
    ON public.hosted_app_visibility_approvals (status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_hosted_app_visibility_approvals_requester_created_at
    ON public.hosted_app_visibility_approvals (requester_user_id, created_at DESC);

DO $$
BEGIN
    IF pg_catalog.to_regclass('public.menus') IS NOT NULL THEN
        INSERT INTO public.menus (
            id, parent_id, path, label_zh, label_en, icon_key, section,
            sort_order, enabled, created_at, updated_at
        )
        SELECT candidate.*
        FROM (VALUES
            ('identity-app-hosting', 'identity', '/internal-workbench/app-hosting', '应用托管', 'App Hosting', 'globe', 'provider', 15, true, NOW(), NOW())
        ) AS candidate(
            id, parent_id, path, label_zh, label_en, icon_key, section,
            sort_order, enabled, created_at, updated_at
        )
        WHERE EXISTS (SELECT 1 FROM public.menus AS parent WHERE parent.id = candidate.parent_id)
          AND NOT EXISTS (SELECT 1 FROM public.menus AS existing WHERE existing.path = candidate.path)
        ON CONFLICT (id) DO NOTHING;
    END IF;
END
$$;
