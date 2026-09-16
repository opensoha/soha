package networkingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func (r *Repository) VPNProbe(ctx context.Context, producerID, intentID, batchID string, now time.Time) (*networkprotocol.VPNProbeBatch, error) {
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`SELECT payload FROM network_ingest_events WHERE producer_id = ? AND producer_kind = 'endpoint' AND event_type = 'vpn.probe.batch' AND payload->>'batchId' = ? AND (? = '' OR payload->>'intentId' = ?) AND occurred_at >= ? AND occurred_at <= ? ORDER BY received_at DESC, sequence DESC LIMIT 1`, producerID, batchID, intentID, intentID, now.Add(-5*time.Minute), now).Row().Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var batch networkprotocol.VPNProbeBatch
	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, err
	}
	return &batch, nil
}

const vpnStatsScope = `WITH authorized AS (
 SELECT * FROM jsonb_to_recordset(?::jsonb) AS b("sessionId" text, "profileId" text, "gatewayId" text, "gatewayRuntimeId" text, "endpointRuntimeId" text)
), scoped AS (
 SELECT DISTINCT ON (e.producer_id, b."sessionId", e.payload->>'epoch', e.payload->>'windowStartedAt', e.payload->>'windowEndedAt')
 b."sessionId" AS session_id, b."gatewayId" AS gateway_id, e.occurred_at, e.sequence, e.payload
 FROM network_ingest_events e JOIN authorized b ON e.producer_id = b."gatewayRuntimeId"
 AND e.payload->>'sessionId' = b."sessionId" AND e.payload->>'profileId' = b."profileId"
 AND e.payload->>'gatewayId' = b."gatewayId" AND e.payload->>'endpointRuntimeId' = b."endpointRuntimeId"
 WHERE e.producer_kind = 'gateway' AND e.event_type = 'vpn.tunnel.stats' AND e.occurred_at >= ? AND e.occurred_at < ?
 ORDER BY e.producer_id, b."sessionId", e.payload->>'epoch', e.payload->>'windowStartedAt', e.payload->>'windowEndedAt', e.received_at
)`

func (r *Repository) VPNMetrics(ctx context.Context, q domain.VPNMetricsQuery) (domain.VPNMetrics, error) {
	result := domain.VPNMetrics{From: q.From, To: q.To, AsOf: time.Now().UTC(), Sessions: []domain.VPNSessionMetrics{}, Gateways: []domain.VPNGatewayMetrics{}, Series: []domain.VPNMetricPoint{}}
	gateways := map[string]*domain.VPNGatewayMetrics{}
	points := map[time.Time]*domain.VPNMetricPoint{}
	if len(q.Sessions) > 0 {
		if err := r.vpnTrafficMetrics(ctx, q, &result, gateways, points); err != nil {
			return result, err
		}
	}
	if len(q.Probes) > 0 {
		if err := r.vpnLatencyMetrics(ctx, q, &result, gateways, points); err != nil {
			return result, err
		}
	}
	for _, item := range gateways {
		result.Gateways = append(result.Gateways, *item)
	}
	for _, item := range points {
		result.Series = append(result.Series, *item)
	}
	slices.SortFunc(result.Gateways, func(a, b domain.VPNGatewayMetrics) int {
		if a.GatewayID < b.GatewayID {
			return -1
		}
		if a.GatewayID > b.GatewayID {
			return 1
		}
		return 0
	})
	slices.SortFunc(result.Series, func(a, b domain.VPNMetricPoint) int { return a.At.Compare(b.At) })
	return result, nil
}

func (r *Repository) vpnTrafficMetrics(ctx context.Context, q domain.VPNMetricsQuery, result *domain.VPNMetrics, gateways map[string]*domain.VPNGatewayMetrics, points map[time.Time]*domain.VPNMetricPoint) error {
	bindings, err := json.Marshal(q.Sessions)
	if err != nil {
		return err
	}
	query := vpnStatsScope + `, totals AS (
 SELECT session_id, gateway_id, sum((payload->>'uploadBytes')::bigint)::bigint AS upload_bytes, sum((payload->>'downloadBytes')::bigint)::bigint AS download_bytes
 FROM scoped GROUP BY session_id, gateway_id
), latest AS (
 SELECT DISTINCT ON (session_id) session_id, payload, occurred_at FROM scoped ORDER BY session_id, occurred_at DESC, sequence DESC
)
 SELECT t.session_id, t.gateway_id, t.upload_bytes, t.download_bytes,
 CASE WHEN l.occurred_at >= ? THEN (l.payload->>'uploadBytes')::double precision / NULLIF(EXTRACT(EPOCH FROM (l.payload->>'windowEndedAt')::timestamptz-(l.payload->>'windowStartedAt')::timestamptz),0) END AS upload_bytes_per_second,
 CASE WHEN l.occurred_at >= ? THEN (l.payload->>'downloadBytes')::double precision / NULLIF(EXTRACT(EPOCH FROM (l.payload->>'windowEndedAt')::timestamptz-(l.payload->>'windowStartedAt')::timestamptz),0) END AS download_bytes_per_second,
 NULLIF(l.payload->>'lastHandshakeAt','')::timestamptz AS last_handshake_at, l.occurred_at AS measured_at
 FROM totals t JOIN latest l USING(session_id) ORDER BY t.session_id`
	freshAfter := q.To.Add(-2 * time.Minute)
	if err := r.db.WithContext(ctx).Raw(query, string(bindings), q.From, q.To, freshAfter, freshAfter).Scan(&result.Sessions).Error; err != nil {
		return err
	}
	for _, session := range result.Sessions {
		g := vpnMetricGateway(gateways, session.GatewayID)
		g.TrafficAvailable = true
		g.UploadBytes += session.UploadBytes
		g.DownloadBytes += session.DownloadBytes
		if session.MeasuredAt.After(g.MeasuredAt) {
			g.MeasuredAt = session.MeasuredAt
		}
	}
	var series []domain.VPNMetricPoint
	if err := r.db.WithContext(ctx).Raw(vpnStatsScope+` SELECT date_trunc('hour', occurred_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS at, sum((payload->>'uploadBytes')::bigint)::bigint AS upload_bytes, sum((payload->>'downloadBytes')::bigint)::bigint AS download_bytes FROM scoped GROUP BY 1 ORDER BY 1`, string(bindings), q.From, q.To).Scan(&series).Error; err != nil {
		return err
	}
	for _, point := range series {
		item := vpnMetricPoint(points, point.At)
		item.TrafficAvailable = true
		item.UploadBytes = point.UploadBytes
		item.DownloadBytes = point.DownloadBytes
	}
	return nil
}

