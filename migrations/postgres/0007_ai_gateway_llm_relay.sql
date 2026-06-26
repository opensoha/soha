SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.ai_gateway_llm_upstreams (
    id text PRIMARY KEY,
    name text NOT NULL,
    provider_kind text NOT NULL,
    base_url text NOT NULL,
    api_key_ciphertext text NOT NULL,
    api_key_prefix text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    weight integer DEFAULT 1 NOT NULL,
    timeout_seconds integer DEFAULT 120 NOT NULL,
    stream_timeout_seconds integer DEFAULT 300 NOT NULL,
    max_concurrency integer DEFAULT 0 NOT NULL,
    supported_models json DEFAULT '[]'::json NOT NULL,
    default_headers json DEFAULT '{}'::json NOT NULL,
    proxy_url text,
    health json DEFAULT '{}'::json NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

CREATE TABLE IF NOT EXISTS public.ai_gateway_llm_model_routes (
    id text PRIMARY KEY,
    public_model text NOT NULL,
    provider_kind text,
    upstream_id text,
    upstream_model text NOT NULL,
    route_group text,
    priority integer DEFAULT 100 NOT NULL,
    weight integer DEFAULT 1 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    transform_policy json DEFAULT '{}'::json NOT NULL,
    fallback_policy json DEFAULT '{}'::json NOT NULL,
    cache_policy json DEFAULT '{}'::json NOT NULL,
    rate_limit_profile_id text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

CREATE TABLE IF NOT EXISTS public.ai_gateway_llm_call_logs (
    id text PRIMARY KEY,
    request_id text,
    actor_type text,
    actor_id text,
    actor_name text,
    token_id text,
    token_prefix text,
    token_kind text,
    ai_client_id text,
    public_model text,
    upstream_id text,
    upstream_name text,
    provider_kind text,
    upstream_model text,
    endpoint text,
    stream boolean DEFAULT false NOT NULL,
    status text NOT NULL,
    http_status integer DEFAULT 0 NOT NULL,
    upstream_status integer DEFAULT 0 NOT NULL,
    error_code text,
    error_message text,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    total_tokens integer DEFAULT 0 NOT NULL,
    reasoning_tokens integer DEFAULT 0 NOT NULL,
    cached_read_tokens integer DEFAULT 0 NOT NULL,
    cached_write_tokens integer DEFAULT 0 NOT NULL,
    estimated_tokens boolean DEFAULT false NOT NULL,
    ttfb_ms bigint DEFAULT 0 NOT NULL,
    ttft_ms bigint DEFAULT 0 NOT NULL,
    duration_ms bigint DEFAULT 0 NOT NULL,
    input_bytes bigint DEFAULT 0 NOT NULL,
    output_bytes bigint DEFAULT 0 NOT NULL,
    cache_status text,
    route_trace json DEFAULT '{}'::json NOT NULL,
    source_ip text,
    user_agent text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL
);

CREATE TABLE IF NOT EXISTS public.ai_gateway_llm_cache_entries (
    id text PRIMARY KEY,
    cache_key text NOT NULL,
    scope_key text NOT NULL,
    public_model text NOT NULL,
    upstream_id text,
    upstream_model text,
    request_hash text NOT NULL,
    response_body_ciphertext text NOT NULL,
    response_headers json DEFAULT '{}'::json NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    hit_count integer DEFAULT 0 NOT NULL,
    expires_at timestamp without time zone,
    last_hit_at timestamp without time zone,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

CREATE TABLE IF NOT EXISTS public.ai_gateway_llm_health_events (
    id text PRIMARY KEY,
    upstream_id text,
    upstream_name text,
    provider_kind text,
    event_type text NOT NULL,
    status text NOT NULL,
    http_status integer DEFAULT 0 NOT NULL,
    latency_ms bigint DEFAULT 0 NOT NULL,
    error_code text,
    error_message text,
    message text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_upstreams_provider_status
    ON public.ai_gateway_llm_upstreams USING btree (provider_kind, status);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_model_routes_public_enabled
    ON public.ai_gateway_llm_model_routes USING btree (public_model, enabled);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_model_routes_upstream
    ON public.ai_gateway_llm_model_routes USING btree (upstream_id);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_call_logs_created_at
    ON public.ai_gateway_llm_call_logs USING btree (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_call_logs_actor
    ON public.ai_gateway_llm_call_logs USING btree (actor_type, actor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_call_logs_token
    ON public.ai_gateway_llm_call_logs USING btree (token_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_call_logs_public_model
    ON public.ai_gateway_llm_call_logs USING btree (public_model, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_call_logs_upstream
    ON public.ai_gateway_llm_call_logs USING btree (upstream_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_call_logs_status
    ON public.ai_gateway_llm_call_logs USING btree (status, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_gateway_llm_cache_entries_cache_key
    ON public.ai_gateway_llm_cache_entries USING btree (cache_key);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_cache_entries_model
    ON public.ai_gateway_llm_cache_entries USING btree (public_model, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_cache_entries_upstream
    ON public.ai_gateway_llm_cache_entries USING btree (upstream_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_cache_entries_expires
    ON public.ai_gateway_llm_cache_entries USING btree (expires_at);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_health_events_upstream
    ON public.ai_gateway_llm_health_events USING btree (upstream_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_llm_health_events_status
    ON public.ai_gateway_llm_health_events USING btree (status, created_at DESC);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'ai_gateway_llm_model_routes_upstream_id_fkey'
    ) THEN
        ALTER TABLE public.ai_gateway_llm_model_routes
            ADD CONSTRAINT ai_gateway_llm_model_routes_upstream_id_fkey
            FOREIGN KEY (upstream_id)
            REFERENCES public.ai_gateway_llm_upstreams(id)
            ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'ai_gateway_llm_cache_entries_upstream_id_fkey'
    ) THEN
        ALTER TABLE public.ai_gateway_llm_cache_entries
            ADD CONSTRAINT ai_gateway_llm_cache_entries_upstream_id_fkey
            FOREIGN KEY (upstream_id)
            REFERENCES public.ai_gateway_llm_upstreams(id)
            ON DELETE SET NULL;
    END IF;
END $$;
