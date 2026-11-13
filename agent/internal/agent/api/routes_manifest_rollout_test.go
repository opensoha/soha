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
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type manifestRolloutStub struct {
	calls int
	err   error
}

func (s *manifestRolloutStub) ExecuteManifestTask(_ context.Context, input sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	s.calls++
	return sohaapi.ManifestExecutionTaskResult{Action: input.Action, Rollout: &sohaapi.ProgressiveRolloutStatus{UID: "native-uid", ResourceVersion: "42"}}, s.err
}

func TestManifestRolloutRoutesProtectControlsAndNeverDispatchApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, route, action, cluster, token string
		allow, fail, extra                  bool
		status                              int
	}{
		{"observe", "observe", "observe", "one", "token", false, false, false, 200},
		{"control", "control", "rollout_control", "one", "token", true, false, false, 200},
		{"control denied", "control", "rollout_control", "one", "token", false, false, false, 403},
		{"apply bypass", "control", "apply", "one", "token", true, false, false, 400},
		{"read bypass", "observe", "rollout_control", "one", "token", false, false, false, 400},
		{"wrong cluster", "control", "rollout_control", "two", "token", true, false, false, 400},
		{"missing credential", "control", "rollout_control", "one", "", true, false, false, 401},
		{"private failure", "control", "rollout_control", "one", "token", true, true, false, 409},
		{"trailing json", "observe", "observe", "one", "token", false, false, true, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &manifestRolloutStub{}
			if test.fail {
				executor.err = errors.New("token=private-value")
			}
			config := cfgpkg.SecurityConfig{}
			if test.allow {
				config.AllowedActions = []string{actionPlatformCustomResourcesApply}
			}
			router := gin.New()
			registerManifestRolloutRoutes(router.Group("/platform/ownership-v2", authMiddleware("token")), executor, "one", newActionPolicy(config, nil, nil))
			input := sohaapi.ManifestExecutionTaskPayload{Action: sohaapi.ManifestTaskAction(test.action), ClusterID: test.cluster, Documents: []sohaapi.ManifestRenderedDocument{{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout"}}}
			body, _ := json.Marshal(input)
			if test.extra {
				body = append(body, []byte(" {}")...)
			}
			req := httptest.NewRequest(http.MethodPost, "/platform/ownership-v2/manifests/rollout/"+test.route, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.status || strings.Contains(response.Body.String(), "private-value") {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			wantCalls := 0
			if test.status == 200 || test.fail {
				wantCalls = 1
			}
			if executor.calls != wantCalls {
				t.Fatalf("runtime calls=%d, want %d", executor.calls, wantCalls)
			}
		})
	}
}
