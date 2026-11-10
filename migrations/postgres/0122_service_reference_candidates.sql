-- Inactive configuration proposals; these rows do not participate in runtime resolution.
CREATE TABLE environment_service_reference_candidates (
    id text PRIMARY KEY,
    application_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    consumer_binding_id text NOT NULL REFERENCES application_environments(id) ON DELETE CASCADE,
    service_id text NOT NULL REFERENCES application_services(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision > 0),
    preview jsonb NOT NULL CHECK (jsonb_typeof(preview) = 'object'),
    source_identity_revision text NOT NULL CHECK (source_identity_revision ~ '^[a-f0-9]{64}$'),
    context_revision text NOT NULL CHECK (context_revision ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (consumer_binding_id, service_id),
    CHECK ((preview->>'applicationId') IS NOT DISTINCT FROM application_id),
    CHECK ((preview->>'serviceId') IS NOT DISTINCT FROM service_id),
    CHECK ((preview->'consumer'->>'applicationEnvironmentId') IS NOT DISTINCT FROM consumer_binding_id)
);

CREATE INDEX environment_service_reference_candidates_application
    ON environment_service_reference_candidates (application_id);
CREATE INDEX environment_service_reference_candidates_service
    ON environment_service_reference_candidates (service_id);
