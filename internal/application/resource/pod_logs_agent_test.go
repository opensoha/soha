package resource

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
)

func TestAgentStreamPodLogsDelegatesToAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/platform/workloads/pods/api-0/logs/stream" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		query := r.URL.Query()
		if query.Get("namespace") != "platform" || query.Get("container") != "app" || query.Get("tailLines") != "10" || query.Get("sinceSeconds") != "5" {
			t.Fatalf("query = %s, want namespace/container/tail/since", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("line 1\nline 2\n"))
	}))
	defer server.Close()

	service := New(Dependencies{
		Agents:      testAgentClients(agentinfra.NewRegistry(0)),
		Connections: stubConnectionResolver{connection: agentConnection(server.URL)},
		Authorizer:  allowAllResourceAuthorizer{},
		Audit:       noopResourceAuditRecorder{},
	})
	var out bytes.Buffer
	err := service.Workloads().StreamPodLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "agent-cluster", "platform", "api-0", "app", 10, 5, &out)
	if err != nil {
		t.Fatalf("StreamPodLogs() error = %v", err)
	}
	if out.String() != "line 1\nline 2\n" {
		t.Fatalf("output = %q, want streamed logs", out.String())
	}
}

func TestAgentStreamPodTerminalDelegatesToAgent(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/platform/workloads/pods/api-0/terminal" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		query := r.URL.Query()
		if query.Get("namespace") != "platform" || query.Get("container") != "app" || query.Get("shell") != "/bin/sh" {
			t.Fatalf("query = %s, want namespace/container/shell", r.URL.RawQuery)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer func() { _ = conn.Close() }()
		var message struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatalf("read input: %v", err)
		}
		if message.Type != "input" || message.Data != "whoami\n" {
			t.Fatalf("input message = %#v, want whoami", message)
		}
		_ = conn.WriteJSON(map[string]any{"type": "stdout", "data": "root\n"})
		_ = conn.WriteJSON(map[string]any{"type": "exit", "message": "terminal session closed"})
	}))
	defer server.Close()

	service := New(Dependencies{
		Agents:      testAgentClients(agentinfra.NewRegistry(0)),
		Connections: stubConnectionResolver{connection: agentConnection(server.URL)},
		Authorizer:  allowAllResourceAuthorizer{},
		Audit:       noopResourceAuditRecorder{},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := service.Workloads().StreamPodTerminal(context.Background(), domainidentity.Principal{UserID: "user-1"}, "agent-cluster", "platform", "api-0", "app", "/bin/sh", strings.NewReader("whoami\n"), &stdout, &stderr, nil)
	if err != nil {
		t.Fatalf("StreamPodTerminal() error = %v", err)
	}
	if stdout.String() != "root\n" || stderr.String() != "" {
		t.Fatalf("stdout=%q stderr=%q, want agent terminal output", stdout.String(), stderr.String())
	}
}
