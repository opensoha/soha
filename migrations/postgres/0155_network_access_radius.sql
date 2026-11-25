SELECT pg_catalog.set_config('search_path', '', false);
ALTER TABLE public.network_nas_authorizations
    ADD COLUMN access_network_id text NOT NULL DEFAULT '',
    ADD COLUMN admission_binding_id text NOT NULL DEFAULT '',
    ADD COLUMN called_station_id text NOT NULL DEFAULT '',
    ADD COLUMN nas_port_id text NOT NULL DEFAULT '',
    ADD COLUMN nas_port bigint CHECK (nas_port BETWEEN 0 AND 4294967295),
    ADD COLUMN outer_identity text NOT NULL DEFAULT '';

ALTER TABLE public.network_nas_session_evidence
    ADD COLUMN input_octets bigint NOT NULL DEFAULT 0 CHECK (input_octets >= 0),
    ADD COLUMN output_octets bigint NOT NULL DEFAULT 0 CHECK (output_octets >= 0);
