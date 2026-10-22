-- Fixed bindings remain unchanged. An environment selects a version only after
-- its frozen configuration Plan has passed current authorization and approval.
ALTER TABLE manifest_bindings ADD COLUMN IF NOT EXISTS template_selection jsonb;
ALTER TABLE delivery_plans ADD COLUMN IF NOT EXISTS template_candidate_id text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS template_update_previews (
    id text PRIMARY KEY,
    snapshot jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS template_update_candidates (
    id text PRIMARY KEY,
    preview_id text NOT NULL REFERENCES template_update_previews(id) ON DELETE CASCADE,
    target_id text NOT NULL,
    frozen jsonb NOT NULL,
    applied_plan_id text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS template_update_candidates_preview ON template_update_candidates(preview_id);
CREATE TABLE IF NOT EXISTS template_update_policies (
    id text PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('deployment','build','workflow')),
    template_id text NOT NULL,
    target_id text NOT NULL,
    application_id text NOT NULL,
    subject_id text NOT NULL,
    token_id text NOT NULL DEFAULT '',
    snapshot jsonb NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    attempt_version bigint NOT NULL DEFAULT 0,
    attempt_preview_id text NOT NULL DEFAULT '',
    next_check_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, template_id, target_id)
);
CREATE INDEX IF NOT EXISTS template_update_policy_due ON template_update_policies(next_check_at,id);
