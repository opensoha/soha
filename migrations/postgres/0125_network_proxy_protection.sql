ALTER TABLE network_proxy_instances
    ADD COLUMN IF NOT EXISTS binding jsonb,
    ADD COLUMN IF NOT EXISTS desired_protection jsonb,
    ADD COLUMN IF NOT EXISTS observed_policy_version bigint NOT NULL DEFAULT 0;

ALTER TABLE network_proxy_instances
    ADD CONSTRAINT network_proxy_binding_object CHECK (binding IS NULL OR jsonb_typeof(binding) = 'object'),
    ADD CONSTRAINT network_proxy_protection_object CHECK (desired_protection IS NULL OR jsonb_typeof(desired_protection) = 'object'),
    ADD CONSTRAINT network_proxy_observed_policy_version_nonnegative CHECK (observed_policy_version >= 0);
