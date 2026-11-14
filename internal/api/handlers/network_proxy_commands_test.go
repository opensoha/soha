package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type typedProxyCommandStub struct{ calls int }

func (s *typedProxyCommandStub) QueueCommand(context.Context, identity.Principal, string, domain.RuntimeCommandInput) (domain.RuntimeCommandReceipt, error) {
	s.calls++
	return domain.RuntimeCommandReceipt{CommandID: "r1", RequestID: "r1", Status: "pending"}, nil
}
func (s *typedProxyCommandStub) GetCommand(context.Context, identity.Principal, string, string) (domain.RuntimeCommandReceipt, error) {
	return domain.RuntimeCommandReceipt{CommandID: "r1", Status: "running"}, nil
}
func (s *typedProxyCommandStub) CancelCommand(context.Context, identity.Principal, string, string) error {
	return nil
}
func TestProxyTypedCommandsUseReceiptWireAndRejectPassthrough(t *testing.T) {
	for _, tt := range []struct {
		body  string
		want  int
		calls int
	}{{`{"requestId":"r1","processEpoch":"e1","kind":"groups","issuedAt":"2026-11-08T00:00:00Z","expiresAt":"2026-11-08T00:00:20Z"}`, 202, 1}, {`{"controllerPath":"/configs","body":{"mode":"direct"}}`, 400, 0}} {
		s := &typedProxyCommandStub{}
		h := NewNetworkProxyCommandsHandler(s)
		out := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(out)
		c.Request = httptest.NewRequest(http.MethodPost, "/proxy-instances/p1/commands", strings.NewReader(tt.body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "instanceID", Value: "p1"}}
		h.Queue(c)
		if out.Code != tt.want || s.calls != tt.calls {
			t.Fatalf("status=%d body=%s calls=%d", out.Code, out.Body, s.calls)
		}
		if tt.want == 202 && (!strings.Contains(out.Body.String(), `"data"`) || !strings.Contains(out.Body.String(), `"commandId":"r1"`)) {
			t.Fatal("receipt envelope disagrees with contract")
		}
	}
}
