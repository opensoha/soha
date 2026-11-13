package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type toolCallerRuntime struct {
	*fakeRuntimeController
	calls int
}

func (r *toolCallerRuntime) CallChatTool(context.Context, string, sohaapi.AgentRunnerToolCallRequest) (sohaapi.AgentToolCallResult, error) {
	r.calls++
	return sohaapi.AgentToolCallResult{RunID: "run-a"}, nil
}

func TestAgentToolRouteRejectsModelIdentityAndAdminToken(t *testing.T) {
	runtime := &toolCallerRuntime{fakeRuntimeController: &fakeRuntimeController{}}
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}}, nil, nil, runtime)
	for _, test := range []struct {
		body, token string
		status      int
	}{
		{`{"toolName":"knowledge.search","input":{}}`, "admin-token", http.StatusUnauthorized},
		{`{"toolName":"knowledge.search","input":{},"runId":"other"}`, "soha-tools:test", http.StatusBadRequest},
		{`{"toolName":"knowledge.search","input":{"callbackToken":"other"}}`, "soha-tools:test", http.StatusBadRequest},
		{`{"toolName":"knowledge.search"}`, "soha-tools:test", http.StatusBadRequest},
		{`{"toolName":"knowledge.search","input":{}} {}`, "soha-tools:test", http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/agent-tools", strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer "+test.token)
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d want=%d", response.Code, test.status)
		}
	}
	if runtime.calls != 0 {
		t.Fatal("invalid request reached tool runtime")
	}
}
