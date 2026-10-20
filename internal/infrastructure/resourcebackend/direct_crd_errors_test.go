package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDirectMissingCRDReadsAreNotFound(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		read func(*Direct) error
	}{
		{"definition", "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/missing.example.io", func(d *Direct) error {
			_, err := d.ResolveCRD(context.Background(), "test", "missing.example.io")
			return err
		}},
		{"resource YAML", "/apis/gateway.networking.k8s.io/v1/namespaces/demo/tcproutes/missing", func(d *Direct) error {
			_, err := d.GetCustomResourceYAML(context.Background(), "test", domainresource.CRDResourceDefinition{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "tcproutes", Kind: "TCPRoute", Namespaced: true}, "demo", "missing")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != test.path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404}`))
			}))
			defer server.Close()
			kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
			manager := k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})
			if err := test.read(NewDirect(NewClusters(manager), nil)); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("missing CRD read must map to application not found: %v", err)
			}
		})
	}
}
