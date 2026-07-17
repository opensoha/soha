ALTER TABLE public.scope_grants
    ADD COLUMN IF NOT EXISTS scope_type text NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS cluster_ids json NOT NULL DEFAULT '[]'::json,
    ADD COLUMN IF NOT EXISTS namespaces json NOT NULL DEFAULT '[]'::json,
    ADD COLUMN IF NOT EXISTS namespace_selector text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS resource_groups json NOT NULL DEFAULT '[]'::json,
    ADD COLUMN IF NOT EXISTS resource_kinds json NOT NULL DEFAULT '[]'::json;

ALTER TABLE public.scope_grants
    DROP CONSTRAINT IF EXISTS scope_grants_scope_type_check;

ALTER TABLE public.scope_grants
    ADD CONSTRAINT scope_grants_scope_type_check
    CHECK (scope_type IN ('legacy', 'delivery', 'platform'));

CREATE INDEX IF NOT EXISTS idx_scope_grants_platform_subject
    ON public.scope_grants (subject_type, subject_id, scope_type)
    WHERE enabled = true;
