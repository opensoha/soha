package networkcontrol

import (
	"context"
	"encoding/json"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type proxyCommandsStub struct {
	calls  int
	epoch  string
	result domain.RuntimeCommandResult
}

func (s *proxyCommandsStub) NextCommand(_ context.Context, _ networkidentity.Identity, epoch string) (*domain.RuntimeCommand, error) {
	s.calls++
	s.epoch = epoch
	return &domain.RuntimeCommand{CommandID: "request-1", Input: domain.RuntimeCommandInput{RequestID: "request-1", ProcessEpoch: epoch, Kind: "groups", IssuedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Second)}}, nil
}
func (s *proxyCommandsStub) CompleteCommand(_ context.Context, _ networkidentity.Identity, result domain.RuntimeCommandResult) error {
	s.calls++
	s.result = result
	return nil
}
func TestProxyCommandsRequireMatchingMTLSIdentity(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	stub := &proxyCommandsStub{}
	router, err := NewRouter(&controlServiceStub{}, controlReadyStoreStub{}, Options{MaxBodyBytes: 262144, RequestsPerMinute: 100, ProxySchemas: schemas, ProxyCommands: stub})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/network-control/v1/proxy-instances/p1/commands/next?processEpoch=e1"
	for _, kind := range []string{"endpoint", "proxy"} {
		request := authenticatedRuntimeRequest(t, http.MethodGet, path, "", kind, "p1")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, request)
		if kind == "endpoint" && rec.Code != http.StatusForbidden {
			t.Fatalf("endpoint status %d", rec.Code)
		}
		if kind == "proxy" && (rec.Code != 200 || stub.epoch != "e1") {
			t.Fatalf("proxy status %d", rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodGet, path, "", "proxy", "foreign"))
	if rec.Code != 401 || stub.calls != 1 {
		t.Fatal("foreign runtime reached command port")
	}
	now := time.Now().UTC()
	receipt := domain.RuntimeCommandReceipt{CommandID: "request-1", RequestID: "request-1", ProcessEpoch: "e1", Kind: "groups", Status: "succeeded", ExpiresAt: now.Add(20 * time.Second)}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": "proxy-runtime/v1alpha1", "runtimeId": "p1", "messageType": "command.result", "occurredAt": now, "payload": receipt})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/proxy-instances/p1/commands/request-1/result", string(raw), "proxy", "p1"))
	if rec.Code != 204 || stub.result.Kind != "groups" || !stub.result.ExpiresAt.Equal(receipt.ExpiresAt) {
		t.Fatalf("result status=%d body=%s result=%+v", rec.Code, rec.Body, stub.result)
	}
}
