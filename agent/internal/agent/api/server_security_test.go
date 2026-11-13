package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/opensoha/soha-agent/internal/agent/buildinfo"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestWriteErrorDoesNotExposeInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writeError(ctx, errors.New("dial tcp 10.0.0.5:6443: tls handshake failed"))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "cluster request failed") {
		t.Fatalf("body missing public error message: %s", body)
	}
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "tls handshake failed") {
		t.Fatalf("body leaked internal error: %s", body)
	}
}

func TestServerDoesNotTrustForwardedClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}}, zap.NewNop(), nil, nil)
	router, ok := server.httpServer.Handler.(*gin.Engine)
	if !ok {
		t.Fatalf("handler type = %T, want *gin.Engine", server.httpServer.Handler)
	}
	router.GET("/client-ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	req.RemoteAddr = "127.0.0.1:43210"
	req.Header.Set("X-Forwarded-For", "203.0.113.25")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if got := recorder.Body.String(); got != "127.0.0.1" {
		t.Fatalf("ClientIP() = %q, want remote peer", got)
	}
}

func TestDockerRuntimeWriteErrorDoesNotExposeInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	dockerRuntimeWriteError(ctx, errors.New("docker compose failed: password=secret"))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, dockerRuntimeRequestFailedMessage) {
		t.Fatalf("body missing public error message: %s", body)
	}
	if strings.Contains(body, "password=secret") || strings.Contains(body, "docker compose failed") {
		t.Fatalf("body leaked internal error: %s", body)
	}
}

func TestRuntimeCancelDeniedWhenActionNotAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.WarnLevel)
	runtime := &fakeRuntimeController{}
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.New(core), nil, runtime)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/execution-tasks/task-1/cancel", bytes.NewBufferString(`{"reason":"test"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if runtime.cancelCalled {
		t.Fatal("CancelActiveTask() was called for denied action")
	}
	if logs.FilterMessage("agent action audit").Len() != 1 {
		t.Fatalf("audit log count = %d, want 1", logs.FilterMessage("agent action audit").Len())
	}
}

func TestObservedCustomResourceDeleteRequiresAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, allowed := range []bool{false, true} {
		configuration := cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"}}
		want := http.StatusForbidden
		if allowed {
			configuration.Security.AllowedActions = []string{actionPlatformCustomResourcesDelete}
			want = http.StatusBadRequest
		}
		server := New(configuration, zap.NewNop(), &k8sagent.Client{}, nil)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/extensions/custom-resources/delete-observed", bytes.NewBufferString(`{}`))
		request.Header.Set("Authorization", "Bearer agent-token")
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("allowed=%v status=%d, want=%d", allowed, response.Code, want)
		}
	}
}

func TestCRDDefinitionDeleteRequiresDedicatedActionAndIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, actions := range [][]string{nil, {actionPlatformResourcesDelete}, {actionPlatformCRDsDelete}} {
		configuration := cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"}, Security: cfgpkg.SecurityConfig{AllowedActions: actions}}
		server := New(configuration, zap.NewNop(), &k8sagent.Client{}, nil)
		for _, identity := range []string{"", "?expectedUid=%20", "?expectedUid=" + strings.Repeat("a", 129)} {
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/platform/extensions/crds/widgets.example.com"+identity, nil)
			request.Header.Set("Authorization", "Bearer agent-token")
			response := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(response, request)
			want := http.StatusForbidden
			if len(actions) > 0 && actions[0] == actionPlatformCRDsDelete {
				want = http.StatusBadRequest
			}
			if response.Code != want {
				t.Fatalf("actions=%v identity=%q status=%d want=%d", actions, identity, response.Code, want)
			}
		}
	}
}

func TestRuntimeCancelAllowedWhenActionAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.InfoLevel)
	runtime := &fakeRuntimeController{}
	server := New(cfgpkg.Config{
		HTTP:     cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth:     cfgpkg.AuthConfig{BearerToken: "agent-token"},
		Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionRuntimeExecutionTaskCancel}},
	}, zap.New(core), nil, runtime)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/execution-tasks/task-1/cancel", bytes.NewBufferString(`{"reason":"test"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if !runtime.cancelCalled {
		t.Fatal("CancelActiveTask() was not called for allowed action")
	}
	if logs.FilterMessage("agent action audit").Len() != 1 {
		t.Fatalf("audit log count = %d, want 1", logs.FilterMessage("agent action audit").Len())
	}
}

