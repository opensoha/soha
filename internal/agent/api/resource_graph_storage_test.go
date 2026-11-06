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
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func TestStorageGraphHTTPReadScopeAndAuth(t *testing.T) {
	for _, root := range []struct{ kind, path, namespace string }{
		{"PersistentVolumeClaim", "/api/v1/namespaces/team-a/persistentvolumeclaims/root", "team-a"},
		{"PersistentVolume", "/api/v1/persistentvolumes/root", ""},
		{"StorageClass", "/apis/storage.k8s.io/v1/storageclasses/root", ""},
	} {
		t.Run(root.kind, func(t *testing.T) {
			var reads atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				var obj any
				switch r.URL.Path {
				case root.path:
					spec := map[string]any{}
					if root.kind == "PersistentVolumeClaim" {
						spec = map[string]any{"volumeName": "volume-a", "storageClassName": "fast"}
					}
					obj = map[string]any{"apiVersion": map[bool]string{true: "storage.k8s.io/v1", false: "v1"}[root.kind == "StorageClass"], "kind": root.kind, "metadata": map[string]any{"name": "root", "namespace": root.namespace, "uid": "claim-uid"}, "spec": spec, "status": map[string]any{"phase": "Bound"}}
				case "/api/v1/persistentvolumes/volume-a":
					obj = map[string]any{"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]any{"name": "volume-a", "uid": "volume-uid"}, "status": map[string]any{"phase": "Bound"}, "spec": map[string]any{"storageClassName": "fast", "claimRef": map[string]any{"namespace": "team-a", "name": "root", "uid": "claim-uid"}}}
				case "/apis/storage.k8s.io/v1/storageclasses/fast":
					obj = map[string]any{"apiVersion": "storage.k8s.io/v1", "kind": "StorageClass", "metadata": map[string]any{"name": "fast"}}
				default:
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"private-provider-detail"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(obj)
			}))
			defer upstream.Close()
			router := newGraphPlatformRouter(t, upstream.URL)
			target := graphTestPath + "?" + url.Values{"kind": {root.kind}, "name": {"root"}, "namespace": {root.namespace}}.Encode()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			if response.Code != http.StatusUnauthorized || reads.Load() != 0 {
				t.Fatalf("unauthenticated request: %d, reads=%d", response.Code, reads.Load())
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Authorization", "Bearer test-token")
			response = httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-provider-detail") {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if root.kind == "PersistentVolumeClaim" {
				var envelope struct {
					Data contractresource.ResourceGraph `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				kinds := map[string]bool{}
				for _, node := range envelope.Data.Nodes {
					kinds[node.Resource.Kind] = true
				}
				relations := map[string]bool{}
				for _, edge := range envelope.Data.Edges {
					relations[edge.Relation] = true
					if edge.Relation == "binds-to" {
						if edge.ResolutionState != "resolved" {
							t.Fatalf("binding not resolved: %+v", edge)
						}
						var targetUID string
						for _, node := range envelope.Data.Nodes {
							if node.ID == edge.TargetID {
								targetUID = node.Resource.UID
							}
						}
						if targetUID != "volume-uid" {
							t.Fatalf("binding target UID=%q", targetUID)
						}
					}
				}
				if !kinds["PersistentVolumeClaim"] || !kinds["PersistentVolume"] || !kinds["StorageClass"] || !relations["binds-to"] || !relations["uses-storage-class"] {
					t.Fatalf("missing storage chain: %s", response.Body.String())
				}
			}
			before := reads.Load()
			invalidNamespace := "team-a"
			if root.namespace != "" {
				invalidNamespace = ""
			}
			invalid := graphTestPath + "?" + url.Values{"kind": {root.kind}, "name": {"root"}, "namespace": {invalidNamespace}}.Encode()
			req = httptest.NewRequest(http.MethodGet, invalid, nil)
			req.Header.Set("Authorization", "Bearer test-token")
			response = httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest || reads.Load() != before {
				t.Fatalf("invalid scope reached provider: response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestResourceGraphCapabilityHead(t *testing.T) {
	var reads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	router := newGraphPlatformRouter(t, upstream.URL)
	storage := contractresource.StorageResourceGraphFeature
	rbac := contractresource.RBACResourceGraphFeature
	for _, tc := range []struct {
		name, query string
		auth        bool
		status      int
	}{
		{"unauthorized storage", "capability=" + storage, false, http.StatusUnauthorized},
		{"unauthorized RBAC", "capability=" + rbac, false, http.StatusUnauthorized},
		{"supported storage", "capability=" + storage, true, http.StatusNoContent},
		{"supported RBAC", "capability=" + rbac, true, http.StatusNoContent},
		{"missing", "", true, http.StatusNotFound},
		{"unknown", "capability=unknown", true, http.StatusNotFound},
		{"different version", "capability=resource.graph.rbac.v2", true, http.StatusNotFound},
		{"different case", "capability=" + strings.ToUpper(rbac), true, http.StatusNotFound},
		{"trailing space", "capability=" + rbac + "%20", true, http.StatusNotFound},
		{"extra parameter", "capability=" + rbac + "&kind=Role", true, http.StatusNotFound},
		{"multiple values", "capability=" + storage + "&capability=" + rbac, true, http.StatusNotFound},
		{"duplicate value", "capability=" + rbac + "&capability=" + rbac, true, http.StatusNotFound},
		{"comma joined", "capability=" + storage + "," + rbac, true, http.StatusNotFound},
		{"malformed extra parameter", "capability=" + rbac + "&kind=%ZZ", true, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodHead, graphTestPath+"?"+tc.query, nil)
			if tc.auth {
				req.Header.Set("Authorization", "Bearer test-token")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.auth && response.Body.Len() != 0 {
				t.Fatalf("unexpected body: %s", response.Body.String())
			}
			if reads.Load() != 0 {
				t.Fatalf("capability probe reached provider %d times", reads.Load())
			}
		})
	}
}

const graphTestPath = "/api/v1/platform/resources/graph"

func newGraphPlatformRouter(t *testing.T, kubeURL string) *gin.Engine {
	t.Helper()
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
`, kubeURL)})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	cfg := cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}}
	cfg.Auth.BearerToken = "test-token"
	registerPlatformRoutes(router, cfg, client, actionPolicy{})
	return router
}
