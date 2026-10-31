package resourcebackend

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
)

func TestDirectPresenceRequiresSuccessfulExactMetadataCollection(t *testing.T) {
	for _, scenario := range []string{"absent", "present", "missing-api", "full-object"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/team-a/configmaps" || r.URL.Query().Get("fieldSelector") != "metadata.name=app" || r.Header.Get("Accept") != "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1" {
					t.Error("direct presence expanded target or full-object fallback")
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if scenario == "missing-api" {
					w.WriteHeader(404)
					return
				}
				if scenario == "full-object" {
					_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"ConfigMapList","metadata":{"resourceVersion":"v"},"items":[]}`)
					return
				}
				items := ""
				if scenario == "present" {
					items = `{"metadata":{"name":"app","namespace":"team-a","uid":"original-uid","resourceVersion":"object-rv"}}`
				}
				_, _ = fmt.Fprintf(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"collection-rv"},"items":[%s]}`, items)
			}))
			defer server.Close()
			kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
			direct := NewDirect(NewClusters(k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})), nil)
			result, err := direct.ObserveResourcePresence(t.Context(), "test", "team-a", "ConfigMap", "app")
			valid := scenario == "absent" || scenario == "present"
			if (err == nil) != valid || valid && result.State != scenario {
				t.Fatal("missing API/full-object fallback became object absence", err)
			}
		})
	}
}