func TestResourceYAMLApplyDeniedWhenActionNotAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.WarnLevel)
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.New(core), &k8sagent.Client{}, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/resources/yaml", bytes.NewBufferString(`{"namespace":"default","kind":"ConfigMap","name":"app","content":"apiVersion: v1"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if logs.FilterMessage("agent action audit").Len() != 1 {
		t.Fatalf("audit log count = %d, want 1", logs.FilterMessage("agent action audit").Len())
	}
}

func TestCustomResourceCreateDeniedWhenActionNotAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.WarnLevel)
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.New(core), &k8sagent.Client{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/extensions/custom-resources", bytes.NewBufferString(`{"definition":{"group":"example.com","version":"v1","resource":"widgets","kind":"Widget","namespaced":true},"namespace":"default","content":"apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: sample\n  namespace: default\n"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if logs.FilterMessage("agent action audit").Len() != 1 {
		t.Fatalf("audit log count = %d, want 1", logs.FilterMessage("agent action audit").Len())
	}
}

func TestHelmInstallDeniedWhenActionNotAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.WarnLevel)
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.New(core), &k8sagent.Client{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/helm/charts/install", bytes.NewBufferString(`{"repositoryUrl":"https://charts.example","chartName":"nginx","version":"1.2.3","releaseName":"edge","namespace":"platform"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if logs.FilterMessage("agent action audit").Len() != 1 {
		t.Fatalf("audit log count = %d, want 1", logs.FilterMessage("agent action audit").Len())
	}
}

func TestPodTerminalDeniedWhenActionNotAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.WarnLevel)
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.New(core), &k8sagent.Client{}, nil)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	defer httpServer.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer agent-token")
	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(httpServer.URL, "http://", "ws://", 1)+"/api/v1/platform/workloads/pods/api-0/terminal?namespace=platform", headers)
	if err == nil {
		_ = conn.Close()
		t.Fatal("terminal websocket dial succeeded, want forbidden")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", resp, http.StatusForbidden)
	}
	if logs.FilterMessage("agent action audit").Len() != 1 {
		t.Fatalf("audit log count = %d, want 1", logs.FilterMessage("agent action audit").Len())
	}
}

