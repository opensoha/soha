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

	cr "github.com/opensoha/soha-contracts/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestRelationMetadataProjectionAndPartialCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/apis/widgets.test/v1" {
			_, _ = w.Write([]byte(`{"resources":[{"name":"widgets","kind":"Widget","namespaced":true}]}`))
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "q=0.9") {
			t.Error("full JSON fallback must not be negotiated")
		}
		if r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("labelSelector") != "app=demo" {
			t.Errorf("unbounded or different query: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"metadata":{"continue":"next"},"items":[{"metadata":{"name":"child","namespace":"team","uid":"child-uid","annotations":{"password":"synthetic-private"},"labels":{"token":"synthetic-private"},"ownerReferences":[{"apiVersion":"widgets.test/v1","kind":"Widget","name":"root","uid":"root-uid"}]},"spec":{"password":"synthetic-private"},"status":{"secret":"synthetic-private"}}]}`))
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	q := cr.RelationQuery{ID: "unknown", Source: cr.RelationSource{APIVersion: "widgets.test/v1", Resource: "widgets", Kind: "Widget", Namespace: "team"}, Selector: map[string]string{"app": "demo"}}
	view, err := ReadRelationSnapshot(context.Background(), client.CoreV1().RESTClient(), "cluster", cr.RelationSnapshotRequest{Queries: []cr.RelationQuery{q}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Sources) != 1 || view.Sources[0].Coverage.State != "partial" || !view.Sources[0].Coverage.Truncated || len(view.Sources[0].Objects) != 1 {
		t.Fatalf("incorrect result: %+v", view)
	}
	data, _ := json.Marshal(view)
	if strings.Contains(string(data), "synthetic-private") {
		t.Fatal("configuration value leaked")
	}
	object := view.Sources[0].Objects[0]
	if object.Resource.UID != "child-uid" || object.Owners[0].UID != "root-uid" {
		t.Fatal("UID evidence lost")
	}
	q.Source.Kind = "DifferentKind"
	view, err = ReadRelationSnapshot(context.Background(), client.CoreV1().RESTClient(), "cluster", cr.RelationSnapshotRequest{Queries: []cr.RelationQuery{q}})
	if err != nil || len(view.Sources[0].Objects) != 0 || view.Sources[0].Coverage.State == "available" {
		t.Fatal("unverified GVR/kind accepted")
	}
}

func TestRelationMetadataNegotiationFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1" {
			_, _ = w.Write([]byte(`{"resources":[{"name":"secrets","kind":"Secret","namespaced":true}]}`))
			return
		}
		if strings.Contains(r.Header.Get("Accept"), ",application/json") {
			t.Error("full Secret requested")
		}
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	query := cr.RelationQuery{ID: "secrets", Source: cr.RelationSource{APIVersion: "v1", Resource: "secrets", Kind: "Secret", Namespace: "team"}}
	snapshot, err := ReadRelationSnapshot(context.Background(), client.CoreV1().RESTClient(), "cluster", cr.RelationSnapshotRequest{Queries: []cr.RelationQuery{query}})
	if err != nil || len(snapshot.Sources[0].Objects) != 0 || snapshot.Sources[0].Coverage.State == "available" {
		t.Fatalf("metadata refusal not closed: %+v %v", snapshot, err)
	}
}

func TestRelationReadErrorsRetainCoverageReasons(t *testing.T) {
	for _, tc := range []struct {
		err           error
		state, reason string
	}{
		{fmt.Errorf("request: %w", context.DeadlineExceeded), "error", "timeout"},
		{context.Canceled, "error", "cancelled"},
		{errRelationSourceNotServed, "not_installed", "source_not_served"},
		{errors.New("opaque server response"), "error", "source_unavailable"},
	} {
		state, reason := relationReadError(tc.err)
		if state != tc.state || reason != tc.reason {
			t.Fatalf("%v became %s/%s", tc.err, state, reason)
		}
	}
}
