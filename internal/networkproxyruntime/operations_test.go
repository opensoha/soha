package networkproxyruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestDeploymentGrantFencesBootEpochAndIntent(t *testing.T) {
	now := time.Now().UTC()
	r := &Runner{config: Config{RuntimeID: "runtime", InstanceID: "pod-0", ManagedDeployment: true}, hostBootEpoch: "boot-1"}
	g := api.NetworkProxyDeploymentRuntimeControl{RuntimeID: "runtime", InstanceID: "pod-0", ProcessEpoch: "boot-1", CredentialID: "credential", CredentialEpoch: 1, OwnerEpoch: 1, ControlRevision: 3, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), AcceptingRequests: true}
	if err := r.acceptDeployment(g, now); err != nil || !r.deployment.mayApply(now) {
		t.Fatal("valid serve grant rejected", err)
	}
	for _, mutate := range []func(*api.NetworkProxyDeploymentRuntimeControl){func(g *api.NetworkProxyDeploymentRuntimeControl) { g.ProcessEpoch = "engine-epoch" }, func(g *api.NetworkProxyDeploymentRuntimeControl) { g.ControlRevision = 2 }, func(g *api.NetworkProxyDeploymentRuntimeControl) { g.AcceptingRequests = false }, func(g *api.NetworkProxyDeploymentRuntimeControl) { g.ExpiresAt = now.Add(-time.Second) }} {
		bad := g
		mutate(&bad)
		if r.acceptDeployment(bad, now) == nil {
			t.Fatal("invalid control accepted")
		}
	}
	g.ControlRevision++
	g.AcceptingRequests = false
	if err := r.acceptDeployment(g, now); err != nil || r.deployment.mayApply(now) {
		t.Fatal("drain admitted traffic", err)
	}
	g.ControlRevision++
	g.AcceptingRequests = true
	if err := r.acceptDeployment(g, now); err != nil || !r.deployment.mayApply(now) {
		t.Fatal("new serve grant rejected", err)
	}
	if r.deployment.mayApply(g.ExpiresAt) {
		t.Fatal("expired grant admitted traffic")
	}
}