func TestPortForwardRegisterListAndDeleteWhenAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	echoPort, stopEcho := startTestPortForwardEchoServer(t)
	defer stopEcho()
	runtime := &fakeActivePortForward{port: echoPort, stopped: make(chan struct{})}
	router := gin.New()
	platform := router.Group("/api/v1/platform")
	platform.Use(authMiddleware("agent-token"))
	actions := newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{
		actionPlatformPortForwardsCreate,
		actionPlatformPortForwardsTunnel,
		actionPlatformPortForwardsDelete,
	}}, zap.NewNop(), nil)
	registry := newPortForwardRegistry(cfgpkg.KubernetesConfig{ID: "agent-cluster"}, func(_ context.Context, namespace, kind, name string, remotePort int) (activePortForward, error) {
		if namespace != "platform" || kind != "Pod" || name != "api-0" || remotePort != 8080 {
			t.Fatalf("unexpected runtime request namespace=%q kind=%q name=%q remotePort=%d", namespace, kind, name, remotePort)
		}
		return runtime, nil
	})
	registerPortForwardRoutes(platform, registry, actions, newWebSocketOriginPolicy(nil, "agent-token"))
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/platform/network/port-forwards", bytes.NewBufferString(`{"namespace":"platform","targetKind":"Pod","targetName":"api-0","localPort":18080,"remotePort":8080}`))
	createReq.Header.Set("Authorization", "Bearer agent-token")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createReq)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d body=%s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	var createBody struct {
		Data struct {
			SessionID  string `json:"sessionId"`
			ClusterID  string `json:"clusterId"`
			Namespace  string `json:"namespace"`
			TargetName string `json:"targetName"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createBody.Data.SessionID == "" || createBody.Data.ClusterID != "agent-cluster" || createBody.Data.Namespace != "platform" || createBody.Data.TargetName != "api-0" || createBody.Data.Status != "active" {
		t.Fatalf("unexpected create body: %#v", createBody.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/platform/network/port-forwards", nil)
	listReq.Header.Set("Authorization", "Bearer agent-token")
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	if !strings.Contains(listRecorder.Body.String(), createBody.Data.SessionID) {
		t.Fatalf("list body %s does not include session %s", listRecorder.Body.String(), createBody.Data.SessionID)
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer agent-token")
	untrustedHeaders := headers.Clone()
	untrustedHeaders.Set("Origin", "https://evil.example")
	untrustedConn, untrustedResp, untrustedErr := websocket.DefaultDialer.Dial(
		strings.Replace(httpServer.URL, "http://", "ws://", 1)+"/api/v1/platform/network/port-forwards/"+createBody.Data.SessionID+"/tunnel",
		untrustedHeaders,
	)
	if untrustedConn != nil {
		_ = untrustedConn.Close()
	}
	if untrustedResp != nil {
		defer func() {
			_ = untrustedResp.Body.Close()
		}()
	}
	if untrustedErr == nil {
		t.Fatal("untrusted origin Dial() succeeded, want rejection")
	}
	if untrustedResp == nil || untrustedResp.StatusCode != http.StatusForbidden {
		t.Fatalf("untrusted origin status = %v, want %d", responseStatus(untrustedResp), http.StatusForbidden)
	}

	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(httpServer.URL, "http://", "ws://", 1)+"/api/v1/platform/network/port-forwards/"+createBody.Data.SessionID+"/tunnel", headers)
	if err != nil {
		if resp != nil {
			t.Fatalf("tunnel dial status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatalf("tunnel dial: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("ping")); err != nil {
		t.Fatalf("write tunnel: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read tunnel: %v", err)
	}
	if string(payload) != "ping" {
		t.Fatalf("tunnel payload = %q, want echo", string(payload))
	}
	_ = conn.Close()

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/platform/network/port-forwards/"+createBody.Data.SessionID, nil)
	deleteReq.Header.Set("Authorization", "Bearer agent-token")
	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d body=%s", deleteRecorder.Code, http.StatusOK, deleteRecorder.Body.String())
	}
	select {
	case <-runtime.stopped:
	case <-time.After(time.Second):
		t.Fatal("port-forward runtime was not stopped")
	}
}

type fakeActivePortForward struct {
	port    int
	once    sync.Once
	stopped chan struct{}
}

func (f *fakeActivePortForward) LocalPort() int {
	return f.port
}

func (f *fakeActivePortForward) Stop() {
	f.once.Do(func() {
		close(f.stopped)
	})
}

func startTestPortForwardEchoServer(t *testing.T) (int, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo server: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	_, portString, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split echo addr: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse echo port: %v", err)
	}
	return port, func() {
		_ = listener.Close()
		<-done
	}
}

func TestRuntimeMetricsEndpointAvailableForRunner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime := runnerpkg.New(cfgpkg.ControlPlaneConfig{}, zap.NewNop())
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.NewNop(), nil, runtime)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/metrics", nil)
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		Data runnerpkg.MetricsSnapshot `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if body.Data.ActiveTasks != 0 {
		t.Fatalf("activeTasks = %d, want 0", body.Data.ActiveTasks)
	}
}

func TestDiagnosticsEndpointRequiresConfiguredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.NewNop(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil)
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestDiagnosticsEndpointReturnsSafeRuntimeSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime := runnerpkg.New(cfgpkg.ControlPlaneConfig{MaxConcurrency: 3}, zap.NewNop())
	server := New(cfgpkg.Config{
		App:  cfgpkg.AppConfig{Name: "soha-agent", Env: "development"},
		HTTP: cfgpkg.HTTPConfig{Addr: ":18080", BasePath: "/api/v1", AllowedOrigins: []string{"https://console.example"}},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
		Audit: cfgpkg.AuditConfig{
			FilePath: "/tmp/agent-actions.jsonl",
		},
		Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionRuntimeExecutionTaskCancel}},
		ControlPlane: cfgpkg.ControlPlaneConfig{
			Enabled:         true,
			BaseURL:         "https://control-plane.example",
			BearerToken:     "runner-token",
			AgentID:         "local-agent",
			RuntimeEndpoint: "https://agent.example",
			MaxConcurrency:  3,
			PollInterval:    5,
			DefaultTimeout:  30,
			CallbackRetry: cfgpkg.CallbackRetryConfig{
				MaxAttempts: 4,
				Backoff:     2,
			},
			ProviderKinds: []string{"ci_agent_runner"},
			WorkspaceRoot: "/tmp/workspaces",
			Docker: cfgpkg.DockerRunnerConfig{
				Enabled:        true,
				WorkerID:       "docker-worker",
				HostIDs:        []string{"host-a", "host-b"},
				OperationKinds: []string{"docker.container.start"},
				ComposeRoot:    "/tmp/compose",
				PollInterval:   7,
			},
			AgentRuntime: cfgpkg.AgentRuntimeConfig{
				Enabled:       true,
				WorkerID:      "hermes-worker",
				ProviderIDs:   []string{"hermes"},
				ProviderKinds: []string{"hermes"},
				HermesCommand: "hermes",
				WorkspaceRoot: "/tmp/agent-runtime",
				PollInterval:  9,
				Providers: map[string]cfgpkg.AgentProviderConfig{
					"hermes": {Command: "hermes"},
				},
			},
		},
		Kubernetes: cfgpkg.KubernetesConfig{
			Enabled:        true,
			ID:             "cluster-a",
			Name:           "Cluster A",
			Kubeconfig:     "secret-kubeconfig-path",
			KubeconfigData: "secret-kubeconfig-data",
			Context:        "context-a",
			Region:         "local",
			Environment:    "development",
			Labels:         map[string]string{"team": "platform"},
		},
	}, zap.NewNop(), nil, runtime)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil)
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	raw := recorder.Body.String()
	for _, secret := range []string{"agent-token", "runner-token", "secret-kubeconfig-path", "secret-kubeconfig-data"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("diagnostics response leaked %q: %s", secret, raw)
		}
	}

	var body struct {
		Data struct {
			App struct {
				Name string `json:"name"`
				Env  string `json:"env"`
			} `json:"app"`
			HTTP struct {
				BasePath            string `json:"basePath"`
				AllowedOriginsCount int    `json:"allowedOriginsCount"`
			} `json:"http"`
			Security struct {
				AllowedActionsCount int  `json:"allowedActionsCount"`
				AuditFileConfigured bool `json:"auditFileConfigured"`
			} `json:"security"`
			Kubernetes struct {
				Enabled              bool `json:"enabled"`
				ClientAvailable      bool `json:"clientAvailable"`
				KubeconfigConfigured bool `json:"kubeconfigConfigured"`
				KubeconfigDataLoaded bool `json:"kubeconfigDataLoaded"`
				LabelsCount          int  `json:"labelsCount"`
			} `json:"kubernetes"`
			Capabilities struct {
				Mode          string   `json:"mode"`
				Status        string   `json:"status"`
				RequiredKeys  []string `json:"requiredKeys"`
				AvailableKeys []string `json:"availableKeys"`
				DegradedKeys  []string `json:"degradedKeys"`
				Items         []struct {
					Key    string `json:"key"`
					Status string `json:"status"`
					Reason string `json:"reason"`
				} `json:"items"`
			} `json:"capabilities"`
			ControlPlane struct {
				Enabled           bool `json:"enabled"`
				BaseURLConfigured bool `json:"baseUrlConfigured"`
				MaxConcurrency    int  `json:"maxConcurrency"`
				Docker            struct {
					Enabled        bool     `json:"enabled"`
					HostCount      int      `json:"hostCount"`
					OperationKinds []string `json:"operationKinds"`
				} `json:"docker"`
				AgentRuntime struct {
					Enabled       bool `json:"enabled"`
					ProviderCount int  `json:"providerCount"`
				} `json:"agentRuntime"`
			} `json:"controlPlane"`
			Runtime struct {
				ControllerAvailable bool `json:"controllerAvailable"`
				ActiveTasks         int  `json:"activeTasks"`
				MetricsAvailable    bool `json:"metricsAvailable"`
			} `json:"runtime"`
			Metrics runnerpkg.MetricsSnapshot `json:"metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode diagnostics response: %v", err)
	}
	if body.Data.App.Name != "soha-agent" || body.Data.App.Env != "development" || body.Data.HTTP.BasePath != "/api/v1" {
		t.Fatalf("unexpected basic diagnostics: %#v", body.Data)
	}
	if body.Data.Security.AllowedActionsCount != 1 || !body.Data.Security.AuditFileConfigured {
		t.Fatalf("unexpected security diagnostics: %#v", body.Data.Security)
	}
	if !body.Data.Kubernetes.Enabled || body.Data.Kubernetes.ClientAvailable || !body.Data.Kubernetes.KubeconfigConfigured || !body.Data.Kubernetes.KubeconfigDataLoaded || body.Data.Kubernetes.LabelsCount != 1 {
		t.Fatalf("unexpected kubernetes diagnostics: %#v", body.Data.Kubernetes)
	}
	if body.Data.Capabilities.Mode != "agent" || body.Data.Capabilities.Status != "degraded" || len(body.Data.Capabilities.RequiredKeys) != 12 || len(body.Data.Capabilities.DegradedKeys) != 12 {
		t.Fatalf("unexpected capability diagnostics: %#v", body.Data.Capabilities)
	}
	if len(body.Data.Capabilities.Items) != 12 || body.Data.Capabilities.Items[0].Key != "cluster.inventory" || body.Data.Capabilities.Items[0].Status != "unsupported" || !strings.Contains(body.Data.Capabilities.Items[0].Reason, "kubernetes client") {
		t.Fatalf("unexpected capability item diagnostics: %#v", body.Data.Capabilities.Items)
	}
	if !body.Data.ControlPlane.Enabled || !body.Data.ControlPlane.BaseURLConfigured || body.Data.ControlPlane.MaxConcurrency != 3 {
		t.Fatalf("unexpected control plane diagnostics: %#v", body.Data.ControlPlane)
	}
	if !body.Data.ControlPlane.Docker.Enabled || body.Data.ControlPlane.Docker.HostCount != 2 || len(body.Data.ControlPlane.Docker.OperationKinds) != 1 {
		t.Fatalf("unexpected docker diagnostics: %#v", body.Data.ControlPlane.Docker)
	}
	if !body.Data.ControlPlane.AgentRuntime.Enabled || body.Data.ControlPlane.AgentRuntime.ProviderCount != 1 {
		t.Fatalf("unexpected agent runtime diagnostics: %#v", body.Data.ControlPlane.AgentRuntime)
	}
	if !body.Data.Runtime.ControllerAvailable || !body.Data.Runtime.MetricsAvailable || body.Data.Runtime.ActiveTasks != 0 || body.Data.Metrics.ActiveTasks != 0 {
		t.Fatalf("unexpected runtime diagnostics: %#v metrics=%#v", body.Data.Runtime, body.Data.Metrics)
	}
}

func TestBuildDiagnosticsCapabilitiesReflectsManagedAgentAllowlist(t *testing.T) {
	available := buildDiagnosticsCapabilitiesView(cfgpkg.Config{
		Kubernetes: cfgpkg.KubernetesConfig{Enabled: true},
		Security:   cfgpkg.SecurityConfig{AllowedActions: []string{"*"}},
	}, true)
	if available.Status != "available" || len(available.AvailableKeys) != len(managedAgentDiagnosticCapabilityKeys) || len(available.DegradedKeys) != 0 {
		t.Fatalf("expected all capabilities available, got %#v", available)
	}

	partial := buildDiagnosticsCapabilitiesView(cfgpkg.Config{
		Kubernetes: cfgpkg.KubernetesConfig{Enabled: true},
		Security: cfgpkg.SecurityConfig{AllowedActions: []string{
			actionPlatformPortForwardsCreate,
			actionPlatformPortForwardsTunnel,
			actionPlatformPodsExec,
		}},
	}, true)
	if partial.Status != "degraded" {
		t.Fatalf("expected degraded capability summary, got %#v", partial)
	}
	expected := map[string]string{
		"port.forward":       "partial",
		"pod.exec":           "available",
		"workload.mutations": "partial",
		"resource.creation":  "partial",
		"helm.releases":      "partial",
	}
	for _, item := range partial.Items {
		if want, ok := expected[item.Key]; ok && item.Status != want {
			t.Fatalf("capability %s status = %s, want %s in %#v", item.Key, item.Status, want, partial.Items)
		}
	}
}

func TestBuildInfoEndpointDoesNotRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldVersion, oldCommit, oldDate := buildinfo.Version, buildinfo.Commit, buildinfo.Date
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit, buildinfo.Date = oldVersion, oldCommit, oldDate
	})
	buildinfo.Version = "v0.1.0-test"
	buildinfo.Commit = "abc1234"
	buildinfo.Date = "2026-06-10T00:00:00Z"

	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.NewNop(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/build-info", nil)
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		Data buildinfo.Info `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode build info response: %v", err)
	}
	if body.Data.Version != "v0.1.0-test" || body.Data.Commit != "abc1234" || body.Data.Date != "2026-06-10T00:00:00Z" {
		t.Fatalf("unexpected build info: %#v", body.Data)
	}
	if body.Data.GOOS == "" || body.Data.GOARCH == "" || body.Data.GoVersion == "" {
		t.Fatalf("runtime build info missing: %#v", body.Data)
	}
}

