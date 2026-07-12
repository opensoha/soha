package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

type streamTaskUpdatesStubService struct {
	VirtualizationService
	calls int
}

func (s *streamTaskUpdatesStubService) GetOperation(_ context.Context, _ domainidentity.Principal, _ string) (domainvirtualization.Task, error) {
	s.calls++
	if s.calls == 1 {
		startedAt := time.Now().UTC()
		return domainvirtualization.Task{
			ID:        "task-1",
			TaskKind:  "vm_action",
			Status:    "running",
			StartedAt: &startedAt,
		}, nil
	}
	return domainvirtualization.Task{}, context.Canceled
}

func TestStreamTaskUpdatesUsesFixedErrorMessage(t *testing.T) {
	service := &streamTaskUpdatesStubService{}
	handler := NewVirtualizationHandler(service)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/tasks/task-1/stream", nil)
	ctx.Params = gin.Params{{Key: "taskID", Value: "task-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})

	handler.StreamTaskUpdates(ctx)

	body := recorder.Body.String()
	if !strings.Contains(body, `event: error`) {
		t.Fatalf("expected error event, got %q", body)
	}
	if !strings.Contains(body, `"error":"task stream closed"`) {
		t.Fatalf("expected fixed error message, got %q", body)
	}
	if strings.Contains(body, "context canceled") {
		t.Fatalf("expected backend error to be redacted, got %q", body)
	}
}

func TestBackendWebSocketDialerUsesConsoleTLSOptions(t *testing.T) {
	result := consoleport.ConsoleURLResult{
		BackendTLS: consoleport.BackendTLS{
			ServerName:         "k8s.example",
			InsecureSkipVerify: true,
		},
	}

	dialer, err := backendWebSocketDialer(result)
	if err != nil {
		t.Fatalf("backendWebSocketDialer() error = %v", err)
	}
	if dialer.TLSClientConfig == nil {
		t.Fatalf("TLSClientConfig is nil")
	}
	if dialer.TLSClientConfig.ServerName != "k8s.example" {
		t.Fatalf("ServerName = %q", dialer.TLSClientConfig.ServerName)
	}
	if !dialer.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify = false, want true")
	}
}

func TestBackendWebSocketDialerRejectsInvalidTLSMaterial(t *testing.T) {
	_, err := backendWebSocketDialer(consoleport.ConsoleURLResult{
		BackendTLS: consoleport.BackendTLS{CAData: []byte("not pem")},
	})
	if err == nil {
		t.Fatal("backendWebSocketDialer() error = nil")
	}
}

func TestMapOperationIncludesDerivedOperationState(t *testing.T) {
	state := &domainvirtualization.OperationState{Phase: "failed", Retryable: true}
	mapped := mapOperation(domainvirtualization.Task{
		ID:             "task-1",
		TaskKind:       "vm_action",
		Status:         "failed",
		OperationState: state,
	})

	if mapped["operationState"] != state {
		t.Fatalf("operationState = %#v, want %#v", mapped["operationState"], state)
	}
}

func TestMapOperationRedactsSensitivePayload(t *testing.T) {
	mapped := mapOperation(domainvirtualization.Task{
		ID:       "task-1",
		TaskKind: "vm_create",
		Payload: map[string]any{
			"cloudInit": "#cloud-config\npassword: secret",
			"providerParams": map[string]any{
				"runnerToken": "runner-secret",
				"storage":     "local-lvm",
			},
		},
	})

	payload, ok := mapped["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want map", mapped["payload"])
	}
	if payload["cloudInit"] != nil || payload["cloudInitConfigured"] != true {
		t.Fatalf("cloudInit was not redacted: %#v", payload)
	}
	providerParams, ok := payload["providerParams"].(map[string]any)
	if !ok {
		t.Fatalf("provider params = %#v, want map", payload["providerParams"])
	}
	if providerParams["runnerToken"] != nil || providerParams["runnerTokenConfigured"] != true || providerParams["storage"] != "local-lvm" {
		t.Fatalf("provider params redaction = %#v", providerParams)
	}
}

func TestMapOperationPreservesConfiguredFlags(t *testing.T) {
	mapped := mapOperation(domainvirtualization.Task{
		ID:       "task-1",
		TaskKind: "vm_create",
		Result: map[string]any{
			"connectionSnapshot": map[string]any{
				"credentialConfigured":            true,
				"prometheusBearerTokenConfigured": true,
			},
		},
	})

	result, ok := mapped["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %#v, want map", mapped["result"])
	}
	snapshot, ok := result["connectionSnapshot"].(map[string]any)
	if !ok {
		t.Fatalf("connection snapshot = %#v, want map", result["connectionSnapshot"])
	}
	if snapshot["credentialConfigured"] != true || snapshot["prometheusBearerTokenConfigured"] != true {
		t.Fatalf("configured flags were not preserved: %#v", snapshot)
	}
	if snapshot["credentialConfiguredConfigured"] != nil || snapshot["prometheusBearerTokenConfiguredConfigured"] != nil {
		t.Fatalf("configured flags were redacted as sensitive values: %#v", snapshot)
	}
}

