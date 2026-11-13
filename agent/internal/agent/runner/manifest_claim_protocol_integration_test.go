package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	"github.com/opensoha/soha/contracts/apiresponse"
)

// This entry point is run by Core's explicitly enabled protocol acceptance tests.
func TestManifestClaimProtocolPeer(t *testing.T) {
	mode := os.Getenv("SOHA_TEST_PROTOCOL_MODE")
	if mode == "" {
		t.Skip("requires the Core protocol acceptance parent")
	}
	enabled := os.Getenv("SOHA_TEST_CONTROL_PLANE") == "1"
	r := New(cfgpkg.ControlPlaneConfig{
		Enabled: enabled, BaseURL: os.Getenv("SOHA_TEST_CORE_URL"),
		AgentID: "protocol-agent", BearerToken: "runner-token",
		CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1},
	}, nil)
	manifest, helm := &recordingManifestExecutor{}, &recordingHelmExecutor{}
	clusterID := os.Getenv("SOHA_TEST_CLUSTER_ID")
	if os.Getenv("SOHA_TEST_MANIFEST") == "1" {
		r.SetManifestExecutor(manifest, clusterID, "cluster-token")
	}
	if os.Getenv("SOHA_TEST_HELM") == "1" {
		r.SetHelmExecutor(helm, clusterID, "cluster-token", nil)
	}
	switch mode {
	case "claim":
		if enabled && r.executionClaimEnabled() {
			for range 3 {
				if task, ok := r.claim(t.Context()); ok {
					r.executeClaimedTask(t.Context(), task)
				}
			}
		}
		if manifest.calls+helm.calls > 0 {
			if _, err := r.apiClient().RecordExecutionCallback(t.Context(), callbackRequest{
				CallbackToken: "wrong-attempt", Status: "completed",
			}); err == nil {
				t.Fatal("Core accepted a callback with the wrong attempt token")
			}
		}
		report, err := json.Marshal(map[string]int{"manifest": manifest.calls, "helm": helm.calls})
		if err != nil {
			t.Fatal(err)
		}
		// #nosec G703 -- the explicit Core parent supplies its t.TempDir report path to this test-only child.
		if err := os.WriteFile(os.Getenv("SOHA_TEST_REPORT"), report, 0o600); err != nil {
			t.Fatal(err)
		}
	case "summary":
		serveProtocolSummary(t, r, clusterID)
	default:
		t.Fatalf("unsupported protocol mode %q", mode)
	}
}

func serveProtocolSummary(t *testing.T, r *Runner, clusterID string) {
	t.Helper()
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/version":
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)
		case "/api":
			_, _ = io.WriteString(w, `{"kind":"APIVersions","versions":["v1"]}`)
		case "/apis":
			_, _ = io.WriteString(w, `{"kind":"APIGroupList","groups":[]}`)
		default:
			http.NotFound(w, req)
		}
	}))
	defer discovery.Close()
	client, err := k8sagent.New(cfgpkg.KubernetesConfig{
		ID: clusterID,
		KubeconfigData: fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: protocol
  cluster:
    server: %s
contexts:
- name: protocol
  context:
    cluster: protocol
    user: protocol
current-context: protocol
users:
- name: protocol
  user: {}
`, discovery.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetExecutionClaimCapabilities(r.ExecutionClaimCapabilities())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/platform/summary" {
			http.NotFound(w, req)
			return
		}
		if req.Header.Get("Authorization") != "Bearer cluster-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(apiresponse.Item(client.Summary(req.Context()))); err != nil {
			t.Errorf("encode actual Agent summary: %v", err)
		}
	}))
	defer server.Close()
	ready := filepath.Clean(os.Getenv("SOHA_TEST_REPORT"))
	// #nosec G703 -- the Core parent supplies its t.TempDir endpoint path, with a fixed staging suffix.
	if err := os.WriteFile(ready+".pending", []byte(server.URL), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- both paths belong to the Core parent's t.TempDir, publishing the endpoint atomically.
	if err := os.Rename(ready+".pending", ready); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}
