package resourcebackend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	shared "github.com/opensoha/soha-contracts/resource/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestResourceGraphNetworkAdapterUsesSharedTopologyCollector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1":
			_, _ = fmt.Fprint(w, `{"resources":[{"name":"services","kind":"Service","namespaced":true},{"name":"pods","kind":"Pod","namespaced":true}]}`)
		case "/api/v1/namespaces/team/services/api":
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Service","metadata":{"name":"api","namespace":"team","uid":"service-uid"},"spec":{"selector":{"app":"api"}}}`)
		case "/api/v1/services":
			_, _ = fmt.Fprint(w, `{"items":[{"metadata":{"name":"api","namespace":"team","uid":"service-uid"},"spec":{"selector":{"app":"api"}}}]}`)
		case "/api/v1/pods":
			_, _ = fmt.Fprint(w, `{"items":[{"metadata":{"name":"pod","namespace":"team","uid":"pod-uid","labels":{"app":"api"}},"status":{"phase":"Running"}}]}`)
		default:
			if strings.Contains(r.URL.Path, "secrets") {
				t.Error("Secret read")
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	got, err := directNetworkResourceGraph(context.Background(), client, nil, client.CoreV1().RESTClient(), "c", "team", "Service", "api")
	if err != nil {
		t.Fatal(err)
	}
	want, err := shared.BuildKubernetesResourceGraphWithNetworkSources(context.Background(), client, nil, client.CoreV1().RESTClient(), cr.ResourceRef{ClusterID: "c", Namespace: "team", Kind: "Service", Name: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if got.RootID != want.RootID || !reflect.DeepEqual(got.Nodes, want.Nodes) || !reflect.DeepEqual(got.Edges, want.Edges) {
		t.Fatalf("network adapter diverged: %+v %+v", got, want)
	}
	if len(got.Edges) == 0 {
		t.Fatal("selector relationship missing")
	}
}
