ALTER TABLE public.network_ingest_events
    DROP CONSTRAINT IF EXISTS network_ingest_events_event_type_check;

ALTER TABLE public.network_ingest_events
    ADD CONSTRAINT network_ingest_events_event_type_check
    CHECK (event_type IN (
        'runtime.heartbeat',
        'radius.accounting',
        'network.flow.aggregate',
        'network.connection.summary',
        'proxy.flow.aggregate'
    ));
