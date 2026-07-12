package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	portforwardrepo "github.com/opensoha/soha/internal/repository/portforward"
)

type allowAllResourceAuthorizer struct{}

func (allowAllResourceAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{
		Allowed: true,
		AllowedActions: []domainaccess.Action{
			domainaccess.ActionList,
			domainaccess.ActionCreate,
			domainaccess.ActionUpdate,
			domainaccess.ActionDelete,
			domainaccess.ActionLogs,
		},
	}, nil
}

func TestAgentPortForwardStartsLocalTunnelThroughAgent(t *testing.T) {
	var seen []string
	localPort := testFreeLocalPort(t)
	server := newAgentPortForwardTestServer(t, localPort, &seen)
	defer server.Close()

	service := New(Dependencies{
		Agents:      testAgentClients(agentinfra.NewRegistry(0)),
		Connections: stubConnectionResolver{connection: agentConnection(server.URL)},
		Authorizer:  allowAllResourceAuthorizer{},
	})
	principal := domainidentity.Principal{UserID: "user-1"}

	created, err := service.PortForwards().RegisterPortForward(context.Background(), principal, "agent-cluster", domainresource.PortForwardRegisterInput{
		Namespace:  "platform",
		TargetKind: "Pod",
		TargetName: "api-0",
		LocalPort:  localPort,
		RemotePort: 8080,
	})
	if err != nil {
		t.Fatalf("RegisterPortForward() error = %v", err)
	}
	if created.SessionID != "agent-session" || created.Status != "active" {
		t.Fatalf("created = %#v, want active agent-session", created)
	}

	assertAgentTunnelRoundTrip(t, localPort)
	assertAgentPortForwardLifecycle(t, service.PortForwards(), principal, &seen)
}

func newAgentPortForwardTestServer(t *testing.T, localPort int, seen *[]string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/network/port-forwards":
			var req domainresource.PortForwardRegisterInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode register request: %v", err)
			}
			if req.Namespace != "platform" || req.TargetName != "api-0" || req.LocalPort != localPort || req.RemotePort != 8080 {
				t.Fatalf("unexpected register request: %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"sessionId":  "agent-session",
				"clusterId":  "agent-cluster",
				"namespace":  "platform",
				"targetKind": "Pod",
				"targetName": "api-0",
				"localPort":  localPort,
				"remotePort": 8080,
				"status":     "active",
				"createdAt":  "2026-06-12T00:00:00Z",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platform/network/port-forwards/agent-session/tunnel":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Fatalf("upgrade tunnel: %v", err)
			}
			defer func() { _ = conn.Close() }()
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read tunnel payload: %v", err)
			}
			if messageType != websocket.BinaryMessage || string(payload) != "ping" {
				t.Fatalf("tunnel message type=%d payload=%q, want binary ping", messageType, string(payload))
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, []byte("agent:pong")); err != nil {
				t.Fatalf("write tunnel payload: %v", err)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platform/network/port-forwards":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{
					"sessionId":  "agent-session",
					"clusterId":  "agent-cluster",
					"namespace":  "platform",
					"targetKind": "Pod",
					"targetName": "api-0",
					"localPort":  localPort,
					"remotePort": 8080,
					"status":     "active",
					"createdAt":  "2026-06-12T00:00:00Z",
				},
			}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/platform/network/port-forwards/agent-session":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
}

func assertAgentTunnelRoundTrip(t *testing.T, localPort int) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), time.Second)
	if err != nil {
		t.Fatalf("dial local tunnel: %v", err)
	}
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set local tunnel deadline: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write local tunnel: %v", err)
	}
	buf := make([]byte, len("agent:pong"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read local tunnel: %v", err)
	}
	if string(buf) != "agent:pong" {
		t.Fatalf("local tunnel payload = %q, want agent:pong", string(buf))
	}
	_ = conn.Close()
}

func assertAgentPortForwardLifecycle(t *testing.T, portForwards *PortForwards, principal domainidentity.Principal, seen *[]string) {
	t.Helper()
	items, err := portForwards.ListPortForwards(context.Background(), principal, "agent-cluster")
	if err != nil {
		t.Fatalf("ListPortForwards() error = %v", err)
	}
	if len(items) != 1 || items[0].SessionID != "agent-session" {
		t.Fatalf("items = %#v, want agent-session", items)
	}
	if err := portForwards.StopPortForward(context.Background(), principal, "agent-cluster", "agent-session"); err != nil {
		t.Fatalf("StopPortForward() error = %v", err)
	}
	if !sawRequest(*seen, "GET /api/v1/platform/network/port-forwards/agent-session/tunnel") {
		t.Fatalf("requests = %#v, want tunnel request", *seen)
	}
	if !sawRequest(*seen, "DELETE /api/v1/platform/network/port-forwards/agent-session") {
		t.Fatalf("requests = %#v, want delete request", *seen)
	}
}

