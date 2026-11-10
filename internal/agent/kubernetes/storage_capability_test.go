package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-agent/internal/agent/runner"
	"github.com/opensoha/soha-contracts/helmrelease"
	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	discoveryfake "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestSummaryAdvertisesResourceGraphInHealthyAndDegradedStates(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		t.Run(fmt.Sprintf("degraded=%t", degraded), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if degraded && r.URL.Path == "/apis" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				switch r.URL.Path {
				case "/version":
					_, _ = w.Write([]byte(`{"gitVersion":"v1.34.0"}`))
				case "/api":
					_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
				case "/apis":
					_, _ = w.Write([]byte(`{"kind":"APIGroupList","groups":[]}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()
			client, err := New(cfgpkg.KubernetesConfig{ID: "cluster-a", KubeconfigData: fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, upstream.URL)})
			if err != nil {
				t.Fatal(err)
			}
			summary := client.Summary(context.Background())
			for _, feature := range []string{contractresource.StorageResourceGraphFeature, contractresource.RBACResourceGraphFeature} {
				if !slices.Contains(summary.Capabilities, feature) {
					t.Fatalf("missing capability %q: %#v", feature, summary)
				}
			}
			const manifestAdmission = "manifest.delivery-admission.v1"
			if slices.Contains(summary.Capabilities, manifestAdmission) || slices.Contains(summary.Capabilities, helmrelease.InitializationCapability) {
				t.Fatal("unconfigured runner task capabilities were advertised")
			}
			client.SetExecutionClaimCapabilities([]string{manifestAdmission})
			withManifest := client.Summary(context.Background())
			if !slices.Contains(withManifest.Capabilities, contractruntime.ManifestAgentCapability) && !degraded {
				t.Fatal("direct Manifest API capability missing")
			}
			if slices.Contains(withManifest.Capabilities, manifestAdmission) == degraded || slices.Contains(withManifest.Capabilities, helmrelease.InitializationCapability) {
				t.Fatalf("Manifest-only task capabilities = %v", withManifest.Capabilities)
			}
			want := "healthy"
			if degraded {
				want = "degraded"
			}
			if summary.Health.Status != want {
				t.Fatalf("health=%q, want %q", summary.Health.Status, want)
			}
		})
	}
}

func TestSummaryAdvertisesConfiguredManifestClaimReadiness(t *testing.T) {
	const manifestAdmission = "manifest.delivery-admission.v1"
	for _, tc := range []struct {
		name                         string
		controlPlane, manifest, helm bool
		wantManifest, wantHelm       bool
	}{
		{name: "Disabled", controlPlane: true},
		{name: "ControlPlaneDisabled", manifest: true, helm: true},
		{name: "ManifestOnly", controlPlane: true, manifest: true, wantManifest: true},
		{name: "HelmOnly", controlPlane: true, helm: true, wantHelm: true},
		{name: "Combined", controlPlane: true, manifest: true, helm: true, wantManifest: true, wantHelm: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const clusterID, clusterToken = "cluster-a", "cluster-token"
			client := &Client{
				cfg:       cfgpkg.KubernetesConfig{ID: clusterID},
				discovery: &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}},
			}
			executor := runner.New(cfgpkg.ControlPlaneConfig{
				Enabled: tc.controlPlane, BaseURL: "http://core", BearerToken: "runner-token",
			}, nil)
			if tc.manifest {
				executor.SetManifestExecutor(client, clusterID, clusterToken)
			}
			if tc.helm {
				executor.SetHelmExecutor(client, clusterID, clusterToken, nil)
			}
			client.SetExecutionClaimCapabilities(executor.ExecutionClaimCapabilities())

			summary := client.Summary(t.Context())
			if summary.Health.Status != "healthy" || !slices.Contains(summary.Capabilities, contractruntime.ManifestAgentCapability) {
				t.Fatalf("direct rollout capability unavailable: %+v", summary)
			}
			for _, capability := range []string{contractruntime.RootCRAgentCapability, contractruntime.ManifestInitializationCapability, manifestAdmission} {
				if slices.Contains(summary.Capabilities, capability) != tc.wantManifest {
					t.Fatalf("capability %q: want advertised=%t, got %v", capability, tc.wantManifest, summary.Capabilities)
				}
			}
			if slices.Contains(summary.Capabilities, helmrelease.InitializationCapability) != tc.wantHelm {
				t.Fatalf("Helm initialization: want advertised=%t, got %v", tc.wantHelm, summary.Capabilities)
			}
		})
	}
}
