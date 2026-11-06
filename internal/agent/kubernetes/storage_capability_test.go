package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	contractresource "github.com/opensoha/soha-contracts/resource"
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
