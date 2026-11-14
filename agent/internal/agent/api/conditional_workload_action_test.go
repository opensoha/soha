package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"go.uber.org/zap"
)

type conditionalActionSpy struct{ calls int }

func (s *conditionalActionSpy) ExecuteWorkloadActionConditional(_ context.Context, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	s.calls++
	return contractresource.ConditionalWorkloadActionReceipt{Applied: !input.DryRun, DryRun: input.DryRun, Namespace: namespace, Kind: kind, Name: name, Action: action, UID: input.Preconditions.ExpectedUID, ResourceVersion: "rv", Preconditions: input.Preconditions}, nil
}

func TestConditionalWorkloadActionUsesExistingExactAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []struct{ kind, action, extra string }{{"Deployment", "restart", ""}, {"Deployment", "scale", `,"replicas":0`}, {"Deployment", "rollback", `,"revision":"2"`}, {"StatefulSet", "restart", ""}, {"StatefulSet", "scale", `,"replicas":2`}, {"DaemonSet", "restart", ""}} {
		for _, allowed := range []bool{true, false} {
			spy := &conditionalActionSpy{}
			list := []string{actionPlatformResourcesApply}
			if allowed {
				list = []string{workloadActionPolicy(target.kind, target.action)}
			}
			router := gin.New()
			registerConditionalWorkloadActionRoute(router.Group("/platform"), spy, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: list}, zap.NewNop(), nil))
			body := `{"preconditions":{"expectedUid":"uid","expectedResourceVersion":"rv"},"dryRun":true` + target.extra + `}`
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/platform/preconditions-v1/resources/"+target.kind+"/app/actions/"+target.action+"?namespace=team-a", strings.NewReader(body)))
			if allowed && (recorder.Code != 200 || spy.calls != 1) || !allowed && (recorder.Code != 403 || spy.calls != 0) {
				t.Fatalf("%s/%s allowed=%t status=%d calls=%d", target.kind, target.action, allowed, recorder.Code, spy.calls)
			}
		}
	}
}
