BEGIN;

CREATE EXTENSION IF NOT EXISTS pg_trgm;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM unnest(string_to_array(current_setting('shared_preload_libraries'), ',')) AS library(name)
        WHERE btrim(library.name) = 'pg_stat_statements'
    ) AND EXISTS (
        SELECT 1
        FROM pg_roles
        WHERE rolname = current_user AND rolsuper
    ) THEN
        EXECUTE 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements';
    END IF;
END
$$;

COMMIT;