func TestTypedSelectReadbackAndReceiptRetry(t *testing.T) {
	var writes atomic.Int32
	selected := "node-a"
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/group" {
			t.Errorf("unexpected controller path %s", r.URL.Path)
		}
		if r.Method == http.MethodPut {
			writes.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			selected = body["name"]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"now": selected, "all": []string{"node-a", "node-b"}})
	}))
	defer controller.Close()
	command := api.NetworkProxyRuntimeCommandInput{RequestID: "request-1", ProcessEpoch: "engine-1", Kind: "select", GroupID: "group", NodeID: "node-b", IssuedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(20 * time.Second)}
	var posts atomic.Int32
	client := NewClient("", "runtime", nil)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("processEpoch") != "engine-1" {
				t.Error("missing engine epoch")
			}
			_ = json.NewEncoder(w).Encode(client.message("command.request", command))
			return
		}
		if posts.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer control.Close()
	client.origin = control.URL
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	client.SetSchemas(schemas)
	e := newEngine(Config{Engine: domain.EngineMihomo, ControllerURL: controller.URL}, "controller-secret")
	e.process = &exec.Cmd{}
	e.processEpoch = "engine-1"
	e.desired.Publication = &api.NetworkProxyServerPublication{Groups: []api.NetworkProxyGroup{{GroupID: "group", Label: "safe", Type: "select", NodeIDs: []string{"node-a", "node-b"}}}}
	r := &Runner{enabled: true, engine: e, client: client, ingress: &publicationIngress{publication: *e.desired.Publication}}
	if err := r.executeCommand(context.Background()); err == nil {
		t.Fatal("lost receipt not reported")
	}
	if err := r.executeCommand(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 || writes.Load() != 1 || selected != "node-b" {
		t.Fatalf("mutation replayed %d times", writes.Load())
	}
	command.NodeID = "unapproved"
	if err := r.executeCommand(context.Background()); err == nil {
		t.Fatal("request identity reused with different intent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, status, _ := r.runCommand(ctx, command)
	if status != "cancelled" {
		t.Fatal("cancelled operation executed")
	}
}

func TestFailedReceiptRetryDoesNotBlockNextCommand(t *testing.T) {
	client := NewClient("", "runtime", nil)
	var claims, rejectedPosts atomic.Int32
	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			id := "new"
			if claims.Add(1) == 1 {
				id = "old"
			}
			command := api.NetworkProxyRuntimeCommandInput{RequestID: id, ProcessEpoch: "epoch", Kind: "logs", IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(20 * time.Second)}
			_ = json.NewEncoder(w).Encode(client.message("command.request", command))
			return
		}
		if strings.HasSuffix(req.URL.Path, "/old/result") {
			if rejectedPosts.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusConflict)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client.origin = server.URL
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	client.SetSchemas(schemas)
	r := &Runner{enabled: true, client: client, engine: &engine{process: &exec.Cmd{}, processEpoch: "epoch"}}
	if err := r.executeCommand(context.Background()); err == nil {
		t.Fatal("initial receipt transport failure was hidden")
	}
	for i := 0; i < 2; i++ {
		if err := r.executeCommand(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if claims.Load() != 3 || rejectedPosts.Load() != 2 || !r.commandReceipts["old"].settled || !r.commandReceipts["new"].settled {
		t.Fatal("a rejected receipt blocked claims or remained in active retry")
	}
}

func TestCloseRejectsPreviousEngineEpoch(t *testing.T) {
	var closed atomic.Int32
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { closed.Add(1); w.WriteHeader(204) }))
	defer controller.Close()
	client := NewClient("", "runtime", nil)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(client.message("connection.close.request", map[string]any{"commandId": "close-1", "connectionId": "same-id", "processEpoch": "old-engine", "expiresAt": time.Now().Add(20 * time.Second)}))
			return
		}
		w.WriteHeader(204)
	}))
	defer control.Close()
	client.origin = control.URL
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	client.SetSchemas(schemas)
	e := newEngine(Config{Engine: domain.EngineMihomo, ControllerURL: controller.URL}, "secret")
	e.processEpoch = "new-engine"
	r := &Runner{engine: e, enabled: true, client: client}
	if err := r.executeClose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 0 {
		t.Fatal("old command closed a new process connection")
	}
}

func TestDeploymentACKUsesHostNonceAndUnknownCount(t *testing.T) {
	var received api.NetworkProxyDeploymentRuntimeAck
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/network-control/v1/proxy-runtimes/runtime/deployment/control/receipt" {
			t.Errorf("wrong ACK path %s", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(received)
	}))
	defer server.Close()
	g := &api.NetworkProxyDeploymentRuntimeControl{RuntimeID: "runtime", InstanceID: "pod-0", ProcessEpoch: "host-epoch", CredentialID: "credential", CredentialEpoch: 2, OwnerEpoch: 3, ControlRevision: 4, ExpiresAt: time.Now().Add(time.Minute), AcceptingRequests: true}
	r := &Runner{config: Config{RuntimeID: "runtime", ManagedDeployment: true}, hostBootEpoch: "host-epoch", client: NewClient(server.URL, "runtime", nil), deployment: deploymentState{grant: g}}
	if err := r.ackDeployment(context.Background(), domain.Observation{ProcessEpoch: "engine-epoch", Health: "unhealthy"}); err != nil {
		t.Fatal(err)
	}
	if received.ActiveCountKnown || received.AcceptingRequests || received.ProcessEpoch != "host-epoch" || received.ControlRevision != 4 || received.OwnerEpoch != 3 || !received.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatalf("incorrect ACK %+v", received)
	}
}
