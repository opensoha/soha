package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestClientResourceYAMLMethodsUseAgentPlatformEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(resourceYAMLTestHandler(t, &seen))
	defer server.Close()

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
			"token":    "agent-token",
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	if _, err := client.GetResourceYAML(context.Background(), "platform", "ConfigMap", "app-config"); err != nil {
		t.Fatalf("GetResourceYAML() error = %v", err)
	}
	if _, err := client.ApplyResourceYAML(context.Background(), "platform", "ConfigMap", "app-config", "apiVersion: v1\nkind: ConfigMap\n"); err != nil {
		t.Fatalf("ApplyResourceYAML() error = %v", err)
	}
	if err := client.DeleteResource(context.Background(), "platform", "ConfigMap", "app-config"); err != nil {
		t.Fatalf("DeleteResource() error = %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("request count = %d, want 3: %#v", len(seen), seen)
	}
}

func TestClientStreamPodLogsCopiesAgentStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-token" {
			t.Fatalf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
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

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
			"token":    "agent-token",
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	var out bytes.Buffer
	if err := client.StreamPodLogs(context.Background(), "platform", "api-0", "app", 10, 5, &out); err != nil {
		t.Fatalf("StreamPodLogs() error = %v", err)
	}
	if out.String() != "line 1\nline 2\n" {
		t.Fatalf("output = %q, want streamed logs", out.String())
	}
}

func TestClientStreamPodTerminalBridgesWebSocketMessages(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(terminalTestHandler(t, upgrader))
	defer server.Close()

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
			"token":    "agent-token",
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = client.StreamPodTerminal(context.Background(), "platform", "api-0", "app", "/bin/sh", strings.NewReader("whoami\n"), &stdout, &stderr, &oneShotTerminalSizeQueue{})
	if err != nil {
		t.Fatalf("StreamPodTerminal() error = %v", err)
	}
	if stdout.String() != "root\n" || stderr.String() != "warn\n" {
		t.Fatalf("stdout=%q stderr=%q, want bridged terminal output", stdout.String(), stderr.String())
	}
}

type oneShotTerminalSizeQueue struct {
	sent bool
}

func resourceYAMLTestHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-token" {
			t.Fatalf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		*seen = append(*seen, r.Method+" "+r.URL.String())
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/platform/resources/yaml":
			handleResourceYAMLGet(t, w, r)
		case "PUT /api/v1/platform/resources/yaml":
			handleResourceYAMLApply(t, w, r)
		case "DELETE /api/v1/platform/resources":
			handleResourceDelete(t, w, r)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}
}

func handleResourceYAMLGet(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("namespace") != "platform" || r.URL.Query().Get("kind") != "ConfigMap" || r.URL.Query().Get("name") != "app-config" {
		t.Fatalf("unexpected get query: %s", r.URL.RawQuery)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"kind": "ConfigMap", "name": "app-config", "namespace": "platform", "content": "apiVersion: v1\nkind: ConfigMap\n"}})
}

func handleResourceYAMLApply(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var req resourceYAMLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Namespace != "platform" || req.Kind != "ConfigMap" || req.Name != "app-config" || req.Content == "" {
		t.Fatalf("unexpected apply request: %#v error=%v", req, err)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"kind": req.Kind, "name": req.Name, "namespace": req.Namespace, "content": req.Content}})
}

func handleResourceDelete(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var req deleteResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Namespace != "platform" || req.Kind != "ConfigMap" || req.Name != "app-config" {
		t.Fatalf("unexpected delete request: %#v error=%v", req, err)
	}
	w.WriteHeader(http.StatusOK)
}

func terminalTestHandler(t *testing.T, upgrader websocket.Upgrader) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-token" || r.URL.Path != "/api/v1/platform/workloads/pods/api-0/terminal" {
			t.Fatalf("unexpected terminal request %s authorization=%q", r.URL.String(), r.Header.Get("Authorization"))
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
		readTerminalClientMessages(t, conn)
		_ = conn.WriteJSON(terminalMessage{Type: "stdout", Data: "root\n"})
		_ = conn.WriteJSON(terminalMessage{Type: "stderr", Data: "warn\n"})
		_ = conn.WriteJSON(terminalMessage{Type: "exit", Message: "terminal session closed"})
	}
}

func readTerminalClientMessages(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	gotInput, gotResize := false, false
	for !gotInput || !gotResize {
		var message terminalMessage
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatalf("read terminal message: %v", err)
		}
		switch message.Type {
		case "input":
			gotInput = message.Data == "whoami\n" || gotInput
		case "resize":
			gotResize = message.Cols == 120 && message.Rows == 40 || gotResize
		case "close":
		default:
			t.Fatalf("unexpected terminal message: %#v", message)
		}
	}
}

func portForwardTestHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.String())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platform/network/port-forwards":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"sessionId": "session-1", "clusterId": "agent-cluster", "namespace": "platform", "targetKind": "Pod", "targetName": "api-0", "localPort": 18080, "remotePort": 8080, "status": "registered", "createdAt": "2026-06-12T00:00:00Z"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/network/port-forwards":
			var req domainresource.PortForwardRegisterInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode register request: %v", err)
			}
			if req.Namespace != "platform" || req.TargetName != "api-0" || req.LocalPort != 18080 || req.RemotePort != 8080 {
				t.Fatalf("unexpected register request: %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"sessionId": "session-2", "clusterId": "agent-cluster", "namespace": req.Namespace, "targetKind": req.TargetKind, "targetName": req.TargetName, "localPort": req.LocalPort, "remotePort": req.RemotePort, "status": "registered", "createdAt": "2026-06-12T00:00:00Z"}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/platform/network/port-forwards/session-2":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}
}

func helmMutationTestHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.String())
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/helm/charts/install":
			var req domainresource.HelmChartInstallInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode install request: %v", err)
			}
			if req.ReleaseName != "edge" || req.Namespace != "platform" || req.ChartName != "nginx" {
				t.Fatalf("unexpected install request: %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"name": "edge", "namespace": "platform", "revision": "1", "status": "deployed"}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/platform/helm/releases/edge/values":
			var req helmReleaseValuesRequest
			if r.URL.Query().Get("namespace") != "platform" || json.NewDecoder(r.Body).Decode(&req) != nil || req.Content != "replicaCount: 2\n" {
				t.Fatalf("unexpected values request: query=%s body=%#v", r.URL.RawQuery, req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"name": "edge", "namespace": "platform", "revision": "2", "content": req.Content, "editable": true, "diffEnabled": true}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/platform/helm/releases/edge":
			if r.URL.Query().Get("namespace") != "platform" {
				t.Fatalf("unexpected delete query: %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}
}

func customResourceTestHandler(t *testing.T, seen *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/platform/extensions/custom-resources/list":
			handleCustomResourceList(t, w, r)
		case "POST /api/v1/platform/extensions/custom-resources":
			handleCustomResourceCreate(t, w, r)
		case "POST /api/v1/platform/extensions/custom-resources/yaml":
			handleCustomResourceYAML(t, w, r, false)
		case "PUT /api/v1/platform/extensions/custom-resources/yaml":
			handleCustomResourceYAML(t, w, r, true)
		case "DELETE /api/v1/platform/extensions/custom-resources":
			handleCustomResourceDelete(t, w, r)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}
}

func handleCustomResourceList(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var req customResourceListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Definition.Kind != "Widget" || req.Namespace != "platform" {
		t.Fatalf("unexpected list request: %#v error=%v", req, err)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"apiVersion": "example.com/v1", "kind": "Widget", "name": "sample", "namespace": "platform"}}})
}

func handleCustomResourceCreate(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var req customResourceYAMLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Definition.Resource != "widgets" || req.Content == "" {
		t.Fatalf("unexpected create request: %#v error=%v", req, err)
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"kind": "Widget", "name": "created", "namespace": "platform", "content": req.Content}})
}

func handleCustomResourceYAML(t *testing.T, w http.ResponseWriter, r *http.Request, apply bool) {
	var req customResourceYAMLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name != "sample" || req.Namespace != "platform" || apply && req.Content == "" {
		t.Fatalf("unexpected yaml request: %#v error=%v", req, err)
	}
	content := "kind: Widget\n"
	if apply {
		content = req.Content
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"kind": "Widget", "name": "sample", "namespace": "platform", "content": content}})
}

func handleCustomResourceDelete(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var req customResourceYAMLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name != "sample" || req.Definition.Kind != "Widget" {
		t.Fatalf("unexpected delete request: %#v error=%v", req, err)
	}
	w.WriteHeader(http.StatusOK)
}

func (q *oneShotTerminalSizeQueue) Next() *domainresource.TerminalSize {
	if q.sent {
		return nil
	}
	q.sent = true
	return &domainresource.TerminalSize{Width: 120, Height: 40}
}

func TestClientPortForwardMethodsUseAgentPlatformEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(portForwardTestHandler(t, &seen))
	defer server.Close()

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	items, err := client.ListPortForwards(context.Background())
	if err != nil {
		t.Fatalf("ListPortForwards() error = %v", err)
	}
	if len(items) != 1 || items[0].SessionID != "session-1" {
		t.Fatalf("items = %#v, want session-1", items)
	}
	created, err := client.RegisterPortForward(context.Background(), domainresource.PortForwardRegisterInput{
		Namespace:  "platform",
		TargetKind: "Pod",
		TargetName: "api-0",
		LocalPort:  18080,
		RemotePort: 8080,
	})
	if err != nil {
		t.Fatalf("RegisterPortForward() error = %v", err)
	}
	if created.SessionID != "session-2" || created.Status != "registered" {
		t.Fatalf("created = %#v, want registered session-2", created)
	}
	if err := client.StopPortForward(context.Background(), "session-2"); err != nil {
		t.Fatalf("StopPortForward() error = %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("request count = %d, want 3: %#v", len(seen), seen)
	}
}

