-- +goose Up
ALTER TABLE public.network_nas_authorizations ADD COLUMN IF NOT EXISTS admission_inputs_hash text NOT NULL DEFAULT '';
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_floor_profile text NOT NULL DEFAULT '';
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_inputs_hash text NOT NULL DEFAULT '';
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_radius_attributes jsonb;
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_revision bigint NOT NULL DEFAULT 0;
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_command_id text NOT NULL DEFAULT '';
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_attempts integer NOT NULL DEFAULT 0 CHECK (admission_attempts BETWEEN 0 AND 3);
ALTER TABLE public.network_runtime_sessions ADD COLUMN IF NOT EXISTS admission_restriction_reason text NOT NULL DEFAULT '';

-- +goose Down
-- Retain security restrictions and decision evidence when reverting application code.
