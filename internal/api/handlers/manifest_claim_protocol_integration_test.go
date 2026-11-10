package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestManifestClaimProtocolOverHTTP(t *testing.T) {
	binary := manifestProtocolBinary(t)
	for _, tc := range []struct {
		name                        string
		manifest, helm, disabled    bool
		empty, unconfirmed, wrongID bool
		wantManifest, wantHelm      int
	}{
		{name: "ManifestOnly", manifest: true, wantManifest: 1},
		{name: "HelmOnly", helm: true, wantHelm: 1},
		{name: "Combined", manifest: true, helm: true, wantManifest: 1, wantHelm: 1},
		{name: "Disabled"},
		{name: "ControlPlaneDisabled", manifest: true, helm: true, disabled: true},
		{name: "Empty", manifest: true, helm: true, empty: true},
		{name: "WrongClusterIdentity", manifest: true, wrongID: true},
		{name: "UnconfirmedAdmission", manifest: true, unconfirmed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newProtocolQueue(tc.empty, tc.unconfirmed)
			handler := NewDeliveryHandlerWithServices(DeliveryServices{
				Runner: store, Agents: helmClaimAuthenticator{},
			}, legacyRunnerKeyring("runner-token"))
			router := gin.New()
			router.Use(store.observe)
			api := router.Group("/api/v1")
			api.POST("/delivery/execution-tasks/claim", handler.ClaimExecutionTask)
			api.POST("/delivery/execution-callbacks", handler.RecordExecutionCallback)
			api.GET("/delivery/execution-tasks/:taskID/runner-status", handler.GetExecutionTaskRunnerStatus)
			server := httptest.NewServer(router)
			defer server.Close()
			clusterID := "one"
			if tc.wrongID {
				clusterID = "two"
			}
			report := filepath.Join(t.TempDir(), "executions.json")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			// #nosec G204 -- the explicit opt-in is an absolute, executable test binary validated above.
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestManifestClaimProtocolPeer$", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), "SOHA_TEST_PROTOCOL_MODE=claim",
				"SOHA_TEST_CORE_URL="+server.URL, "SOHA_TEST_CLUSTER_ID="+clusterID, "SOHA_TEST_REPORT="+report,
				protocolFlag("SOHA_TEST_MANIFEST", tc.manifest), protocolFlag("SOHA_TEST_HELM", tc.helm),
				protocolFlag("SOHA_TEST_CONTROL_PLANE", !tc.disabled))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Agent protocol peer failed: %v\n%s", err, output)
			}
			// #nosec G304 -- this report uses the current test's t.TempDir and a fixed filename.
			raw, err := os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			var counts map[string]int
			if err := json.Unmarshal(raw, &counts); err != nil {
				t.Fatal(err)
			}
			if counts["manifest"] != tc.wantManifest || counts["helm"] != tc.wantHelm {
				t.Fatalf("executor calls = %v, want Manifest=%d Helm=%d", counts, tc.wantManifest, tc.wantHelm)
			}
			store.assertProtocol(t, tc.wantManifest, tc.wantHelm, tc.empty, tc.disabled || !tc.manifest && !tc.helm)
			store.assertRejections(t, tc.wrongID, tc.unconfirmed)
		})
	}
}

func manifestProtocolBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("SOHA_TEST_AGENT_BINARY")
	if binary == "" {
		t.Skip("set SOHA_TEST_AGENT_BINARY to the precompiled Agent runner test binary")
	}
	// #nosec G703 -- the local test driver supplies this binary path, validated as absolute, regular and executable below.
	info, err := os.Stat(binary)
	if err != nil || !filepath.IsAbs(binary) {
		t.Fatalf("Agent binary must be an existing absolute path: %q (%v)", binary, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("Agent binary is not an executable regular file: %q", binary)
	}
	return binary
}

func protocolFlag(key string, enabled bool) string {
	if enabled {
		return key + "=1"
	}
	return key + "=0"
}

type protocolQueue struct {
	mu                 sync.Mutex
	tasks              []dd.ExecutionTask
	callbacks          map[string][]string
	claimRequests      int
	admissionResponses int
	emptyResponses     int
	unauthorized       int
	unconfirmed        int
	rejectedCallbacks  int
}

