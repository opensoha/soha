ALTER TABLE public.network_ingest_events
    DROP CONSTRAINT network_ingest_events_event_type_check;
ALTER TABLE public.network_ingest_events
    ADD CONSTRAINT network_ingest_events_event_type_check CHECK (event_type IN (
        'runtime.heartbeat', 'radius.accounting', 'network.flow.aggregate',
        'network.connection.summary', 'proxy.flow.aggregate',
        'vpn.probe.batch', 'vpn.tunnel.stats', 'vpn.gateway.health'
    )),
    ADD CONSTRAINT network_ingest_events_vpn_producer_check CHECK (
        (event_type <> 'vpn.probe.batch' OR producer_kind = 'endpoint') AND
        (event_type NOT IN ('vpn.tunnel.stats', 'vpn.gateway.health') OR producer_kind = 'gateway')
    );

CREATE INDEX idx_network_ingest_vpn_probe_batch ON public.network_ingest_events
    (producer_id, (payload->>'batchId'), received_at DESC, sequence DESC)
    WHERE producer_kind = 'endpoint' AND event_type = 'vpn.probe.batch';
CREATE INDEX idx_network_ingest_vpn_probe_scope ON public.network_ingest_events
    (producer_id, (payload->>'intentId'), (payload->>'profileId'), occurred_at)
    WHERE producer_kind = 'endpoint' AND event_type = 'vpn.probe.batch';
CREATE INDEX idx_network_ingest_vpn_stats_scope ON public.network_ingest_events
    (producer_id, occurred_at)
    WHERE producer_kind = 'gateway' AND event_type = 'vpn.tunnel.stats';
