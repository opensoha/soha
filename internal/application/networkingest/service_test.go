package networkingest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type captureStore struct {
	events        []domainnetworkingest.Event
	summary       domainnetworkingest.Summary
	summaryFilter domainnetworkingest.SummaryFilter
}

func (store *captureStore) Append(_ context.Context, events []domainnetworkingest.Event) (domainnetworkingest.Result, error) {
	store.events = append(store.events, events...)
	return domainnetworkingest.Result{Accepted: len(events)}, nil
}

func (store *captureStore) Summary(_ context.Context, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	store.summaryFilter = filter
	return store.summary, nil
}

func TestServiceSummaryAllowsOnlyDedicatedCoreReaderAndBoundsWindow(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	store := &captureStore{summary: domainnetworkingest.Summary{EventCount: 2}}
	service, err := New(store, schemas, Options{MaxEventsPerBatch: 1000, MaxClockSkew: 5 * time.Minute, Retention: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	identity := networkidentity.Identity{Scope: networkidentity.ScopeIngest, Kind: "core", ID: "soha-server"}
	summary, err := service.Summary(context.Background(), identity, domainnetworkingest.SummaryFilter{})
	if err != nil || summary.EventCount != 2 || !store.summaryFilter.From.Equal(now.Add(-time.Hour)) || !store.summaryFilter.To.Equal(now) || store.summaryFilter.Limit != 100 {
		t.Fatalf("Summary() = %#v, %v filter=%#v", summary, err, store.summaryFilter)
	}
	if _, err := service.Summary(context.Background(), networkidentity.Identity{Scope: networkidentity.ScopeIngest, Kind: "endpoint", ID: "endpoint-1"}, domainnetworkingest.SummaryFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("endpoint Summary() error = %v, want unauthorized", err)
	}
	if _, err := service.Summary(context.Background(), identity, domainnetworkingest.SummaryFilter{From: now.Add(-8 * 24 * time.Hour), To: now}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unbounded Summary() error = %v, want invalid argument", err)
	}
}

func TestServiceIngestAuthenticatesProducerAndPersistsValidatedEvents(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	store := &captureStore{}
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	service, err := New(store, schemas, Options{MaxEventsPerBatch: 1000, MaxClockSkew: 5 * time.Minute, Retention: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }
	raw := []byte(`{"schemaVersion":"network-ingest/v1alpha1","batchId":"batch-1","producerId":"gateway-1","producerKind":"gateway","sentAt":"2026-09-02T08:00:00Z","events":[{"id":"event-1","type":"runtime.heartbeat","sequence":1,"occurredAt":"2026-09-02T08:00:00Z","payload":{"status":"healthy","configurationVersion":1,"policyVersion":1,"uptimeSeconds":10}}]}`)

	ack, err := service.Ingest(context.Background(), networkidentity.Identity{Scope: networkidentity.ScopeIngest, Kind: "gateway", ID: "gateway-1"}, raw)
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if ack.BatchID != "batch-1" || ack.Accepted != 1 || len(store.events) != 1 {
		t.Fatalf("ack = %#v, events = %#v", ack, store.events)
	}
	if store.events[0].EventHash == "" || store.events[0].Payload == nil {
		t.Fatalf("stored event = %#v", store.events[0])
	}

	_, err = service.Ingest(context.Background(), networkidentity.Identity{Scope: networkidentity.ScopeIngest, Kind: "endpoint", ID: "gateway-1"}, raw)
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("identity mismatch error = %v, want unauthorized", err)
	}
}

func TestServiceConvertsStrictRADIUSAccountingToCanonicalEvent(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	store := &captureStore{}
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	service, err := New(store, schemas, Options{MaxEventsPerBatch: 1000, MaxClockSkew: 5 * time.Minute, Retention: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }
	identity := networkidentity.Identity{Scope: networkidentity.ScopeIngest, Kind: "freeradius", ID: "freeradius-hq"}
	raw := []byte(`{"schemaVersion":"network-radius-accounting/v1alpha1","eventTimestamp":1788336000,"statusType":"interim-update","accountingSessionId":"session-1","sessionId":"soha-session-1","nasId":"nas-hq","userId":"user-1","deviceId":"device-1","sessionTimeSeconds":300,"inputOctets":1024,"outputOctets":2048}`)
	ack, err := service.IngestRADIUS(context.Background(), identity, raw)
	if err != nil {
		t.Fatalf("IngestRADIUS() error = %v", err)
	}
	if ack.Accepted != 1 || len(store.events) != 1 || store.events[0].EventType != networkprotocol.EventRadiusAccounting || store.events[0].ProducerKind != "freeradius" || store.events[0].Sequence != 1788336000 {
		t.Fatalf("ack/events = %#v / %#v", ack, store.events)
	}
	if !strings.Contains(string(store.events[0].Payload), `"sessionId":"soha-session-1"`) {
		t.Fatalf("RADIUS event lost Soha session correlation: %s", store.events[0].Payload)
	}
	firstID := store.events[0].EventID
	if _, err := service.IngestRADIUS(context.Background(), identity, append([]byte(" \n"), raw...)); err != nil {
		t.Fatalf("IngestRADIUS(whitespace retry) error = %v", err)
	}
	if len(store.events) != 2 || store.events[1].EventID != firstID || store.events[1].EventHash != store.events[0].EventHash {
		t.Fatalf("RADIUS retry changed identity/hash: %#v", store.events)
	}
	secret := []byte(`{"schemaVersion":"network-radius-accounting/v1alpha1","eventTimestamp":1788336000,"statusType":"start","accountingSessionId":"session-1","nasId":"nas-hq","sessionTimeSeconds":0,"userPassword":"must-not-cross-boundary"}`)
	if _, err := service.IngestRADIUS(context.Background(), identity, secret); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("IngestRADIUS(secret field) error = %v, want invalid argument", err)
	}
	if _, err := service.IngestRADIUS(context.Background(), networkidentity.Identity{Scope: networkidentity.ScopeIngest, Kind: "gateway", ID: "freeradius-hq"}, raw); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("IngestRADIUS(wrong kind) error = %v, want unauthorized", err)
	}
}