func TestRuntimeCancelDeniedWritesAuditFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditPath := filepath.Join(t.TempDir(), "actions.jsonl")
	runtime := &fakeRuntimeController{}
	server := New(cfgpkg.Config{
		HTTP:  cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth:  cfgpkg.AuthConfig{BearerToken: "agent-token"},
		Audit: cfgpkg.AuditConfig{FilePath: auditPath},
	}, zap.NewNop(), nil, runtime)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/execution-tasks/task-1/cancel", bytes.NewBufferString(`{"reason":"test"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if runtime.cancelCalled {
		t.Fatal("CancelActiveTask() was called for denied action")
	}
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	var record actionAuditRecord
	if err := json.Unmarshal(bytes.TrimSpace(raw), &record); err != nil {
		t.Fatalf("decode audit record: %v raw=%q", err, raw)
	}
	if record.Action != actionRuntimeExecutionTaskCancel || record.Allowed || record.Reason == "" || record.Path != "/api/v1/runtime/execution-tasks/:taskID/cancel" {
		t.Fatalf("unexpected audit record: %#v", record)
	}
}

type fakeRuntimeController struct {
	cancelCalled bool
}

func (f *fakeRuntimeController) ListActiveTasks() []runnerpkg.ActiveTask {
	return nil
}

