package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestTopologySnapshotBoundsDiscoveryAndProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1":
			_, _ = fmt.Fprint(w, `{"resources":[{"name":"pods","kind":"Pod","namespaced":true},{"name":"services","kind":"Service","namespaced":true}]}`)
		case "/api/v1/namespaces/team/pods":
			if r.URL.Query().Get("limit") != "200" {
				t.Error("missing item bound")
			}
			_, _ = fmt.Fprint(w, `{"metadata":{"continue":"next"},"items":[{"metadata":{"name":"p","namespace":"team","uid":"uid","resourceVersion":"9"},"spec":{"secret":"credential"},"status":{"phase":"Running"}}]}`)
		case "/api/v1/namespaces/team/services":
			http.Error(w, `{"kind":"Status","apiVersion":"v1","reason":"Forbidden","code":403}`, http.StatusForbidden)
		case "/apis/traefik.io/v1alpha1":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	output, err := ReadNetworkTopologySnapshot(context.Background(), client.CoreV1().RESTClient(), "cluster", cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: "kubernetes.pods", Namespace: "team"}, {SourceID: "kubernetes.services", Namespace: "team"}, {SourceID: "traefik.ingressroutes", Namespace: "team"}}})
	if err != nil {
		t.Fatal(err)
	}
	if output.Sources[0].Coverage.State != "partial" || !output.Sources[0].Coverage.Truncated || output.Sources[1].Coverage.State != "denied" || output.Sources[2].Coverage.State != "not_installed" {
		t.Fatalf("coverage: %+v", output.Sources)
	}
	raw, _ := json.Marshal(output)
	if strings.Contains(string(raw), "credential") {
		t.Fatal("full manifest escaped")
	}
	if output.Sources[0].Objects[0].Resource.ResourceVersion != "9" {
		t.Fatal("identity lost")
	}
}

func TestTopologySnapshotDoesNotReadArbitrarySources(t *testing.T) {
	_, err := ReadNetworkTopologySnapshot(context.Background(), nil, "c", cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: "secrets"}}})
	if err == nil {
		t.Fatal("unknown source accepted")
	}
}
