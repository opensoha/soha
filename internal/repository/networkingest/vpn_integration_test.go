package networkingest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkingest"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/networkprotocol"
	repo "github.com/opensoha/soha/internal/repository/networkingest"
)

func checkVPNIngest(t *testing.T, ctx context.Context, store *dbstore.Store, repository *repo.Repository, now time.Time) {
	t.Helper()
	gateway, endpoint := "gateway-"+uuid.NewString(), "endpoint-"+uuid.NewString()
	defer func() {
		_ = store.DB().Exec(`DELETE FROM network_ingest_events WHERE producer_id IN ?`, []string{gateway, endpoint}).Error
	}()
	appendEvent := func(producer, kind, eventType string, payload any, sequence int64) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		event := domain.Event{ProducerID: producer, ProducerKind: kind, EventID: uuid.NewString(), BatchID: uuid.NewString(), EventHash: "sha256:" + strings.Repeat("a", 64), EventType: eventType, Sequence: sequence, OccurredAt: now, ReceivedAt: now, Payload: raw}
		if result, err := repository.Append(ctx, []domain.Event{event}); err != nil || result.Accepted != 1 {
			t.Fatalf("VPN append: %+v %v", result, err)
		}
	}
	stats := networkprotocol.VPNTunnelStats{SessionID: "session-1", ProfileID: "profile-1", GatewayID: "gw-1", EndpointRuntimeID: endpoint, Epoch: "epoch-1", WindowStartedAt: now.Add(-time.Minute), WindowEndedAt: now, UploadBytes: 60, DownloadBytes: 120}
	appendEvent(gateway, "gateway", networkprotocol.EventVPNTunnelStats, stats, 1)
	appendEvent(gateway, "gateway", networkprotocol.EventVPNTunnelStats, stats, 2) // Same counter window with another event ID must not double count.
	stats.SessionID, stats.UploadBytes = "hidden-session", 999999
	appendEvent(gateway, "gateway", networkprotocol.EventVPNTunnelStats, stats, 3)
	probe := networkprotocol.VPNProbeBatch{IntentID: "intent-1", ProfileID: "profile-1", ProfileRevision: 1, SelectionPolicyRevision: 1, BatchID: "probe-1", NetworkEpoch: "network-1", WindowStartedAt: now.Add(-time.Second), WindowEndedAt: now, Results: []networkprotocol.VPNProbeResult{{GatewayID: "gw-1", SentCount: 3, RTTSamplesMs: []float64{20, 35, 70}}, {GatewayID: "hidden-gw", SentCount: 1, RTTSamplesMs: []float64{9999}}}}
	appendEvent(endpoint, "endpoint", networkprotocol.EventVPNProbeBatch, probe, 1)
	appendEvent(gateway, "gateway", networkprotocol.EventVPNGatewayHealth, networkprotocol.VPNGatewayHealth{GatewayID: "gw-1", Ready: true, ActiveEndpointPeers: 1, ConfigurationVersion: 1, ObservedAt: now}, 4)
	got, err := repository.VPNProbe(ctx, endpoint, "intent-1", "probe-1", now)
	if err != nil || got == nil || got.BatchID != probe.BatchID {
		t.Fatalf("probe lookup: %+v %v", got, err)
	}
	if got, err := repository.VPNProbe(ctx, endpoint, "other-intent", "probe-1", now); err != nil || got != nil {
		t.Fatalf("foreign intent returned: %+v %v", got, err)
	}
	checkVPNMetricsScope(t, ctx, repository, now, gateway, endpoint)
}

func checkVPNMetricsScope(t *testing.T, ctx context.Context, repository *repo.Repository, now time.Time, gateway, endpoint string) {
	t.Helper()
	query := domain.VPNMetricsQuery{From: now.Add(-time.Hour), To: now.Add(time.Second), Sessions: []domain.VPNSessionBinding{{SessionID: "session-1", ProfileID: "profile-1", GatewayID: "gw-1", GatewayRuntimeID: gateway, EndpointRuntimeID: endpoint}}, Probes: []domain.VPNProbeBinding{{IntentID: "intent-1", ProfileID: "profile-1", EndpointRuntimeID: endpoint, GatewayIDs: []string{"gw-1"}}}}
	metrics, err := repository.VPNMetrics(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Sessions) != 1 || metrics.Sessions[0].UploadBytes != 60 || metrics.Sessions[0].DownloadBytes != 120 || metrics.Sessions[0].UploadBytesPerSecond == nil || *metrics.Sessions[0].UploadBytesPerSecond != 1 {
		t.Fatalf("traffic scope/dedup/rate: %+v", metrics.Sessions)
	}
	if metrics.SampleCount != 3 || metrics.LatencyP50Ms == nil || *metrics.LatencyP50Ms != 35 || metrics.LatencyP95Ms == nil || *metrics.LatencyP95Ms < 66 || *metrics.LatencyP95Ms > 67 || len(metrics.Gateways) != 1 {
		t.Fatalf("latency scope/percentiles: %+v", metrics)
	}
	query.Sessions, query.Probes = nil, nil
	empty, err := repository.VPNMetrics(ctx, query)
	if err != nil || len(empty.Sessions) != 0 || len(empty.Gateways) != 0 || empty.LatencyP50Ms != nil {
		t.Fatalf("empty scope returned metrics: %+v %v", empty, err)
	}
}
