package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDirectMissingConfigMapDetailIsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/demo/configmaps/missing" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404}`))
	}))
	defer server.Close()
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	manager := k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})
	direct := NewDirect(NewClusters(manager), nil)
	_, err := direct.GetConfigMapDetail(context.Background(), "test", "demo", "missing")
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing ConfigMap must map to application not found: %v", err)
	}
}

func TestDirectPodYAMLIncludesManifestIdentityAfterTypedDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/demo/pods/sample" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"sample","namespace":"demo"}}`))
	}))
	defer server.Close()
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	manager := k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})
	direct := NewDirect(NewClusters(manager), nil)
	view, err := direct.GetPodYAML(context.Background(), "test", "demo", "sample")
	if err != nil || !strings.Contains(view.Content, "apiVersion: v1\n") || !strings.Contains(view.Content, "kind: Pod\n") {
		t.Fatalf("Pod YAML cannot be reapplied: %+v %v", view, err)
	}
}

func TestDirectManifestCreationUsesTheYAMLEditManager(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/namespaces/demo/configmaps" || r.URL.Query().Get("fieldManager") != resourceEditFieldManager {
			t.Errorf("unexpected create request: %s %s", r.Method, r.URL)
		}
		if calls == 0 && r.URL.Query().Get("dryRun") != "All" {
			t.Error("preflight must remain a server dry-run")
		}
		if calls == 1 && r.URL.Query().Get("dryRun") != "" {
			t.Error("persistent create still dry-run")
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"sample","namespace":"demo"}}`))
	}))
	defer server.Close()
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	manager := k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})
	direct := NewDirect(NewClusters(manager), nil)
	manifest := domainresource.ResolvedCreateManifest{Ref: domainresource.ResourceCreateRef{Kind: "ConfigMap", Namespaced: true}, Version: "v1", Resource: "configmaps", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample\n  namespace: demo\n"}
	if err := direct.DryRunCreateManifest(context.Background(), "test", manifest, "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.CreateResolvedManifest(context.Background(), "test", manifest, "demo"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("create calls=%d", calls)
	}
}
