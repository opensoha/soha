ALTER TABLE public.network_ingest_events
    DROP CONSTRAINT network_ingest_events_producer_kind_check,
    DROP CONSTRAINT network_ingest_events_event_type_check;
ALTER TABLE public.network_ingest_events
    ADD CONSTRAINT network_ingest_events_producer_kind_check CHECK (producer_kind IN (
        'endpoint', 'gateway', 'freeradius', 'network-control', 'proxy'
    )),
    ADD CONSTRAINT network_ingest_events_event_type_check CHECK (event_type IN (
        'runtime.heartbeat', 'radius.accounting', 'network.flow.aggregate',
        'network.connection.summary', 'proxy.flow.aggregate', 'vpn.probe.batch',
        'vpn.tunnel.stats', 'vpn.gateway.health', 'proxy.runtime.sample'
    )),
    ADD CONSTRAINT network_ingest_events_proxy_runtime_producer_check CHECK (
        event_type <> 'proxy.runtime.sample' OR producer_kind = 'proxy'
    );

ALTER TABLE public.network_ingest_producer_state
    DROP CONSTRAINT network_ingest_producer_state_producer_kind_check;
ALTER TABLE public.network_ingest_producer_state
    ADD CONSTRAINT network_ingest_producer_state_producer_kind_check CHECK (producer_kind IN (
        'endpoint', 'gateway', 'freeradius', 'network-control', 'proxy'
    ));

CREATE INDEX idx_network_ingest_proxy_runtime_samples ON public.network_ingest_events
    (producer_id, occurred_at DESC, sequence DESC)
    WHERE event_type = 'proxy.runtime.sample';