func TestMapConnectionRedactsPrometheusBearerToken(t *testing.T) {
	mapped := mapConnection(domainvirtualization.Connection{
		ID:       "conn-1",
		Provider: "kubevirt",
		Name:     "kv",
		Config: map[string]any{
			"backendUrl":            "https://kube.example:6443",
			"prometheusUrl":         "https://prometheus.example",
			"prometheusBearerToken": "secret-token",
		},
	})

	config, ok := mapped["config"].(map[string]any)
	if !ok {
		t.Fatalf("config = %#v, want map", mapped["config"])
	}
	if config["prometheusBearerToken"] != nil {
		t.Fatalf("config leaked token: %#v", config)
	}
	if config["prometheusBearerTokenConfigured"] != true {
		t.Fatalf("token configured flag = %#v", config)
	}
}

func TestMapVMAndImageExposeOrphanHint(t *testing.T) {
	vm := mapVM(domainvirtualization.VM{ID: "vm-1", Config: map[string]any{"source": "sync"}})
	if vm["orphanHint"] != "provider_discovered" {
		t.Fatalf("vm orphan hint = %#v", vm["orphanHint"])
	}
	image := mapImage(domainvirtualization.Image{ID: "image-1", Config: map[string]any{"orphanHint": "provider_discovered"}})
	if image["orphanHint"] != "provider_discovered" {
		t.Fatalf("image orphan hint = %#v", image["orphanHint"])
	}
}

type deleteConnectionStubService struct {
	VirtualizationService
	deleteID    string
	deleteForce bool
	deps        domainvirtualization.ConnectionDeleteDependencies
}

func (s *deleteConnectionStubService) DeleteConnection(_ context.Context, _ domainidentity.Principal, id string, opts appvirtualization.DeleteConnectionOptions) error {
	s.deleteID = id
	s.deleteForce = opts.Force
	return nil
}

func (s *deleteConnectionStubService) GetConnectionDeleteDependencies(_ context.Context, _ domainidentity.Principal, id string) (domainvirtualization.ConnectionDeleteDependencies, error) {
	s.deps.Connection.ID = id
	return s.deps, nil
}

func TestDeleteConnectionPassesForceQuery(t *testing.T) {
	service := &deleteConnectionStubService{}
	handler := NewVirtualizationHandler(service)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/virtualization/clusters/conn-1?force=true", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "conn-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})

	handler.DeleteConnection(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.deleteID != "conn-1" || !service.deleteForce {
		t.Fatalf("delete call id=%q force=%v", service.deleteID, service.deleteForce)
	}
}

