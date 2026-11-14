package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

type proxyCloseCommandsStub struct {
	NetworkProxyConnectionCommands
	called bool
	input  domain.CloseInput
}

func (s *proxyCloseCommandsStub) CloseWithInput(_ context.Context, _ identity.Principal, id, connectionID string, input domain.CloseInput) (domain.CloseCommand, error) {
	s.called = true
	s.input = input
	return domain.CloseCommand{ID: input.RequestID, InstanceID: id, ConnectionID: connectionID, ExpiresAt: input.ExpiresAt}, nil
}

func TestProxyCloseHandlerPreservesCallerIdentityAndLegacyRequests(t *testing.T) {
	const modern = `{"requestId":"caller-id","processEpoch":"e1","issuedAt":"2026-11-09T12:00:00.000000001Z","expiresAt":"2026-11-09T12:00:10.000000002Z"}`
	for _, tt := range []struct {
		name, body, epoch string
		status            int
	}{
		{"current input", modern, "e1", http.StatusAccepted},
		{"legacy empty body", "", "", http.StatusAccepted},
		{"legacy empty object", `{}`, "", http.StatusAccepted},
		{"legacy epoch only", `{"processEpoch":"e1"}`, "e1", http.StatusAccepted},
		{"incomplete caller input", `{"processEpoch":"e1","requestId":"caller-id"}`, "", http.StatusBadRequest},
		{"null caller identity", `{"processEpoch":"e1","requestId":null}`, "", http.StatusBadRequest},
		{"malformed expiry", strings.Replace(modern, `2026-11-09T12:00:10.000000002Z`, `invalid`, 1), "", http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := &proxyCloseCommandsStub{}
			router := gin.New()
			router.POST("/:instanceID/:connectionID", NewNetworkProxyHandler(nil, stub).Close)
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/p1/c1", strings.NewReader(tt.body)))
			if resp.Code != tt.status || stub.called != (tt.status == http.StatusAccepted) || stub.input.ProcessEpoch != tt.epoch {
				t.Fatalf("status = %d, called = %v, input = %+v", resp.Code, stub.called, stub.input)
			}
			if tt.name == "current input" && (stub.input.RequestID != "caller-id" || stub.input.IssuedAt.Nanosecond() != 1 || stub.input.ExpiresAt.Sub(stub.input.IssuedAt) != 10*time.Second+time.Nanosecond) {
				t.Fatalf("caller input changed: %+v", stub.input)
			}
		})
	}
}
