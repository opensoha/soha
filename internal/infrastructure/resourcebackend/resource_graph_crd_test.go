package resourcebackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

func TestResourceGraphCRDFallbackVersionsAreServedOnly(t *testing.T) {
	object := unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "widgets.example.io", "uid": "definition-uid"},
		"spec": map[string]interface{}{"group": "example.io", "scope": "Namespaced", "names": map[string]interface{}{"kind": "Widget", "plural": "widgets"}, "versions": []interface{}{
			map[string]interface{}{"name": "v1", "served": true, "storage": true},
			map[string]interface{}{"name": "v2", "served": false},
			map[string]interface{}{"name": "v3"},
		}},
	}}
	view := mapCRD(object)
	if len(view.Versions) != 1 || view.Versions[0] != "v1" || view.UID != "definition-uid" {
		t.Fatalf("unserved version escaped fallback: %+v", view)
	}
}

func TestResourceGraphCRDCatalogIsOneMetadataPageWithoutSchemaFallback(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		count                                 int
		continuation                          string
		metadataStatus                        int32
		discoveryFailure, unmapped, wantError bool
	}{
		{name: "bounded success", count: 1},
		{name: "continue rejected", count: 1, continuation: "next", wantError: true},
		{name: "oversized page rejected", count: 501, wantError: true},
		{name: "metadata unsupported", count: 1, metadataStatus: http.StatusNotAcceptable, wantError: true},
		{name: "discovery failure", count: 1, discoveryFailure: true, wantError: true},
		{name: "unmapped definition", count: 1, unmapped: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var metadataReads, fullReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions" {
					metadataReads.Add(1)
					if r.Header.Get("Accept") != partialMetadataListAcceptType {
						fullReads.Add(1)
						t.Errorf("full-schema CRD request: %s", r.Header.Get("Accept"))
					}
					if r.URL.Query().Get("limit") != "501" || r.URL.Query().Get("continue") != "" {
						t.Errorf("unbounded or repeated page query: %s", r.URL.RawQuery)
					}
					if tc.metadataStatus != 0 {
						w.WriteHeader(int(tc.metadataStatus))
						_ = json.NewEncoder(w).Encode(metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotAcceptable, Code: tc.metadataStatus})
						return
					}
					items := make([]metav1.PartialObjectMetadata, tc.count)
					for i := range items {
						items[i] = metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.io", UID: "definition-uid"}}
					}
					_ = json.NewEncoder(w).Encode(metav1.PartialObjectMetadataList{TypeMeta: metav1.TypeMeta{Kind: "PartialObjectMetadataList", APIVersion: "meta.k8s.io/v1"}, ListMeta: metav1.ListMeta{Continue: tc.continuation}, Items: items})
					return
				}
				if !serveResourceGraphCRDDiscovery(w, r, tc.discoveryFailure, tc.unmapped) {
					fullReads.Add(1)
					t.Errorf("unexpected schema/resource read: %s", r.URL)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			bundle := &k8sinfra.Bundle{RESTConfig: &rest.Config{Host: server.URL}, Dynamic: dynamic}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			views, err := listResourceGraphCRDs(ctx, bundle)
			if (err != nil) != tc.wantError {
				t.Fatalf("views=%+v err=%v", views, err)
			}
			if metadataReads.Load() != 1 || fullReads.Load() != 0 || len(dynamic.Actions()) != 0 {
				t.Fatalf("schema fallback or pagination occurred: metadata=%d full=%d dynamic=%v", metadataReads.Load(), fullReads.Load(), dynamic.Actions())
			}
			if !tc.wantError && (len(views) != 1 || views[0].UID != "definition-uid" || len(views[0].Versions) != 1 || views[0].Versions[0] != "v1") {
				t.Fatalf("discovered identity lost: %+v", views)
			}
		})
	}
}

func serveResourceGraphCRDDiscovery(w http.ResponseWriter, r *http.Request, discoveryFailure, unmapped bool) bool {
	if discoveryFailure {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(metav1.Status{Status: "Failure", Reason: metav1.StatusReasonForbidden, Code: 403})
		return true
	}
	switch r.URL.Path {
	case "/api":
		_ = json.NewEncoder(w).Encode(metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{"v1"}})
	case "/api/v1":
		_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: "v1"})
	case "/apis":
		groups := []metav1.APIGroup{}
		if !unmapped {
			groups = append(groups, metav1.APIGroup{Name: "example.io", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "example.io/v1", Version: "v1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "example.io/v1", Version: "v1"}})
		}
		_ = json.NewEncoder(w).Encode(metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}, Groups: groups})
	case "/apis/example.io/v1":
		_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: "example.io/v1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}}}})
	default:
		return false
	}
	return true
}