func (r *Repository) vpnLatencyMetrics(ctx context.Context, q domain.VPNMetricsQuery, result *domain.VPNMetrics, gateways map[string]*domain.VPNGatewayMetrics, points map[time.Time]*domain.VPNMetricPoint) error {
	bindings, err := json.Marshal(q.Probes)
	if err != nil {
		return err
	}
	query := `WITH authorized AS (
 SELECT * FROM jsonb_to_recordset(?::jsonb) AS b("intentId" text, "profileId" text, "endpointRuntimeId" text, "gatewayIds" jsonb)
), probes AS (
 SELECT DISTINCT ON (e.producer_id, e.payload->>'intentId', e.payload->>'batchId') e.payload, e.occurred_at, b."gatewayIds" AS gateway_ids
 FROM network_ingest_events e JOIN authorized b ON e.producer_id = b."endpointRuntimeId" AND e.payload->>'intentId' = b."intentId" AND e.payload->>'profileId' = b."profileId"
 WHERE e.producer_kind = 'endpoint' AND e.event_type = 'vpn.probe.batch' AND e.occurred_at >= ? AND e.occurred_at < ?
 ORDER BY e.producer_id, e.payload->>'intentId', e.payload->>'batchId', e.received_at
), samples AS (
 SELECT sample->>'gatewayId' AS gateway_id, rtt.value::double precision AS ms, occurred_at
 FROM probes CROSS JOIN LATERAL jsonb_array_elements(payload->'results') sample
 CROSS JOIN LATERAL jsonb_array_elements_text(sample->'rttSamplesMs') AS rtt(value)
 WHERE jsonb_exists(gateway_ids, sample->>'gatewayId')
)
 SELECT 'total' AS kind, '' AS gateway_id, NULL::timestamptz AS at, count(*)::integer AS sample_count, percentile_cont(0.5) WITHIN GROUP(ORDER BY ms) AS p50, percentile_cont(0.95) WITHIN GROUP(ORDER BY ms) AS p95, max(occurred_at) AS measured_at FROM samples
 UNION ALL
 SELECT 'gateway', gateway_id, NULL::timestamptz, count(*)::integer, percentile_cont(0.5) WITHIN GROUP(ORDER BY ms), percentile_cont(0.95) WITHIN GROUP(ORDER BY ms), max(occurred_at) FROM samples GROUP BY gateway_id
 UNION ALL
 SELECT 'hour', '', date_trunc('hour', occurred_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC', count(*)::integer, percentile_cont(0.5) WITHIN GROUP(ORDER BY ms), percentile_cont(0.95) WITHIN GROUP(ORDER BY ms), max(occurred_at) FROM samples GROUP BY 3`
	var rows []struct {
		Kind        string
		GatewayID   string
		At          *time.Time
		SampleCount int
		P50         *float64
		P95         *float64
		MeasuredAt  *time.Time
	}
	if err := r.db.WithContext(ctx).Raw(query, string(bindings), q.From, q.To).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		switch row.Kind {
		case "total":
			result.SampleCount, result.LatencyP50Ms, result.LatencyP95Ms = row.SampleCount, row.P50, row.P95
		case "gateway":
			g := vpnMetricGateway(gateways, row.GatewayID)
			g.SampleCount, g.LatencyP50Ms, g.LatencyP95Ms = row.SampleCount, row.P50, row.P95
			if row.MeasuredAt != nil && row.MeasuredAt.After(g.MeasuredAt) {
				g.MeasuredAt = *row.MeasuredAt
			}
		case "hour":
			if row.At != nil {
				point := vpnMetricPoint(points, *row.At)
				point.LatencyP50Ms, point.LatencyP95Ms = row.P50, row.P95
			}
		}
	}
	return nil
}

func vpnMetricGateway(items map[string]*domain.VPNGatewayMetrics, id string) *domain.VPNGatewayMetrics {
	if item := items[id]; item != nil {
		return item
	}
	item := &domain.VPNGatewayMetrics{GatewayID: id}
	items[id] = item
	return item
}
func vpnMetricPoint(items map[time.Time]*domain.VPNMetricPoint, at time.Time) *domain.VPNMetricPoint {
	at = at.UTC()
	if item := items[at]; item != nil {
		return item
	}
	item := &domain.VPNMetricPoint{At: at}
	items[at] = item
	return item
}
