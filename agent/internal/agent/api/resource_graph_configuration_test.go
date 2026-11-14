package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
)

func TestConfigurationGraphHTTPReadWithoutMutationAllowlist(t *testing.T) {
	for _, root := range []struct{ api, kind, path, namespace string }{
		{"v1", "ConfigMap", "/api/v1/namespaces/team-a/configmaps/root", "team-a"},
		{"v1", "Secret", "/api/v1/namespaces/team-a/secrets/root", "team-a"},
		{"v1", "ResourceQuota", "/api/v1/namespaces/team-a/resourcequotas/root", "team-a"},
		{"v1", "LimitRange", "/api/v1/namespaces/team-a/limitranges/root", "team-a"},
		{"autoscaling/v2", "HorizontalPodAutoscaler", "/apis/autoscaling/v2/namespaces/team-a/horizontalpodautoscalers/root", "team-a"},
		{"policy/v1", "PodDisruptionBudget", "/apis/policy/v1/namespaces/team-a/poddisruptionbudgets/root", "team-a"},
		{"scheduling.k8s.io/v1", "PriorityClass", "/apis/scheduling.k8s.io/v1/priorityclasses/root", ""},
		{"node.k8s.io/v1", "RuntimeClass", "/apis/node.k8s.io/v1/runtimeclasses/root", ""},
		{"coordination.k8s.io/v1", "Lease", "/apis/coordination.k8s.io/v1/namespaces/team-a/leases/root", "team-a"},
		{"admissionregistration.k8s.io/v1", "MutatingWebhookConfiguration", "/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations/root", ""},
		{"admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations/root", ""},
	} {
		t.Run(root.kind, func(t *testing.T) {
			var reads atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				if r.URL.Path != root.path {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"private-provider-detail"}`))
					return
				}
				api, kind := root.api, root.kind
				if kind == "Secret" {
					if r.Header.Get("Accept") != "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1" {
						t.Error("Secret root requested payload")
					}
					api, kind = "meta.k8s.io/v1", "PartialObjectMetadata"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": api, "kind": kind, "metadata": map[string]any{"name": "root", "namespace": root.namespace, "uid": "root-uid"}})
			}))
			defer upstream.Close()
			client, err := k8sagent.New(cfgpkg.KubernetesConfig{ID: "cluster-a", KubeconfigData: fmt.Sprintf(`apiVersion: v1
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
			router := gin.New()
			registerResourceYAMLRoutes(router.Group("/platform", authMiddleware("test-token")), client, actionPolicy{})
			target := "/platform/resources/graph?" + url.Values{"kind": {root.kind}, "name": {"root"}, "namespace": {root.namespace}}.Encode()
			for _, authorized := range []bool{false, true} {
				request := httptest.NewRequest(http.MethodGet, target, nil)
				if authorized {
					request.Header.Set("Authorization", "Bearer test-token")
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := http.StatusUnauthorized
				if authorized {
					want = http.StatusOK
				}
				if response.Code != want || strings.Contains(response.Body.String(), "private-provider-detail") {
					t.Fatalf("response=%d %s", response.Code, response.Body.String())
				}
				if !authorized && reads.Load() != 0 {
					t.Fatal("unauthenticated graph reached provider")
				}
			}
			before := reads.Load()
			invalidNamespace := ""
			if root.namespace == "" {
				invalidNamespace = "team-a"
			}
			invalid := "/platform/resources/graph?" + url.Values{"kind": {root.kind}, "name": {"root"}, "namespace": {invalidNamespace}}.Encode()
			request := httptest.NewRequest(http.MethodGet, invalid, nil)
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || reads.Load() != before {
				t.Fatalf("invalid scope reached provider: response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}
