package networkcontrol

import (
	"context"
	"encoding/json"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"net/http/httptest"
	"testing"
	"time"
)

type freshnessStub struct {
	ProxyRuntime
	calls    int
	snapshot domain.ConnectionsSnapshot
}

func (s *freshnessStub) PutConnectionsSnapshot(_ context.Context, _ networkidentity.Identity, _ time.Time, _ string, v domain.ConnectionsSnapshot) error {
	s.calls++
	s.snapshot = v
	return nil
}
func (s *freshnessStub) CompleteCloseWithEpoch(context.Context, networkidentity.Identity, string, string, string, string) error {
	return nil
}
func TestSnapshotFreshnessRouter(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	stub := &freshnessStub{}
	router, err := NewRouter(&controlServiceStub{}, controlReadyStoreStub{}, Options{MaxBodyBytes: 262144, RequestsPerMinute: 100, ProxySchemas: schemas, ProxyRuntime: stub})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                        string
		freshness                   string
		garbage, foreign, populated bool
		status                      int
	}{{"empty", "fresh", false, false, false, 204}, {"populated", "fresh", false, false, true, 204}, {"legacy", "", false, false, false, 204}, {"unknown", "fresh", true, false, false, 400}, {"invalid", "invalid", false, false, false, 400}, {"foreign", "fresh", false, true, false, 401}} {
		t.Run(tc.name, func(t *testing.T) {
			connections := []domain.Connection{}
			if tc.populated {
				connections = append(connections, domain.Connection{ID: "c1", Destination: "example.com:443", Network: "tcp"})
			}
			payload := map[string]any{"engine": "mihomo", "connections": connections, "processEpoch": "e1", "sampleSequence": 0, "totalActive": len(connections), "returnedCount": len(connections), "truncated": false}
			if tc.freshness != "" {
				payload["freshness"] = tc.freshness
			}
			if tc.garbage {
				payload["garbage"] = true
			}
			raw, _ := json.Marshal(map[string]any{"schemaVersion": "proxy-runtime/v1alpha1", "runtimeId": "p1", "messageType": "connections.snapshot", "occurredAt": time.Now().UTC(), "payload": payload})
			id := "p1"
			if tc.foreign {
				id = "foreign"
			}
			before := stub.calls
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, authenticatedRuntimeRequest(t, "POST", "/api/network-control/v1/proxy-instances/p1/connections", string(raw), "proxy", id))
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			if tc.status != 204 {
				if stub.calls != before {
					t.Fatal("unexpected call")
				}
				return
			}
			assertFreshSnapshot(t, stub, before, len(connections), tc.freshness)
		})
	}
}

func assertFreshSnapshot(t *testing.T, stub *freshnessStub, before, count int, freshness string) {
	t.Helper()
	v := stub.snapshot
	if stub.calls != before+1 || v.ProcessEpoch != "e1" || v.SampleSequence != 0 || v.TotalActive == nil || *v.TotalActive != count || v.ReturnedCount == nil || *v.ReturnedCount != count || v.Truncated == nil || *v.Truncated || len(v.Connections) != count || v.Freshness != freshness {
		t.Fatalf("snapshot=%+v", v)
	}
}
