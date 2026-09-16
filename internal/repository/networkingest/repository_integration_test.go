package networkingest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkingestrepo "github.com/opensoha/soha/internal/repository/networkingest"
	"go.uber.org/zap"
)

func TestRepositoryWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_NETWORK_INGEST_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_NETWORK_INGEST_TEST_POSTGRES_PORT to run the PostgreSQL integration test")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid PostgreSQL test port: %v", err)
	}
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha_ingest", User: "pgsql", Password: "test-only", SSLMode: "disable",
		MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "network-ingest", "postgres")); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}

	repository := networkingestrepo.New(store.DB())
	producerID := "gateway-" + uuid.NewString()
	now := time.Now().UTC()
	checkGatewayIngest(t, ctx, store, repository, producerID, now)
	checkRADIUSIngest(t, ctx, store, repository, now)
	checkProxyIngest(t, ctx, repository, now)
	checkVPNIngest(t, ctx, store, repository, now)
}

func checkGatewayIngest(t *testing.T, ctx context.Context, store *dbstore.Store, repository *networkingestrepo.Repository, producerID string, now time.Time) {
	t.Helper()
	events := []domainnetworkingest.Event{
		{ProducerID: producerID, EventID: "event-1", EventHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BatchID: "batch-1", ProducerKind: "gateway", EventType: "runtime.heartbeat", Sequence: 10, OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"status":"healthy"}`)},
		{ProducerID: producerID, EventID: "event-2", EventHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", BatchID: "batch-1", ProducerKind: "gateway", EventType: "runtime.heartbeat", Sequence: 12, OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"status":"healthy"}`)},
		{ProducerID: producerID, EventID: "event-3", EventHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", BatchID: "batch-1", ProducerKind: "gateway", EventType: "runtime.heartbeat", Sequence: 11, OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"status":"healthy"}`)},
	}
	result, err := repository.Append(ctx, events)
	if err != nil || result.Accepted != 3 || result.Duplicate != 0 {
		t.Fatalf("first append = %#v, %v", result, err)
	}
	result, err = repository.Append(ctx, events)
	if err != nil || result.Accepted != 0 || result.Duplicate != 3 {
		t.Fatalf("duplicate append = %#v, %v", result, err)
	}
	conflict := append([]domainnetworkingest.Event(nil), events...)
	conflict[0].EventHash = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if _, err := repository.Append(ctx, conflict); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflicting append error = %v, want conflict", err)
	}

	var lastSequence, gaps, regressions int64
	if err := store.SQLDB().QueryRowContext(ctx, `SELECT last_sequence, gap_count, regression_count FROM public.network_ingest_producer_state WHERE producer_id = $1`, producerID).Scan(&lastSequence, &gaps, &regressions); err != nil {
		t.Fatalf("read producer state: %v", err)
	}
	if lastSequence != 12 || gaps != 1 || regressions != 1 {
		t.Fatalf("producer state = last:%d gaps:%d regressions:%d", lastSequence, gaps, regressions)
	}
}

func checkRADIUSIngest(t *testing.T, ctx context.Context, store *dbstore.Store, repository *networkingestrepo.Repository, now time.Time) {
	t.Helper()
	radiusProducerID := "freeradius-" + uuid.NewString()
	radiusEvents := []domainnetworkingest.Event{
		{ProducerID: radiusProducerID, EventID: "start", EventHash: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", BatchID: "start", ProducerKind: "freeradius", EventType: "radius.accounting", Sequence: 100, OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"statusType":"start"}`)},
		{ProducerID: radiusProducerID, EventID: "interim", EventHash: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", BatchID: "interim", ProducerKind: "freeradius", EventType: "radius.accounting", Sequence: 160, OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"statusType":"interim-update"}`)},
	}
	if result, err := repository.Append(ctx, radiusEvents); err != nil || result.Accepted != 2 {
		t.Fatalf("append RADIUS events = %#v, %v", result, err)
	}
	var lastSequence, gaps, regressions int64
	if err := store.SQLDB().QueryRowContext(ctx, `SELECT last_sequence, gap_count, regression_count FROM public.network_ingest_producer_state WHERE producer_id = $1`, radiusProducerID).Scan(&lastSequence, &gaps, &regressions); err != nil {
		t.Fatalf("read RADIUS producer state: %v", err)
	}
	if lastSequence != 160 || gaps != 0 || regressions != 0 {
		t.Fatalf("RADIUS producer state = last:%d gaps:%d regressions:%d", lastSequence, gaps, regressions)
	}
}

func checkProxyIngest(t *testing.T, ctx context.Context, repository *networkingestrepo.Repository, now time.Time) {
	t.Helper()
	endpointProducerID := "endpoint-" + uuid.NewString()
	proxyEvents := []domainnetworkingest.Event{
		{ProducerID: endpointProducerID, EventID: "proxy-1", EventHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111", BatchID: "proxy", ProducerKind: "endpoint", EventType: "proxy.flow.aggregate", Sequence: 1, OccurredAt: now.Add(-time.Minute), ReceivedAt: now.Add(-time.Minute), Payload: []byte(`{"windowStartedAt":"2026-09-03T00:00:00Z","windowEndedAt":"2026-09-03T00:01:00Z","engine":"mihomo","profileId":"profile-1","profileRevision":1,"mode":"managed_follow","selectedProxy":"edge-a","uploadBytes":10,"downloadBytes":20,"activeConnections":1}`)},
		{ProducerID: endpointProducerID, EventID: "proxy-2", EventHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222", BatchID: "proxy", ProducerKind: "endpoint", EventType: "proxy.flow.aggregate", Sequence: 2, OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"windowStartedAt":"2026-09-03T00:01:00Z","windowEndedAt":"2026-09-03T00:02:00Z","engine":"mihomo","profileId":"profile-1","profileRevision":1,"mode":"managed_follow","selectedProxy":"edge-a","uploadBytes":30,"downloadBytes":40,"activeConnections":3}`)},
	}
	if result, err := repository.Append(ctx, proxyEvents); err != nil || result.Accepted != 2 {
		t.Fatalf("append proxy events = %#v, %v", result, err)
	}
	summary, err := repository.Summary(ctx, domainnetworkingest.SummaryFilter{From: now.Add(-2 * time.Minute), To: now.Add(time.Minute), ProducerID: endpointProducerID, Limit: 10})
	if err != nil || summary.EventCount != 2 || summary.ProxyFlowCount != 2 || summary.UploadBytes != 40 || summary.DownloadBytes != 60 || summary.ActiveConnections != 3 || len(summary.Producers) != 1 || len(summary.ProxyFlows) != 1 {
		t.Fatalf("Summary() = %#v, %v", summary, err)
	}
	flow := summary.ProxyFlows[0]
	if flow.ProducerID != endpointProducerID || flow.ProfileID != "profile-1" || flow.SelectedProxy != "edge-a" || flow.UploadBytes != 40 || flow.DownloadBytes != 60 || flow.ActiveConnections != 3 {
		t.Fatalf("proxy flow summary = %#v", flow)
	}
	removed, err := repository.DeleteBefore(ctx, now.Add(time.Second))
	if err != nil || removed != 7 {
		t.Fatalf("DeleteBefore() = %d, %v", removed, err)
	}
}
