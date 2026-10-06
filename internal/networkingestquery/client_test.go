package networkingestquery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestClientReturnsOnlyValidatedAggregateSummary(t *testing.T) {
	from := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/ingest/v1/query/summary" || request.URL.Query().Get("producerId") != "endpoint-1" || request.URL.Query().Get("limit") != "20" {
			t.Fatalf("unexpected request %s", request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"from":"2026-09-03T00:00:00Z","to":"2026-09-03T01:00:00Z","eventCount":2,"heartbeatCount":1,"radiusAccountingCount":0,"networkFlowCount":0,"connectionSummaryCount":0,"proxyFlowCount":1,"uploadBytes":10,"downloadBytes":20,"activeConnections":1,"producers":[],"proxyFlows":[{"producerId":"endpoint-1","engine":"mihomo","profileId":"profile-1","profileRevision":1,"mode":"managed_follow","selectedProxy":"edge-a","uploadBytes":10,"downloadBytes":20,"activeConnections":1,"lastOccurredAt":"2026-09-03T00:59:00Z"}]}}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := client.Summary(context.Background(), domainnetworkingest.SummaryFilter{From: from, To: to, ProducerID: "endpoint-1", Limit: 20})
	if err != nil || summary.EventCount != 2 || summary.DownloadBytes != 20 || len(summary.ProxyFlows) != 1 {
		t.Fatalf("Summary() = %#v, %v", summary, err)
	}
}

func TestClientRejectsRawDestinationFields(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"from":"2026-09-03T00:00:00Z","to":"2026-09-03T01:00:00Z","eventCount":1,"heartbeatCount":0,"radiusAccountingCount":0,"networkFlowCount":0,"connectionSummaryCount":0,"proxyFlowCount":1,"uploadBytes":1,"downloadBytes":2,"activeConnections":1,"producers":[],"proxyFlows":[{"producerId":"endpoint-1","engine":"mihomo","profileId":"profile-1","profileRevision":1,"mode":"managed_follow","selectedProxy":"edge-a","uploadBytes":1,"downloadBytes":2,"activeConnections":1,"lastOccurredAt":"2026-09-03T00:59:00Z","destinationHost":"must-not-cross"}]}}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Summary(context.Background(), domainnetworkingest.SummaryFilter{})
	if !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("Summary() error = %v, want service unavailable", err)
	}
}

func TestProxyRuntimeSamplesValidateIdentityAndOrder(t *testing.T) {
	from := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	responseBody := `{"data":{"instanceId":"proxy-1","samples":[{"observedAt":"2026-09-27T08:01:00Z","uptimeSeconds":60,"uploadTotal":10,"downloadTotal":20}]}}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/ingest/v1/query/proxy-runtime/samples" || request.URL.Query().Get("instanceId") != "proxy-1" {
			t.Fatalf("unexpected sample query %s", request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, responseBody)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	samples, err := client.Samples(context.Background(), "proxy-1", from, to)
	if err != nil || len(samples) != 1 || samples[0].DownloadTotal != 20 {
		t.Fatalf("Samples() = %#v, %v", samples, err)
	}
	responseBody = `{"data":{"instanceId":"proxy-2","samples":[]}}`
	if _, err := client.Samples(context.Background(), "proxy-1", from, to); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("mismatched producer error = %v", err)
	}
	responseBody = `{"data":{"instanceId":"proxy-1","samples":[{"observedAt":"2026-09-27T08:01:00Z","uptimeSeconds":60,"uploadTotal":10,"downloadTotal":20,"destination":"secret"}]}}`
	if _, err := client.Samples(context.Background(), "proxy-1", from, to); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("unexpected detail field error = %v", err)
	}
}
