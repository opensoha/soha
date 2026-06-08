BEGIN;

CREATE TABLE IF NOT EXISTS public.installed_plugins (
    id text NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    publisher text NOT NULL,
    type text NOT NULL,
    status text NOT NULL,
    source text NOT NULL,
    manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    checksum_status text NOT NULL,
    signature_status text,
    requested_permissions jsonb DEFAULT '{}'::jsonb NOT NULL,
    configured_secret_refs jsonb DEFAULT '{}'::jsonb NOT NULL,
    installed_by text NOT NULL,
    installed_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    enabled_at timestamp without time zone,
    disabled_at timestamp without time zone,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);

ALTER TABLE ONLY public.installed_plugins
    ADD CONSTRAINT installed_plugins_pkey PRIMARY KEY (id);

CREATE INDEX IF NOT EXISTS idx_installed_plugins_status
    ON public.installed_plugins USING btree (status);

CREATE INDEX IF NOT EXISTS idx_installed_plugins_type
    ON public.installed_plugins USING btree (type);

COMMIT;
