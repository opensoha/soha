-- Keep opaque directory identifiers in storage while exposing provider metadata
-- that management surfaces can translate into human-readable source labels.
UPDATE teams AS team
SET metadata = (
    COALESCE(team.metadata, '{}'::json)::jsonb
    || jsonb_build_object(
        'directoryConnectionId', connection.id,
        'directoryConnectionName', connection.name,
        'directoryProviderType', connection.provider_type
    )
)::json
FROM directory_connections AS connection
WHERE team.source = 'directory:' || connection.id;
