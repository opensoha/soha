ALTER TABLE public.network_nas_authorizations
    ADD COLUMN IF NOT EXISTS decision_attribute_version text NOT NULL DEFAULT '';
