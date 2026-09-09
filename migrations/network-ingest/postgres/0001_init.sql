SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.network_ingest_events (
    producer_id text NOT NULL,
    event_id text NOT NULL,
    event_hash text NOT NULL CHECK (event_hash ~ '^sha256:[a-f0-9]{64}$'),
    batch_id text NOT NULL,
    producer_kind text NOT NULL CHECK (producer_kind IN ('endpoint', 'gateway', 'freeradius', 'network-control')),
    event_type text NOT NULL CHECK (event_type IN ('runtime.heartbeat', 'radius.accounting', 'network.flow.aggregate', 'network.connection.summary', 'proxy.flow.aggregate')),
    sequence bigint NOT NULL CHECK (sequence >= 0),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    PRIMARY KEY (producer_id, event_id)
);

CREATE TABLE IF NOT EXISTS public.network_ingest_producer_state (
    producer_id text PRIMARY KEY,
    producer_kind text NOT NULL CHECK (producer_kind IN ('endpoint', 'gateway', 'freeradius', 'network-control')),
    last_sequence bigint NOT NULL CHECK (last_sequence >= 0),
    gap_count bigint NOT NULL DEFAULT 0 CHECK (gap_count >= 0),
    regression_count bigint NOT NULL DEFAULT 0 CHECK (regression_count >= 0),
    last_seen_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_network_ingest_events_received
    ON public.network_ingest_events (received_at);
CREATE INDEX IF NOT EXISTS idx_network_ingest_events_producer_sequence
    ON public.network_ingest_events (producer_id, sequence);
CREATE INDEX IF NOT EXISTS idx_network_ingest_events_type_occurred
    ON public.network_ingest_events (event_type, occurred_at);
