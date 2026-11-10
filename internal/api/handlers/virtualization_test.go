package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

type streamTaskUpdatesStubService struct {
	VirtualizationService
	calls int
}

func TestOperationAllowedActionsExcludeDirectCheckRetry(t *testing.T) {
	for _, kind := range []string{"connection_test", "asset_sync", "vm_action"} {
		for _, status := range []string{"failed", "canceled", "callback_timeout"} {
			item := domainvirtualization.Task{TaskKind: kind, Status: status}
			actions := operationAllowedActions(item)
			if kind == "connection_test" {
				if len(actions) != 0 {
					t.Fatalf("direct check %s actions = %v", status, actions)
				}
			} else if len(actions) != 1 || actions[0] != "retry" {
				t.Fatalf("async %s/%s actions = %v", kind, status, actions)
			}
		}
	}
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
	handler := NewVirtualizationHandlerWithServices(VirtualizationServices{Operations: service})

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

func TestMapVMDetailRedactsProviderRaw(t *testing.T) {
	mapped := mapVMDetail(appvirtualization.VMDetail{VM: domainvirtualization.VM{
		ID: "vm-1",
		Raw: map[string]any{
			"providerParams": map[string]any{
				"agentInstallScript":  "curl http://internal/install",
				"controlPlaneBaseURL": "http://internal:8080",
				"runtimeEndpoint":     "http://vm:18080",
				"sshkeys":             "ssh-ed25519 public-key",
				"storage":             "local-lvm",
			},
		},
	}})

	raw, ok := mapped["providerRaw"].(map[string]any)
	if !ok {
		t.Fatalf("providerRaw = %#v, want map", mapped["providerRaw"])
	}
	params, ok := raw["providerParams"].(map[string]any)
	if !ok {
		t.Fatalf("provider params = %#v, want map", raw["providerParams"])
	}
	for _, key := range []string{"agentInstallScript", "controlPlaneBaseURL", "runtimeEndpoint", "sshkeys"} {
		if params[key] != nil || params[key+"Configured"] != true {
			t.Fatalf("%s was not redacted: %#v", key, params)
		}
	}
	if params["storage"] != "local-lvm" {
		t.Fatalf("safe provider param was removed: %#v", params)
	}
}

func TestMapVMDetailUsesObservedResourcesBeforeFlavor(t *testing.T) {
	for _, config := range []map[string]any{{"cpu": 2, "memoryMiB": 4096, "diskGiB": 48}, nil} {
		detail := appvirtualization.VMDetail{VM: domainvirtualization.VM{Config: config}, Flavor: &domainvirtualization.Flavor{CPUCores: 1, MemoryMB: 1024, DiskGB: 40}}
		vm, ok := mapVMDetail(detail)["vm"].(gin.H)
		if !ok {
			t.Fatal("missing VM detail projection")
		}
		want := []int{2, 4096, 48}
		if config == nil {
			want = []int{1, 1024, 40}
		}
		for index, key := range []string{"cpu", "memoryMiB", "diskGiB"} {
			if vm[key] != want[index] {
				t.Fatalf("%s=%v want %d", key, vm[key], want[index])
			}
		}
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
	handler := NewVirtualizationHandlerWithServices(VirtualizationServices{Connections: service})

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
	handler := NewVirtualizationHandlerWithServices(VirtualizationServices{Connections: service})

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

type consoleURLStubService struct {
	VirtualizationService
	result consoleport.ConsoleURLResult
	err    error
}

func (s *consoleURLStubService) GetConsoleURL(context.Context, domainidentity.Principal, string) (consoleport.ConsoleURLResult, error) {
	return s.result, s.err
}

func TestStreamVMConsoleReturnsServiceUnavailableWhenConsoleNotReady(t *testing.T) {
	handler := NewVirtualizationHandler(&consoleURLStubService{result: consoleport.ConsoleURLResult{
		Type:     "novnc",
		Provider: "pve",
		Ready:    false,
		Message:  "console not ready",
	}}, func(context.Context, consoleport.ConsoleURLResult) (consoleport.Stream, error) {
		t.Error("console dial attempted before readiness check")
		return nil, consoleport.ErrBackendConnection
	})

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

func TestMapOperationDoesNotExposeEncryptedBootstrap(t *testing.T) {
	result := mapOperation(domainvirtualization.Task{ID: "task", Payload: map[string]any{"cloudInitCredential": "sealed-test-value", "cloudInitConfigured": true}})
	payload, ok := result["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", result["payload"])
	}
	if payload["cloudInitCredential"] != nil || payload["cloudInitConfigured"] != true {
		t.Fatalf("bootstrap envelope escaped: %#v", payload)
	}
}

type connectionTestStub struct {
	VirtualizationService
	status string
}

func (s *connectionTestStub) TestConnection(context.Context, domainidentity.Principal, string) (sohaapi.ConnectionCheckResult, error) {
	return sohaapi.ConnectionCheckResult{Status: s.status, Healthy: s.status == "healthy", Message: "probe result", CheckedAt: time.Now().UTC()}, nil
}
func TestConnectionReturnsCompletedResult(t *testing.T) {
	for _, status := range []string{"healthy", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			handler := NewVirtualizationHandlerWithServices(VirtualizationServices{Connections: &connectionTestStub{status: status}})
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/virtualization/clusters/connection-1/test", nil)
			ctx.Params = gin.Params{{Key: "id", Value: "connection-1"}}
			handler.TestConnection(ctx)
			if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), `"id":`) || !strings.Contains(recorder.Body.String(), `"healthy":`) || !strings.Contains(recorder.Body.String(), `"status":"`+status+`"`) || !strings.Contains(recorder.Body.String(), "probe result") {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