func newProtocolQueue(empty, unconfirmed bool) *protocolQueue {
	q := &protocolQueue{callbacks: make(map[string][]string)}
	if empty {
		return q
	}
	q.tasks = []dd.ExecutionTask{
		{ID: "manifest", TaskKind: "manifest_preflight", Status: "queued",
			ProviderKind:  dd.ManifestAdmissionProviderPrefix + resourceruntime.ManifestAgentProviderPrefix + "one",
			CallbackToken: "manifest-attempt", ManifestAdmissionValidated: !unconfirmed,
			Payload: map[string]any{"action": "preflight", "clusterId": "one", "packageId": "pkg", "generation": 1, "idempotencyKey": "operation"}},
		{ID: "helm", TaskKind: "helm_observe", Status: "queued", ProviderKind: "helm_agent.one",
			CallbackToken: "helm-attempt", Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{
				Action: sohaapi.Observe, Snapshot: sohaapi.HelmDeliverySnapshot{
					ClusterID: "one", Namespace: "dev", ReleaseName: "app", DeliveryPlanID: "plan",
					ApplicationID: "app", ApplicationEnvironmentID: "env", ServiceID: "svc", TargetID: "target",
				},
			}}},
		{ID: "foreign", TaskKind: "manifest_preflight", Status: "queued",
			ProviderKind:  dd.ManifestAdmissionProviderPrefix + resourceruntime.ManifestAgentProviderPrefix + "two",
			CallbackToken: "foreign-attempt", ManifestAdmissionValidated: true},
	}
	return q
}

func (q *protocolQueue) observe(c *gin.Context) {
	c.Next()
	if c.Request.URL.Path != "/api/v1/delivery/execution-tasks/claim" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claimRequests++
	if c.Writer.Header().Get(dd.ManifestAdmissionHeader) == "1" {
		q.admissionResponses++
	}
	if c.Writer.Status() == http.StatusNoContent {
		q.emptyResponses++
	}
	if c.Writer.Status() == http.StatusUnauthorized {
		q.unauthorized++
	}
	if c.Writer.Status() == http.StatusNotImplemented {
		q.unconfirmed++
	}
}

func (q *protocolQueue) ClaimExecutionTask(_ context.Context, providers []string, _, _ string) (dd.ExecutionTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.tasks {
		if q.tasks[i].Status == "queued" && slices.Contains(providers, q.tasks[i].ProviderKind) {
			q.tasks[i].Status = "claimed"
			return q.tasks[i], nil
		}
	}
	return dd.ExecutionTask{}, apperrors.ErrNotFound
}

func (q *protocolQueue) RecordCallback(_ context.Context, input dd.ExecutionCallbackInput) (dd.ExecutionTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.tasks {
		if q.tasks[i].Status != "queued" && input.CallbackToken != "" && q.tasks[i].CallbackToken == input.CallbackToken {
			q.tasks[i].Status = input.Status
			q.callbacks[q.tasks[i].ID] = append(q.callbacks[q.tasks[i].ID], input.Status)
			return q.tasks[i], nil
		}
	}
	q.rejectedCallbacks++
	return dd.ExecutionTask{}, apperrors.ErrUnauthorized
}

func (q *protocolQueue) GetExecutionTaskForRunner(_ context.Context, id string) (dd.ExecutionTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, task := range q.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return dd.ExecutionTask{}, apperrors.ErrNotFound
}

func (q *protocolQueue) assertProtocol(t *testing.T, manifests, helms int, empty, disabled bool) {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, count := range map[string]int{"manifest": manifests, "helm": helms} {
		want := []string(nil)
		if count == 1 {
			want = []string{"running", "completed"}
		}
		if !slices.Equal(q.callbacks[id], want) {
			t.Errorf("%s callback statuses = %v, want %v", id, q.callbacks[id], want)
		}
	}
	if q.admissionResponses != manifests {
		t.Errorf("confirmed admission HTTP responses = %d, want %d", q.admissionResponses, manifests)
	}
	if manifests+helms > 0 && q.rejectedCallbacks != 1 {
		t.Errorf("rejected wrong-attempt callbacks = %d, want 1", q.rejectedCallbacks)
	}
	if disabled && q.claimRequests != 0 {
		t.Errorf("disabled runner made %d claims", q.claimRequests)
	}
	if !disabled && q.claimRequests == 0 {
		t.Error("enabled runner did not attempt an HTTP claim")
	}
	if empty && q.emptyResponses == 0 {
		t.Error("empty queue did not return an HTTP 204 response")
	}
	for _, task := range q.tasks {
		if task.ID == "foreign" && task.Status != "queued" {
			t.Error("foreign cluster task was claimed")
		}
	}
}

func (q *protocolQueue) assertRejections(t *testing.T, wrongIdentity, unconfirmed bool) {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	if wrongIdentity && q.unauthorized == 0 {
		t.Error("wrong cluster credential did not receive HTTP 401")
	}
	if unconfirmed && q.unconfirmed == 0 {
		t.Error("unconfirmed private admission did not receive HTTP 501")
	}
}
