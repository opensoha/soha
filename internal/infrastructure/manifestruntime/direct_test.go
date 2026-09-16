package manifestruntime

import (
	"context"
	"testing"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestInventoryRetainsResourceObservations(t *testing.T) {
	live := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"observedGeneration": int64(0)}}}
	live.SetGeneration(3)
	live.SetFinalizers([]string{"test.soha.io/cleanup"})
	now := metav1.Now()
	live.SetDeletionTimestamp(&now)
	item := inventoryItem(domainmanifest.TaskPayload{Generation: 12}, domainmanifest.RenderedDocument{}, live, live)
	if item.Generation != 12 || item.ResourceGeneration != 3 || item.ObservedResourceGeneration == nil || *item.ObservedResourceGeneration != 0 || item.DeletingAt == nil || !item.DeletingAt.Equal(live.GetDeletionTimestamp().Time) || len(item.Finalizers) != 1 {
		t.Fatalf("lost resource observations: %#v", item)
	}
	unstructured.RemoveNestedField(live.Object, "status", "observedGeneration")
	if inventoryItem(domainmanifest.TaskPayload{}, domainmanifest.RenderedDocument{}, live, live).ObservedResourceGeneration != nil {
		t.Fatal("missing controller observation was fabricated as zero")
	}
}

type staticRESTMapper struct{ mapping *meta.RESTMapping }

func (m staticRESTMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return m.mapping, nil
}

func TestObserveDocumentsFailsWhenResourceIsUnavailable(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	mapper := staticRESTMapper{mapping: &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		Scope:            meta.RESTScopeNamespace,
	}}
	payload := domainmanifest.TaskPayload{
		Action: domainmanifest.TaskActionObserve, PackageID: "package-1", BindingID: "binding-1", DeploymentID: "deployment-1",
		Generation: 2, IdempotencyKey: "manifest:test:observe", ClusterID: "cluster-1", Namespace: "payments",
		Documents: []domainmanifest.RenderedDocument{{
			Index: 0, Path: "configmap.yaml", APIVersion: "v1", Kind: "ConfigMap",
			Namespace: "payments", Name: "missing", ContentDigest: "digest",
			Content: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"missing","namespace":"payments"}}`,
		}},
	}

	result, err := observeDocuments(context.Background(), client, mapper, payload)
	if err == nil {
		t.Fatal("observeDocuments() error = nil, want unavailable resource failure")
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Stage != "observe" {
		t.Fatalf("diagnostics = %#v, want one observe diagnostic", result.Diagnostics)
	}
}
