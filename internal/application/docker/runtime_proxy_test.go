package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestQueryProjectLogsNormalizesTimestampAndFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req dockerRuntimeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode runtime request: %v", err)
		}
		if req.SinceSeconds != 90 || req.TailLines != 50 {
			t.Fatalf("runtime request = %#v, want since=90 tail=50", req)
		}
		_, _ = w.Write([]byte(`{"data":{"projectId":"project-1","serviceName":"web","tailLines":50,"content":"web-1 | 2026-07-31T10:00:00Z ready\nweb-1 | 2026-07-31T10:00:01Z ERROR failed\n","source":"agent_docker_cli"}}`))
	}))
	defer server.Close()

	service := newDockerRuntimeProxyTestService(server.URL)
	page, err := service.QueryProjectLogs(context.Background(), dockerRuntimeProxyPrincipal(), "project-1", domainresource.LogQuery{
		Selector: &domainresource.LogSourceSelector{DockerService: "web"}, Tail: 50, Limit: 10, Text: "error",
		Direction: sohaapi.LogDirectionForward, RuntimeOptions: &domainresource.LogRuntimeOptions{SinceSeconds: 90},
	})
	if err != nil {
		t.Fatalf("QueryProjectLogs() error = %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Message != "ERROR failed" || !page.Entries[0].Timestamp.Equal(time.Date(2026, 7, 31, 10, 0, 1, 0, time.UTC)) {
		t.Fatalf("entries = %#v, want filtered timestamped error", page.Entries)
	}
	if page.Entries[0].Source.DockerProjectID != "project-1" || page.Entries[0].Source.DockerService != "web" {
		t.Fatalf("source = %#v, want docker project/service", page.Entries[0].Source)
	}
}

type recordingDockerTicketIssuer struct {
	request domainidentity.StreamTicketRequest
}

func (r *recordingDockerTicketIssuer) IssueStreamTicket(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, request domainidentity.StreamTicketRequest) (domainidentity.StreamTicket, error) {
	r.request = request
	return domainidentity.StreamTicket{Ticket: "ticket", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func TestIssueProjectLogStreamTicketBindsResolvedService(t *testing.T) {
	issuer := &recordingDockerTicketIssuer{}
	service := newDockerRuntimeProxyTestService("http://127.0.0.1:1")
	service.logStreamTickets = issuer
	_, err := service.IssueProjectLogStreamTicket(context.Background(), dockerRuntimeProxyPrincipal(), domainidentity.AccessContext{TokenKind: "session_access"}, "project-1", domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{DockerService: "web"}})
	if err != nil {
		t.Fatalf("IssueProjectLogStreamTicket() error = %v", err)
	}
	if issuer.request.Path != "/api/v1/docker/projects/project-1/logs/stream" {
		t.Fatalf("path = %q", issuer.request.Path)
	}
	query, err := dockerLogQueryFromTicket(domainidentity.AccessContext{TokenKind: "stream_ticket", Metadata: issuer.request.Metadata}, "project-1")
	if err != nil || query.Selector == nil || query.Selector.DockerService != "web" {
		t.Fatalf("bound query = %#v error = %v", query, err)
	}
}

func TestStreamProjectLogEventsPreflightsTargetBeforeStatus(t *testing.T) {
	service := newDockerRuntimeProxyTestService("http://127.0.0.1:1")
	query := domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{DockerService: "web"}}
	accessCtx := domainidentity.AccessContext{
		TokenKind: "stream_ticket",
		Metadata: map[string]any{
			dockerLogProjectMetadataKey: "missing-project",
			dockerLogQueryMetadataKey:   query,
		},
	}
	emitted := 0
	err := service.StreamProjectLogEventsFromTicket(context.Background(), dockerRuntimeProxyPrincipal(), accessCtx, "missing-project", func(domainresource.LogStreamEvent) error {
		emitted++
		return nil
	})
	if err == nil || emitted != 0 {
		t.Fatalf("StreamProjectLogEventsFromTicket() error=%v emitted=%d, want preflight failure before status", err, emitted)
	}
}

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
		defer func() { _ = conn.Close() }()
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