func TestClientStreamPortForwardBridgesWebSocketBytes(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-token" {
			t.Fatalf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/platform/network/port-forwards/session-2/tunnel" {
			t.Fatalf("path = %s, want port-forward tunnel path", r.URL.Path)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer func() { _ = conn.Close() }()
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read tunnel message: %v", err)
		}
		if messageType != websocket.BinaryMessage || string(payload) != "ping" {
			t.Fatalf("message type=%d payload=%q, want binary ping", messageType, string(payload))
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte("pong")); err != nil {
			t.Fatalf("write tunnel message: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
			"token":    "agent-token",
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	local, peer := net.Pipe()
	defer func() { _ = local.Close() }()
	defer func() { _ = peer.Close() }()
	if err := peer.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set pipe deadline: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.StreamPortForward(context.Background(), "session-2", local)
	}()
	if _, err := peer.Write([]byte("ping")); err != nil {
		t.Fatalf("write local pipe: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(peer, buf); err != nil {
		t.Fatalf("read local pipe: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("pipe payload = %q, want pong", string(buf))
	}
	if err := <-errCh; err != nil {
		t.Fatalf("StreamPortForward() error = %v", err)
	}
}

func TestClientCustomResourceMethodsUseAgentPlatformEndpoints(t *testing.T) {
	definition := domainresource.CRDResourceDefinition{
		CRDName:    "widgets.example.com",
		Group:      "example.com",
		Version:    "v1",
		Resource:   "widgets",
		Kind:       "Widget",
		Namespaced: true,
	}
	var seen []string
	server := httptest.NewServer(customResourceTestHandler(t, &seen))
	defer server.Close()

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	if _, err := client.ListCustomResources(context.Background(), definition, "platform"); err != nil {
		t.Fatalf("ListCustomResources() error = %v", err)
	}
	if _, err := client.CreateCustomResourceYAML(context.Background(), definition, "platform", "kind: Widget\nmetadata:\n  name: created\n"); err != nil {
		t.Fatalf("CreateCustomResourceYAML() error = %v", err)
	}
	if _, err := client.GetCustomResourceYAML(context.Background(), definition, "platform", "sample"); err != nil {
		t.Fatalf("GetCustomResourceYAML() error = %v", err)
	}
	if _, err := client.ApplyCustomResourceYAML(context.Background(), definition, "platform", "sample", "kind: Widget\nmetadata:\n  name: sample\n"); err != nil {
		t.Fatalf("ApplyCustomResourceYAML() error = %v", err)
	}
	if err := client.DeleteCustomResource(context.Background(), definition, "platform", "sample"); err != nil {
		t.Fatalf("DeleteCustomResource() error = %v", err)
	}
	if len(seen) != 5 {
		t.Fatalf("request count = %d, want 5: %#v", len(seen), seen)
	}
}

func TestClientHelmMutationMethodsUseAgentPlatformEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(helmMutationTestHandler(t, &seen))
	defer server.Close()

	client, err := NewRegistry(time.Second).ClientFor(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{
			"endpoint": server.URL,
		},
	})
	if err != nil {
		t.Fatalf("ClientFor() error = %v", err)
	}

	installed, err := client.InstallHelmChart(context.Background(), domainresource.HelmChartInstallInput{
		RepositoryURL: "https://charts.example",
		ChartName:     "nginx",
		Version:       "1.2.3",
		ReleaseName:   "edge",
		Namespace:     "platform",
	})
	if err != nil {
		t.Fatalf("InstallHelmChart() error = %v", err)
	}
	if installed.Name != "edge" || installed.Revision != "1" {
		t.Fatalf("installed = %#v, want edge revision 1", installed)
	}
	values, err := client.UpdateHelmReleaseValues(context.Background(), "platform", "edge", "replicaCount: 2\n")
	if err != nil {
		t.Fatalf("UpdateHelmReleaseValues() error = %v", err)
	}
	if values.Revision != "2" || !values.Editable {
		t.Fatalf("values = %#v, want revision 2 editable", values)
	}
	if err := client.DeleteHelmRelease(context.Background(), "platform", "edge"); err != nil {
		t.Fatalf("DeleteHelmRelease() error = %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("request count = %d, want 3: %#v", len(seen), seen)
	}
}