func (f *fakeRuntimeController) GetActiveTask(string) (runnerpkg.ActiveTask, bool) {
	return runnerpkg.ActiveTask{}, false
}

func (f *fakeRuntimeController) CancelActiveTask(taskID string, reason string) bool {
	f.cancelCalled = true
	return taskID == "task-1"
}

func TestProtectedNativeRouteRejectsArgoBeforeKubernetesWrite(t *testing.T) {
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformCustomResourcesCreate}}}, nil, &k8sagent.Client{}, nil)
	body := `{"definition":{"group":"argoproj.io","version":"v1alpha1","resource":"applications","kind":"Application","namespaced":true},"namespace":"test","content":"apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: app\n  namespace: test\n"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/ownership-v1/extensions/custom-resources", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "resource_ownership_conflict") {
		t.Fatalf("protected write = %d %s", response.Code, response.Body.String())
	}
}

func TestBasicResourceMutationRoutesDenyMissingActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "test-token"}}, zap.NewNop(), &k8sagent.Client{}, nil)
	for _, route := range []struct{ method, path string }{
		{"PUT", "/configuration/configmaps/demo/data?namespace=team"}, {"PUT", "/configuration/secrets/demo/data?namespace=team"},
		{"POST", "/namespaces"}, {"PUT", "/namespaces/team"}, {"DELETE", "/namespaces/team"},
		{"PUT", "/infrastructure/nodes/node"}, {"PUT", "/infrastructure/nodes/node/schedulability"}, {"POST", "/infrastructure/nodes/node/drain"},
		{"POST", "/workloads/cronjobs/job/suspend?namespace=team"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, "/api/v1/platform/ownership-v2"+route.path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
