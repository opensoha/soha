package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestStreamProjectLogsProxiesToDockerAgentRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/docker/runtime/logs/stream" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer runtime-token" {
			t.Fatalf("Authorization = %q, want runtime token", r.Header.Get("Authorization"))
		}
		var req dockerRuntimeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode runtime request: %v", err)
		}
		if req.ProjectID != "project-1" || req.ServiceName != "web" || req.TailLines != 25 || !strings.Contains(req.ComposeContent, "nginx") {
			t.Fatalf("runtime request = %#v, want project web logs", req)
		}
		_, _ = w.Write([]byte("web line 1\nweb line 2\n"))
	}))
	defer server.Close()

	service := newDockerRuntimeProxyTestService(server.URL)
	var out bytes.Buffer
	if err := service.StreamProjectLogs(context.Background(), dockerRuntimeProxyPrincipal(), "project-1", "web", 25, &out); err != nil {
		t.Fatalf("StreamProjectLogs() error = %v", err)
	}
	if out.String() != "web line 1\nweb line 2\n" {
		t.Fatalf("output = %q, want streamed logs", out.String())
	}
}

func TestStreamProjectTerminalProxiesToDockerAgentRuntime(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/docker/runtime/terminal" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-token" {
			t.Fatalf("Authorization = %q, want runtime token", r.Header.Get("Authorization"))
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		var init dockerRuntimeMessage
		if err := conn.ReadJSON(&init); err != nil {
			t.Fatalf("read init: %v", err)
		}
		if init.Type != "init" {
			t.Fatalf("init message = %#v, want init", init)
		}
		var req dockerRuntimeRequest
		if err := json.Unmarshal([]byte(init.Data), &req); err != nil {
			t.Fatalf("decode init data: %v", err)
		}
		if req.ProjectID != "project-1" || req.ServiceName != "web" || req.Shell != "/bin/sh" || !strings.Contains(req.ComposeContent, "nginx") {
			t.Fatalf("runtime request = %#v, want project web terminal", req)
		}
		var input dockerRuntimeMessage
		if err := conn.ReadJSON(&input); err != nil {
			t.Fatalf("read input: %v", err)
		}
		if input.Type != "input" || input.Data != "pwd\n" {
			t.Fatalf("input message = %#v, want pwd", input)
		}
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "stdout", Data: "/app\n"})
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "stderr", Data: "warn\n"})
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "exit", Message: "terminal session closed"})
	}))
	defer server.Close()

	service := newDockerRuntimeProxyTestService(server.URL)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := service.StreamProjectTerminal(context.Background(), dockerRuntimeProxyPrincipal(), "project-1", "web", "/bin/sh", strings.NewReader("pwd\n"), &stdout, &stderr); err != nil {
		t.Fatalf("StreamProjectTerminal() error = %v", err)
	}
	if stdout.String() != "/app\n" || stderr.String() != "warn\n" {
		t.Fatalf("stdout=%q stderr=%q, want bridged terminal output", stdout.String(), stderr.String())
	}
}

func newDockerRuntimeProxyTestService(endpoint string) *Service {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Endpoint: endpoint, Status: "ready"}
	repo.projects["project-1"] = domaindocker.Project{
		ID:         "project-1",
		HostID:     "host-1",
		Name:       "Demo",
		Slug:       "demo",
		Status:     "running",
		EnvContent: "ENV=prod\n",
		ComposeContent: `services:
  web:
    image: nginx
`,
	}
	return New(repo, dockerTestPermissions(), nil, WithRuntimeBearerToken("runtime-token"))
}

func dockerRuntimeProxyPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}}
}
