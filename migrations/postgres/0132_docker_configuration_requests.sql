CREATE TABLE IF NOT EXISTS docker_configuration_requests (
    id text PRIMARY KEY,
    actor_id text NOT NULL,
    request_key text NOT NULL CHECK (request_key ~ '^[A-Za-z0-9._:-]{8,128}$'),
    input_digest text NOT NULL CHECK (input_digest ~ '^[0-9a-f]{64}$'),
    resource_kind text NOT NULL CHECK (resource_kind IN ('docker.host','docker.project')),
    resource_id text NOT NULL,
    action text NOT NULL CHECK (action IN ('register','update','delete')),
    receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (actor_id, request_key),
    CHECK (receipt->>'id' = id AND receipt->>'requestKey' = request_key),
    CHECK (receipt->>'status' = 'committed' AND receipt->>'action' = action),
    CHECK (receipt->'resource'->>'id' = resource_id AND receipt->'resource'->>'resourceKind' = resource_kind)
);
-- Intentionally no resource FK/cascade: original mutation evidence survives
-- deletion and recreation of a host/project with the same public ID.
