SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.ai_agent_provider_catalog_state (
    id text PRIMARY KEY,
    revision bigint NOT NULL CHECK (revision > 0),
    digest text NOT NULL,
    catalog jsonb NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ai_agent_provider_catalog_singleton CHECK (id = 'current')
);

CREATE TABLE IF NOT EXISTS public.ai_agent_provider_registry_acks (
    runner_id text PRIMARY KEY,
    revision bigint NOT NULL CHECK (revision > 0),
    active_revision bigint NOT NULL DEFAULT 0 CHECK (active_revision >= 0),
    accepted boolean NOT NULL,
    reason text NOT NULL DEFAULT '',
    acknowledgement jsonb NOT NULL,
    observed_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_agent_provider_registry_acks_observed
    ON public.ai_agent_provider_registry_acks (observed_at DESC, runner_id ASC);
