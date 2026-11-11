package runtime

import (
	"context"
	"encoding/json"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
)

func TestWorkloadCronJobHealthRequiresCurrentOwnedResources(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(root, source, target *unstructured.Unstructured)
		want   string
	}{
		{"synchronized configuration", func(_, _, _ *unstructured.Unstructured) {}, "healthy"},
		{"old CR generation", func(root, _, _ *unstructured.Unstructured) { root.SetGeneration(3) }, "progressing"},
		{"old source version", func(_, source, _ *unstructured.Unstructured) { source.SetResourceVersion("12") }, "progressing"},
		{"unobserved target changes", func(_, _, target *unstructured.Unstructured) { target.SetResourceVersion("22") }, "progressing"},
		{"replaced source", func(_, source, _ *unstructured.Unstructured) { source.SetUID(types.UID("new-source")) }, "degraded"},
		{"replaced target", func(_, _, target *unstructured.Unstructured) { target.SetUID(types.UID("new-target")) }, "degraded"},
		{"foreign controller", func(_, _, target *unstructured.Unstructured) { target.SetOwnerReferences(nil) }, "degraded"},
		{"deleting target", func(_, _, target *unstructured.Unstructured) { now := metav1.Now(); target.SetDeletionTimestamp(&now) }, "degraded"},
		{"deleting root", func(root, _, _ *unstructured.Unstructured) { now := metav1.Now(); root.SetDeletionTimestamp(&now) }, "degraded"},
		{"missing controller evidence", func(root, _, _ *unstructured.Unstructured) {
			unstructured.RemoveNestedField(root.Object, "status", "cronJobRef", "resourceVersion")
		}, "unknown"},
		{"cross namespace reference", func(root, _, _ *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(root.Object, "other", "status", "cronJobRef", "namespace")
		}, "unknown"},
		{"unsupported source", func(root, _, _ *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(root.Object, "Secret", "spec", "sourceRef", "kind")
		}, "unknown"},
		{"controller failure", func(root, _, _ *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(root.Object, []any{map[string]any{"type": "Ready", "status": "False", "reason": "SourceNotFound", "observedGeneration": int64(2)}}, "status", "conditions")
		}, "degraded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, source, target := cronJobHealthFixture(t)
			test.change(root, source, target)
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), source, target)
			got, err := ManifestHealth(context.Background(), client, root)
			if err != nil || got != test.want {
				t.Fatalf("health = %s, %v; want %s", got, err, test.want)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" || action.GetNamespace() != "demo" {
					t.Fatalf("unexpected action: %#v", action)
				}
			}
		})
	}
	root, _, _ := cronJobHealthFixture(t)
	if got := contractresource.ManifestHealth(root.Object); got != "unknown" {
		t.Fatalf("root condition alone must not prove health: %s", got)
	}
}

func cronJobHealthFixture(t *testing.T) (*unstructured.Unstructured, *unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	decode := func(raw string) *unstructured.Unstructured {
		var object map[string]any
		if err := json.Unmarshal([]byte(raw), &object); err != nil {
			t.Fatal(err)
		}
		return &unstructured.Unstructured{Object: object}
	}
	root := decode(`{"apiVersion":"workloads.soha.io/v1alpha1","kind":"WorkloadCronJob","metadata":{"name":"daily","namespace":"demo","uid":"root-uid","generation":2},"spec":{"sourceRef":{"kind":"Deployment","name":"web"}},"status":{"observedGeneration":2,"sourceUid":"source-uid","sourceResourceVersion":"11","cronJobRef":{"apiVersion":"batch/v1","kind":"CronJob","namespace":"demo","name":"daily","uid":"target-uid","resourceVersion":"21"},"conditions":[{"type":"Ready","status":"True","reason":"Reconciled","observedGeneration":2}]}}`)
	source := decode(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"demo","uid":"source-uid","resourceVersion":"11"}}`)
	target := decode(`{"apiVersion":"batch/v1","kind":"CronJob","metadata":{"name":"daily","namespace":"demo","uid":"target-uid","resourceVersion":"21","ownerReferences":[{"apiVersion":"workloads.soha.io/v1alpha1","kind":"WorkloadCronJob","name":"daily","uid":"root-uid","controller":true}]}}`)
	return root, source, target
}
