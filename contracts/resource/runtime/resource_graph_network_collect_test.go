package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestNetworkGraphNamedRootOutsideBoundedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1":
			_, _ = fmt.Fprint(w, `{"resources":[{"name":"services","kind":"Service","namespaced":true}]}`)
		case "/api/v1/namespaces/team/services/root":
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Service","metadata":{"name":"root","namespace":"team","uid":"root-uid"},"spec":{"type":"ClusterIP"}}`)
		case "/api/v1/services":
			if r.URL.Query().Get("limit") != "200" {
				t.Error("unbounded list")
			}
			items := []any{}
			for i := 0; i < 201; i++ {
				items = append(items, map[string]any{"metadata": map[string]any{"name": fmt.Sprint("svc-", i), "namespace": "team", "uid": fmt.Sprint("uid-", i)}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"continue": "next"}, "items": items})
		default:
			if strings.Contains(r.URL.Path, "secrets") {
				t.Error("Secret data read")
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	graph, err := BuildKubernetesResourceGraphWithNetworkSources(context.Background(), client, nil, client.CoreV1().RESTClient(), cr.ResourceRef{ClusterID: "c", Namespace: "team", Kind: "service", Name: "root"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range graph.Nodes {
		if node.ID == graph.RootID && node.Resource.Name == "root" && node.Resource.UID == "root-uid" {
			found = true
		}
	}
	if !found || len(graph.Warnings) == 0 {
		t.Fatalf("named root or truncation missing: %+v", graph)
	}
}

func TestNetworkGraphNamedSnapshotIdentityErrors(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"GVK", `{"apiVersion":"example.io/v1","kind":"Service","metadata":{"name":"root","namespace":"team","uid":"uid"}}`},
		{"namespace", `{"apiVersion":"v1","kind":"Service","metadata":{"name":"root","namespace":"other","uid":"uid"}}`},
		{"UID", `{"apiVersion":"v1","kind":"Service","metadata":{"name":"root","namespace":"team","uid":""}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1" {
					_, _ = fmt.Fprint(w, `{"resources":[{"name":"services","kind":"Service","namespaced":true}]}`)
				} else {
					_, _ = fmt.Fprint(w, tc.body)
				}
			}))
			defer server.Close()
			client, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			_, err := BuildKubernetesResourceGraphWithNetworkSources(context.Background(), client, nil, client.CoreV1().RESTClient(), cr.ResourceRef{ClusterID: "c", Namespace: "team", Kind: "Service", Name: "root"})
			if !errors.Is(err, ErrInvalidResourceGraphRoot) {
				t.Fatalf("identity accepted: %v", err)
			}
		})
	}
}

func TestNetworkGraphNamedSnapshotCoverageAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reason string
	}{{"missing", 404, "named_not_found"}, {"denied", 403, "kubernetes_rbac"}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1" {
					_, _ = fmt.Fprint(w, `{"resources":[{"name":"services","kind":"Service","namespaced":true}]}`)
					return
				}
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "apiVersion": "v1", "code": tc.status, "reason": map[int]string{404: "NotFound", 403: "Forbidden"}[tc.status], "message": "private details"})
			}))
			defer server.Close()
			client, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			query := cr.NetworkTopologySourceQuery{SourceID: "kubernetes.services", Namespace: "team", Name: "root"}
			snapshot, err := ReadNetworkTopologySnapshot(context.Background(), client.CoreV1().RESTClient(), "c", cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{query}})
			if err != nil || snapshot.Version != cr.NetworkTopologyNamedRootFeature || len(snapshot.Sources) != 1 || snapshot.Sources[0].Coverage.Name != "root" || snapshot.Sources[0].Coverage.ReasonCode != tc.reason {
				t.Fatalf("coverage lost: %+v %v", snapshot, err)
			}
			raw, _ := json.Marshal(snapshot)
			if strings.Contains(string(raw), "private details") {
				t.Fatal("provider detail leaked")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := ReadNetworkTopologySnapshot(ctx, client.CoreV1().RESTClient(), "c", cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{query}}); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel lost: %v", err)
			}
		})
	}
}

func TestNetworkGraphNonNetworkFakeKeepsExistingCollector(t *testing.T) {
	client := fake.NewClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "team", UID: "uid"}})
	graph, err := BuildKubernetesResourceGraphWithNetworkSources(context.Background(), client, nil, nil, cr.ResourceRef{ClusterID: "c", Namespace: "team", Kind: "Pod", Name: "pod"})
	if err != nil || len(graph.Nodes) == 0 {
		t.Fatalf("nonnetwork fallback: %+v %v", graph, err)
	}
	if _, err := BuildKubernetesResourceGraphWithNetworkSources(context.Background(), client, nil, nil, cr.ResourceRef{ClusterID: "c", Namespace: "team", Kind: "Service", Name: "root"}); !errors.Is(err, ErrUnsupportedResourceGraphRoot) {
		t.Fatalf("missing network transport: %v", err)
	}
}

func TestNetworkGraphNamedRootByteBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1" {
			_, _ = fmt.Fprint(w, `{"resources":[{"name":"services","kind":"Service","namespaced":true}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Service","metadata":{"name":"root","namespace":"team","uid":"uid","annotations":{"oversized":"`+strings.Repeat("x", cr.NetworkTopologyMaxSourceBytes)+`"}}}`)
	}))
	defer server.Close()
	client, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	snapshot, err := ReadNetworkTopologySnapshot(context.Background(), client.CoreV1().RESTClient(), "c", cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: "kubernetes.services", Namespace: "team", Name: "root"}}})
	if err != nil || len(snapshot.Sources) != 1 || snapshot.Sources[0].Coverage.State != "partial" || snapshot.Sources[0].Coverage.ReasonCode != "byte_budget" || len(snapshot.Sources[0].Objects) != 0 {
		t.Fatalf("named byte limit lost: %+v %v", snapshot, err)
	}
}
