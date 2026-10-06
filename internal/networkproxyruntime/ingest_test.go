package networkproxyruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestIngestClientSendsInstanceSample(t *testing.T) {
	var batch networkprotocol.IngestBatch
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/ingest/v1/events:batch" || request.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&batch); err != nil {
			t.Errorf("decode sample batch: %v", err)
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	transport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("test server has no HTTP transport")
	}
	client := NewIngestClient(server.URL, "proxy-1", transport.TLSClientConfig)
	active := 2
	if err := client.Send(context.Background(), domain.Observation{Engine: domain.EngineMihomo, UptimeSeconds: 60,
		UploadTotal: 100, DownloadTotal: 200, ActiveConnections: &active}); err != nil {
		t.Fatal(err)
	}
	if batch.ProducerKind != "proxy" || batch.ProducerID != "proxy-1" || len(batch.Events) != 1 || batch.Events[0].Type != networkprotocol.EventProxyRuntimeSample {
		t.Fatalf("sample batch = %+v", batch)
	}
	var payload networkprotocol.ProxyRuntimeSample
	if err := json.Unmarshal(batch.Events[0].Payload, &payload); err != nil || payload.DownloadTotal != 200 || payload.ActiveConnections == nil || *payload.ActiveConnections != 2 {
		t.Fatalf("sample payload = %+v, %v", payload, err)
	}
}
