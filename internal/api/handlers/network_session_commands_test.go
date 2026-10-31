package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

type networkCommandReadStub struct {
	NetworkAccessService
	session, key string
	command      domainnetworkaccess.SessionCommand
}

func (s *networkCommandReadStub) GetSessionCommandForSession(_ context.Context, _ domainidentity.Principal, session, key string) (domainnetworkaccess.SessionCommand, error) {
	s.session, s.key = session, key
	return s.command, nil
}
func (s *networkCommandReadStub) GetOriginalSessionCommand(_ context.Context, _ domainidentity.Principal, session, key string) (domainnetworkaccess.SessionCommand, error) {
	s.session, s.key = session, key
	return s.command, nil
}

func TestNetworkCommandQueriesPreservePublicProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hash := "sha256:" + strings.Repeat("a", 64)
	for _, original := range []bool{false, true} {
		stub := &networkCommandReadStub{command: domainnetworkaccess.SessionCommand{ID: "command-1", SessionID: "session-1", PlanHash: hash, SubjectID: "private-subject", DeviceID: "private-device", Status: "applied"}}
		handler := &NetworkAccessHandler{service: stub}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "sessionID", Value: "session-1"}, {Key: "commandID", Value: "command-1"}}
		ctx.Request = httptest.NewRequest(http.MethodGet, "/commands?planHash="+url.QueryEscape(hash), nil)
		key := "command-1"
		if original {
			handler.GetOriginalSessionCommand(ctx)
			key = hash
		} else {
			handler.GetSessionCommand(ctx)
		}
		body := recorder.Body.String()
		if recorder.Code != 200 || stub.session != "session-1" || stub.key != key || recorder.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, `"status":"applied"`) {
			t.Fatalf("query status=%d body=%s coordinates=%s/%s", recorder.Code, body, stub.session, stub.key)
		}
		for _, private := range []string{"planHash", "radiusAttributes", "private-subject", "private-device"} {
			if strings.Contains(body, private) {
				t.Fatalf("private field in public response: %s", private)
			}
		}
	}
}
