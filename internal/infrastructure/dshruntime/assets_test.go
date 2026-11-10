package dshruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func TestRuntimeAssetAdapterWire(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-token" {
			t.Errorf("missing runtime authorization")
		}
		switch calls.Add(1) {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/v1/assets" {
				t.Errorf("catalog request = %s %s", r.Method, r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"items":[{"kind":"mcp","id":"example","version":"2","transport":"stdio","executionPosition":"personal-docker"}]}`)
		case 2:
			var body map[string]string
			if r.Method != http.MethodPost || r.URL.Path != "/v1/assets/mcp/probe" ||
				json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]string{"id": "example", "version": "2", "scope": "heya"}) {
				t.Errorf("probe request = %s %s %+v", r.Method, r.URL.Path, body)
			}
			_, _ = fmt.Fprint(w, `{"status":"ready","capabilities":["tools","resources"]}`)
		case 3:
			_, _ = fmt.Fprint(w, `{"status":"ready","capabilities":["private-operation"]}`)
		default:
			_, _ = fmt.Fprint(w, `{"items":[{"kind":"mcp","id":"example","version":"2","credentialFile":"/private/secret"}]}`)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, ServiceToken: "service-token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	adapter, err := NewAdapter(client)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	items, err := adapter.ListRuntimeAssets(ctx)
	want := domain.RuntimeAssetDescriptor{Kind: "mcp", ID: "example", Version: "2", Transport: "stdio", ExecutionPosition: "personal-docker"}
	if err != nil || len(items) != 1 || items[0] != want {
		t.Fatalf("catalog = %+v, %v", items, err)
	}
	if _, err := adapter.ProbeRuntimeMCP(ctx, "example", "2", "bogus"); !errors.Is(err, ErrInput) {
		t.Fatalf("invalid probe = %v", err)
	}
	probe, err := adapter.ProbeRuntimeMCP(ctx, "example", "2", "heya")
	if err != nil || probe.Status != domain.RuntimeMCPProbeReady || len(probe.Capabilities) != 2 {
		t.Fatalf("probe = %+v, %v", probe, err)
	}
	if _, err := adapter.ProbeRuntimeMCP(ctx, "example", "2", "heya"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("untyped probe capability = %v", err)
	}
	if _, err := adapter.ListRuntimeAssets(ctx); !errors.Is(err, ErrProtocol) {
		t.Fatalf("unexpected source field = %v", err)
	}
}
