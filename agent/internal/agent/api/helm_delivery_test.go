package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type helmDeliveryReaderStub struct {
	calls int
	err   error
}

func (s *helmDeliveryReaderStub) PrepareHelmDelivery(_ context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	s.calls++
	return input, s.err
}
func (s *helmDeliveryReaderStub) ExecuteHelmDelivery(_ context.Context, _ sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	s.calls++
	return sohaapi.HelmExecutionTaskResult{Stopped: true}, s.err
}

func TestHelmReadRoutesDenyMutationWrongClusterAndSecretErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, route, action, cluster, token string
		fail                                bool
		status                              int
	}{
		{"prepare", "prepare", "preflight", "one", "token", false, 200},
		{"preflight", "preflight", "preflight", "one", "token", false, 200},
		{"observe", "observe", "observe", "one", "token", false, 200},
		{"apply through prepare", "prepare", "apply", "one", "token", false, 400},
		{"apply through observe", "observe", "apply", "one", "token", false, 400},
		{"other cluster", "prepare", "preflight", "two", "token", false, 400},
		{"no credential", "prepare", "preflight", "one", "", false, 401},
		{"private provider failure", "prepare", "preflight", "one", "token", true, 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &helmDeliveryReaderStub{}
			if test.fail {
				reader.err = errors.New("password=private-value")
			}
			router := gin.New()
			group := router.Group("/platform", authMiddleware("token"))
			registerHelmDeliveryRoutes(group, reader, "one")
			input := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.HelmExecutionTaskPayloadAction(test.action), Snapshot: sohaapi.HelmDeliverySnapshot{ClusterID: test.cluster, Namespace: "dev", ReleaseName: "app", DeliveryPlanID: "plan", ApplicationID: "app", ApplicationEnvironmentID: "env", ServiceID: "svc", TargetID: "target"}}
			body, _ := json.Marshal(input)
			req := httptest.NewRequest(http.MethodPost, "/platform/helm/delivery/"+test.route, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+test.token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != test.status || strings.Contains(w.Body.String(), "private-value") {
				t.Fatalf("response=%d %s", w.Code, w.Body.String())
			}
			if test.status == 400 || test.status == 401 {
				if reader.calls != 0 {
					t.Fatal("invalid request reached runtime")
				}
			}
		})
	}
}
