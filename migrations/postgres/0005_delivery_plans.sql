CREATE TABLE IF NOT EXISTS public.delivery_plans (
    id text NOT NULL PRIMARY KEY,
    source text DEFAULT 'manual'::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    application_id text NOT NULL,
    application_name text,
    application_environment_id text NOT NULL,
    environment_key text,
    action text NOT NULL,
    target_id text,
    target_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    target_summary text,
    build_source_id text,
    release_bundle_id text,
    ref_type text,
    ref_name text,
    image_tag text,
    release_name text,
    container_name text,
    reason text,
    risk_level text,
    requires_approval boolean DEFAULT false NOT NULL,
    impact jsonb DEFAULT '{}'::jsonb NOT NULL,
    rollback_strategy text,
    variables jsonb DEFAULT '{}'::jsonb NOT NULL,
    build_args jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by text,
    confirmed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_delivery_plans_application_id
    ON public.delivery_plans USING btree (application_id);

CREATE INDEX IF NOT EXISTS idx_delivery_plans_application_environment_id
    ON public.delivery_plans USING btree (application_environment_id);

CREATE INDEX IF NOT EXISTS idx_delivery_plans_source_status_updated_at
    ON public.delivery_plans USING btree (source, status, updated_at DESC);
