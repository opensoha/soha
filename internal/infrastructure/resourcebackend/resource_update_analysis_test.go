package resourcebackend

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAnalyzeResourceUpdateMapsChangedFieldsToManagers(t *testing.T) {
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "api", "namespace": "team-a"},
		"spec":     map[string]any{"replicas": int64(1)},
	}}
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager: "helm", Operation: metav1.ManagedFieldsOperationApply, APIVersion: "apps/v1",
		FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)},
	}})
	desired := live.DeepCopy()
	desired.Object["spec"].(map[string]any)["replicas"] = int64(2)

	analysis := analyzeResourceUpdate(live, desired)
	if len(analysis.ChangedFields) != 1 || analysis.ChangedFields[0] != "/spec/replicas" {
		t.Fatalf("changed fields = %#v", analysis.ChangedFields)
	}
	if len(analysis.Owners) != 1 || analysis.Owners[0].Manager != "helm" || len(analysis.Owners[0].Fields) != 1 || analysis.Owners[0].Fields[0] != "/spec/replicas" {
		t.Fatalf("owners = %#v", analysis.Owners)
	}
}

func TestAnalyzeResourceUpdateIncludesRemovedFields(t *testing.T) {
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Service",
		"metadata": map[string]any{"name": "api", "namespace": "team-a", "labels": map[string]any{"obsolete": "true"}},
	}}
	desired := live.DeepCopy()
	delete(desired.Object["metadata"].(map[string]any)["labels"].(map[string]any), "obsolete")

	analysis := analyzeResourceUpdate(live, desired)
	if len(analysis.ChangedFields) != 1 || analysis.ChangedFields[0] != "/metadata/labels/obsolete" {
		t.Fatalf("changed fields = %#v", analysis.ChangedFields)
	}
}