func TestGetConnectionDeleteDependenciesMapsPreview(t *testing.T) {
	service := &deleteConnectionStubService{
		deps: domainvirtualization.ConnectionDeleteDependencies{
			Connection:       domainvirtualization.Connection{Name: "pve-a", Provider: "pve"},
			VMCount:          2,
			ImageCount:       3,
			FlavorCount:      1,
			TaskCount:        4,
			PendingTaskCount: 0,
			DockerHostCount:  1,
			ForceRequired:    true,
			Blocking:         true,
			BlockingReasons:  []string{"virtual_machines", "docker_hosts"},
		},
	}
	handler := NewVirtualizationHandler(service)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/virtualization/clusters/conn-1/delete-dependencies", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "conn-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})

	handler.GetConnectionDeleteDependencies(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"dockerHostCount":1`) || !strings.Contains(recorder.Body.String(), `"forceRequired":true`) {
		t.Fatalf("dependency preview body = %s", recorder.Body.String())
	}
}

func TestProxyWebsocketCopiesFullMessages(t *testing.T) {
	clientProxyConn, clientConn := newWebSocketTestPair(t)
	backendProxyConn, backendConn := newWebSocketTestPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxyWebsocket(ctx, clientProxyConn, backendProxyConn)
	}()

	payload := bytes.Repeat([]byte("x"), 96*1024)
	if err := clientConn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatalf("write client message: %v", err)
	}

	if err := backendConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set backend read deadline: %v", err)
	}
	messageType, got, err := backendConn.ReadMessage()
	if err != nil {
		t.Fatalf("read backend message: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("message type = %d, want %d", messageType, websocket.BinaryMessage)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("message length = %d, want %d", len(got), len(payload))
	}

	ack := []byte("console-ready")
	if err := backendConn.WriteMessage(websocket.TextMessage, ack); err != nil {
		t.Fatalf("write backend message: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	messageType, got, err = clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read client message: %v", err)
	}
	if messageType != websocket.TextMessage {
		t.Fatalf("message type = %d, want %d", messageType, websocket.TextMessage)
	}
	if !bytes.Equal(got, ack) {
		t.Fatalf("message = %q, want %q", got, ack)
	}

	cancel()
	waitProxyDone(t, done)
}

func TestProxyPVEVNCDialsBackendWithTicketCookieAndQuery(t *testing.T) {
	errCh := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("PVEAuthCookie"); err != nil || cookie.Value != "ticket-1" {
			errCh <- fmt.Errorf("PVEAuthCookie = %#v err=%v", cookie, err)
			http.Error(w, "bad cookie", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("vncticket") != "ticket-1" || r.URL.Query().Get("port") != "5901" {
			errCh <- fmt.Errorf("query = %s", r.URL.RawQuery)
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer func() { _ = conn.Close() }()
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		if messageType != websocket.BinaryMessage || string(payload) != "hello" {
			errCh <- fmt.Errorf("message type=%d payload=%q", messageType, payload)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte("ready")); err != nil {
			errCh <- err
		}
	}))
	defer backend.Close()

	clientProxyConn, clientConn := newWebSocketTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxyPVEVNC(ctx, clientProxyConn, backend.URL+"/api2/json/nodes/pve-a/qemu/101/vncwebsocket?port=5901", "ticket-1", consoleport.ConsoleURLResult{})
	}()

	if err := clientConn.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
		t.Fatalf("write client message: %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	messageType, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read client response: %v", err)
	}
	if messageType != websocket.TextMessage || string(payload) != "ready" {
		t.Fatalf("client response type=%d payload=%q", messageType, payload)
	}
	select {
	case err := <-errCh:
		t.Fatalf("backend validation error: %v", err)
	default:
	}
	cancel()
	waitProxyDone(t, done)
}

func TestProxyPVEVNCUsesInsecureTLSForSelfSignedBackend(t *testing.T) {
	errCh := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("vncticket") != "ticket-tls" {
			errCh <- fmt.Errorf("query = %s", r.URL.RawQuery)
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_, payload, err := conn.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		if string(payload) != "ping" {
			errCh <- fmt.Errorf("payload=%q", payload)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte("pong")); err != nil {
			errCh <- err
		}
	}))
	defer backend.Close()

	clientProxyConn, clientConn := newWebSocketTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxyPVEVNC(ctx, clientProxyConn, backend.URL+"/vncwebsocket", "ticket-tls", consoleport.ConsoleURLResult{
			BackendTLS: consoleport.BackendTLS{InsecureSkipVerify: true},
		})
	}()

	if err := clientConn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write client message: %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read client response: %v", err)
	}
	if string(payload) != "pong" {
		t.Fatalf("payload = %q, want pong", payload)
	}
	select {
	case err := <-errCh:
		t.Fatalf("backend validation error: %v", err)
	default:
	}
	cancel()
	waitProxyDone(t, done)
}

type consoleURLStubService struct {
	VirtualizationService
	result consoleport.ConsoleURLResult
}

func (s *consoleURLStubService) GetConsoleURL(context.Context, domainidentity.Principal, string) (consoleport.ConsoleURLResult, error) {
	return s.result, nil
}

func TestStreamVMConsoleReturnsServiceUnavailableWhenConsoleNotReady(t *testing.T) {
	handler := NewVirtualizationHandler(&consoleURLStubService{result: consoleport.ConsoleURLResult{
		Type:     "novnc",
		Provider: "pve",
		Ready:    false,
		Message:  "console not ready",
	}})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/virtualization/vms/vm-1/console/novnc", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "vm-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})

	handler.StreamVMConsole(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "console not ready") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestProxyWebsocketStopsOnContextCancel(t *testing.T) {
	clientProxyConn, _ := newWebSocketTestPair(t)
	backendProxyConn, _ := newWebSocketTestPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxyWebsocket(ctx, clientProxyConn, backendProxyConn)
	}()

	cancel()
	waitProxyDone(t, done)
}

func TestWriteWebsocketProxyErrorRedactsBackendDetails(t *testing.T) {
	clientProxyConn, clientConn := newWebSocketTestPair(t)
	defer func() { _ = clientProxyConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	writeWebsocketProxyError(clientProxyConn, "backend connection failed")

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	messageType, got, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read client message: %v", err)
	}
	if messageType != websocket.TextMessage {
		t.Fatalf("message type = %d, want %d", messageType, websocket.TextMessage)
	}
	var payload map[string]string
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("decode proxy error: %v", err)
	}
	if payload["error"] != "backend connection failed" {
		t.Fatalf("error = %q, want backend connection failed", payload["error"])
	}
	if strings.Contains(string(got), "refused") || strings.Contains(string(got), "dial") || strings.Contains(string(got), "tls") {
		t.Fatalf("expected backend details to be redacted, got %q", got)
	}
}

func newWebSocketTestPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	var serverConn *websocket.Conn
	var clientConn *websocket.Conn
	release := make(chan struct{})
	connCh := make(chan *websocket.Conn, 1)
	errCh := make(chan error, 1)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		connCh <- conn
		<-release
	}))

	t.Cleanup(func() {
		if clientConn != nil {
			_ = clientConn.Close()
		}
		if serverConn != nil {
			_ = serverConn.Close()
		}
		close(release)
		server.Close()
	})

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	var err error
	clientConn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		t.Fatalf("dial websocket test server: %v", err)
	}

	select {
	case serverConn = <-connCh:
	case err := <-errCh:
		t.Fatalf("upgrade websocket test server: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket test server connection")
	}

	return serverConn, clientConn
}

func waitProxyDone(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket proxy to stop")
	}
}
