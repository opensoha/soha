CREATE TABLE identity_outpost_runtime_deployments (
    outpost_id text PRIMARY KEY REFERENCES identity_outposts(id) ON DELETE CASCADE,
    slot_id text NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    state jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (slot_id)
);

CREATE TABLE identity_outpost_runtime_requests (
    outpost_id text NOT NULL REFERENCES identity_outposts(id) ON DELETE CASCADE,
    request_id text NOT NULL,
    fingerprint text NOT NULL,
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (outpost_id, request_id)
);

CREATE TABLE identity_outpost_runtime_instances (
    outpost_id text NOT NULL REFERENCES identity_outposts(id) ON DELETE CASCADE,
    instance_id text NOT NULL,
    process_epoch text NOT NULL,
    generation bigint NOT NULL,
	credential_hash text NOT NULL DEFAULT '',
	configuration_version bigint NOT NULL DEFAULT 0,
	control_credential_hash text NOT NULL,
	control_configuration_version bigint NOT NULL,
	control_generation bigint NOT NULL,
	control_issued_at timestamptz NOT NULL,
	control_expires_at timestamptz NOT NULL,
    accepting_requests boolean NOT NULL,
    active_checks bigint NOT NULL CHECK (active_checks >= 0),
    observed_at timestamptz,
    lease_expires_at timestamptz,
    PRIMARY KEY(outpost_id, instance_id)
);

CREATE TABLE identity_outpost_runtime_retired_epochs (
    outpost_id text NOT NULL REFERENCES identity_outposts(id) ON DELETE CASCADE,
    instance_id text NOT NULL,
    process_epoch text NOT NULL,
    retired_at timestamptz NOT NULL,
    PRIMARY KEY(outpost_id,instance_id,process_epoch)
);