func TestPersistRegisteredPortForwardSessionCleansUpOnRepositoryFailure(t *testing.T) {
	sessionID := "direct-session-cleanup"
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	close(doneCh)
	session := &portForwardSession{
		view: domainresource.PortForwardSessionView{
			SessionID:  sessionID,
			ClusterID:  "direct-cluster",
			Namespace:  "default",
			TargetKind: "Pod",
			TargetName: "api-0",
			LocalPort:  18080,
			RemotePort: 8080,
			Status:     "active",
			CreatedBy:  "user-1",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		stopCh: stopCh,
		doneCh: doneCh,
	}
	registerPortForwardSession(session)
	t.Cleanup(func() {
		portForwardRegistryMu.Lock()
		delete(portForwardRegistry, sessionID)
		portForwardRegistryMu.Unlock()
	})

	repoErr := errors.New("repository unavailable")
	err := persistRegisteredPortForwardSession(context.Background(), failingPortForwardRepository{err: repoErr}, session, "direct")
	if !errors.Is(err, repoErr) {
		t.Fatalf("persistRegisteredPortForwardSession() error = %v, want %v", err, repoErr)
	}

	portForwardRegistryMu.Lock()
	_, stillRegistered := portForwardRegistry[sessionID]
	portForwardRegistryMu.Unlock()
	if stillRegistered {
		t.Fatalf("session %s remained registered after repository failure", sessionID)
	}
	select {
	case <-stopCh:
	default:
		t.Fatalf("session stop channel was not closed after repository failure")
	}
}

func TestDirectPortForwardDelegatesTransportToInfrastructurePort(t *testing.T) {
	starter := &recordingDirectPortForwardStarter{session: &recordingDirectPortForwardSession{}}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{
			Summary: domaincluster.Summary{ID: "direct-port-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig},
		}},
		Authorizer:   allowAllResourceAuthorizer{},
		DirectTunnel: starter,
	})
	principal := domainidentity.Principal{UserID: "user-1"}
	created, err := service.PortForwards().RegisterPortForward(
		context.Background(), principal, "direct-port-cluster",
		domainresource.PortForwardRegisterInput{
			Namespace: "platform", TargetKind: "Service", TargetName: "api",
			LocalPort: 18080, RemotePort: 8080,
		},
	)
	if err != nil {
		t.Fatalf("RegisterPortForward() error = %v", err)
	}
	t.Cleanup(func() {
		portForwardRegistryMu.Lock()
		delete(portForwardRegistry, created.SessionID)
		portForwardRegistryMu.Unlock()
	})
	if starter.clusterID != "direct-port-cluster" {
		t.Fatalf("StartPortForward() clusterID = %q", starter.clusterID)
	}
	if starter.view.SessionID == "" || starter.view.TargetKind != "Service" || starter.view.CreatedBy != "user-1" {
		t.Fatalf("StartPortForward() view = %#v", starter.view)
	}
	if err := service.PortForwards().StopPortForward(context.Background(), principal, "direct-port-cluster", created.SessionID); err != nil {
		t.Fatalf("StopPortForward() error = %v", err)
	}
	if !starter.session.stopped {
		t.Fatal("StopPortForward() did not stop infrastructure session")
	}
}

func agentConnection(endpoint string) domaincluster.Connection {
	return domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID:             "agent-cluster",
			ConnectionMode: domaincluster.ConnectionModeAgent,
		},
		Metadata: map[string]any{
			"endpoint": endpoint,
		},
	}
}

func testAgentClients(registry *agentinfra.Registry) AgentClients {
	return AgentClients{
		Workloads:       testAgentFactory[WorkloadAgent](registry),
		Configuration:   testAgentFactory[ConfigurationAgent](registry),
		Network:         testAgentFactory[NetworkAgent](registry),
		Storage:         testAgentFactory[StorageAgent](registry),
		RBAC:            testAgentFactory[RBACAgent](registry),
		Helm:            testAgentFactory[HelmAgent](registry),
		Inventory:       testAgentFactory[InventoryAgent](registry),
		CustomResources: testAgentFactory[CustomResourceAgent](registry),
		Generic:         testAgentFactory[GenericResourceAgent](registry),
		Events:          testAgentFactory[EventAgent](registry),
		PortForwards:    testAgentFactory[PortForwardAgent](registry),
	}
}

func testAgentFactory[T any](registry *agentinfra.Registry) AgentClientFactory[T] {
	return func(connection domaincluster.Connection) (T, error) {
		var zero T
		client, err := registry.ClientFor(connection)
		if err != nil {
			return zero, err
		}
		typed, ok := any(client).(T)
		if !ok {
			return zero, fmt.Errorf("agent client does not implement requested test capability")
		}
		return typed, nil
	}
}

func testFreeLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	_, portString, err := net.SplitHostPort(listener.Addr().String())
	if closeErr := listener.Close(); closeErr != nil {
		t.Fatalf("release local port: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("split local port: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse local port: %v", err)
	}
	return port
}

func sawRequest(seen []string, want string) bool {
	for _, item := range seen {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

type failingPortForwardRepository struct {
	err error
}

type recordingDirectPortForwardStarter struct {
	clusterID string
	view      domainresource.PortForwardSessionView
	session   *recordingDirectPortForwardSession
}

func (s *recordingDirectPortForwardStarter) StartPortForward(_ context.Context, clusterID string, view domainresource.PortForwardSessionView) (DirectPortForwardSession, error) {
	s.clusterID = clusterID
	s.view = view
	return s.session, nil
}

type recordingDirectPortForwardSession struct {
	stopped bool
}

func (s *recordingDirectPortForwardSession) Stop() {
	s.stopped = true
}

func (*recordingDirectPortForwardSession) LastError() string {
	return ""
}

func (f failingPortForwardRepository) List(context.Context) ([]portforwardrepo.Record, error) {
	return nil, nil
}

func (f failingPortForwardRepository) Upsert(context.Context, portforwardrepo.Record) error {
	return f.err
}

func (f failingPortForwardRepository) Delete(context.Context, string) error {
	return nil
}

func (f failingPortForwardRepository) MarkStatus(context.Context, string, string, string) error {
	return nil
}
