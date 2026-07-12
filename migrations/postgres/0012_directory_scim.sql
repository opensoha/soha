SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.directory_scim_tokens (
    connection_id text PRIMARY KEY REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE, created_at timestamptz NOT NULL DEFAULT now(), rotated_at timestamptz
);

CREATE TABLE IF NOT EXISTS public.directory_scim_organizations (
    connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    external_id text NOT NULL, name text NOT NULL, external_parent_id text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(connection_id,external_id)
);

CREATE TABLE IF NOT EXISTS public.directory_scim_people (
    connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    external_id text NOT NULL, username text NOT NULL, display_name text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '',
    phone text NOT NULL DEFAULT '', active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(connection_id,external_id)
);

CREATE TABLE IF NOT EXISTS public.directory_scim_memberships (
    connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    external_person_id text NOT NULL, external_organization_id text NOT NULL,
    PRIMARY KEY(connection_id,external_person_id,external_organization_id)
);
