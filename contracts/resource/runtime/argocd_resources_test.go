package runtime

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestArgoFrozenResourcesRejectUnsafeWrites(t *testing.T) {
	for _, mode := range []string{"valid", "outside", "duplicate", "hook", "image", "init image", "external", "status", "missing image"} {
		t.Run(mode, func(t *testing.T) {
			app, live := argoFixture(t)
			desired := live.DeepCopy()
			desired.SetUID("")
			desired.SetAnnotations(nil)
			desired.SetGeneration(0)
			delete(desired.Object, "status")
			resources := []*unstructured.Unstructured{desired}
			switch mode {
			case "outside":
				desired.SetNamespace("other")
			case "duplicate":
				resources = append(resources, desired.DeepCopy())
			case "hook":
				desired.SetAnnotations(map[string]string{"argocd.argoproj.io/hook": "PreSync"})
			case "image":
				_ = unstructured.SetNestedSlice(desired.Object, []any{map[string]any{"name": "app", "image": "mutable:latest"}}, "spec", "template", "spec", "containers")
			case "init image":
				_ = unstructured.SetNestedSlice(desired.Object, []any{map[string]any{"name": "init", "image": "mutable:latest"}}, "spec", "template", "spec", "initContainers")
			case "external":
				desired.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "Helm"})
			case "status":
				desired.Object["status"] = map[string]any{"observedGeneration": int64(1)}
			case "missing image":
				unstructured.RemoveNestedField(desired.Object, "spec", "template", "spec", "containers")
			}
			if err := ValidateArgoResources(app, resources); (err == nil) != (mode == "valid") {
				t.Fatalf("%s: %v", mode, err)
			}
		})
	}
}

func TestArgoResourceOwnerCheckIsReadOnlyAndRejectsAdoption(t *testing.T) {
	for _, mode := range []string{"owned", "absent", "unowned", "other app", "direct", "helm"} {
		t.Run(mode, func(t *testing.T) {
			app, live := argoFixture(t)
			desired := live.DeepCopy()
			desired.SetUID("")
			desired.SetAnnotations(nil)
			desired.SetGeneration(0)
			delete(desired.Object, "status")
			switch mode {
			case "unowned":
				live.SetAnnotations(nil)
			case "other app":
				live.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "other:apps/Deployment:demo/web"})
			case "direct":
				live.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "opensoha-manifest/other"}})
			case "helm":
				live.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "Helm"})
			}
			objects := []runtime.Object{live}
			if mode == "absent" {
				objects = nil
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			err := CheckArgoResourceOwners(t.Context(), client, app, []*unstructured.Unstructured{desired})
			if (err == nil) != (mode == "owned" || mode == "absent") {
				t.Fatalf("%s: %v", mode, err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("ownership check mutated a resource")
				}
			}
		})
	}
}

func TestArgoApplicationRejectsDirectWriters(t *testing.T) {
	app, _ := argoFixture(t)
	for _, action := range []string{"apply", "update", "delete"} {
		client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app.DeepCopy())
		resource := client.Resource(argoApplications).Namespace(app.GetNamespace())
		var err error
		switch action {
		case "apply":
			_, err = ApplyManifest(t.Context(), resource, argoDesiredFixture(app), metav1.PatchOptions{FieldManager: "direct"})
		case "update":
			_, err = UpdateManifest(t.Context(), resource, app.DeepCopy())
		case "delete":
			err = DeleteManifest(t.Context(), resource, app.GetName(), string(app.GetUID()))
		}
		if err == nil {
			t.Fatalf("direct %s accepted GitOps root", action)
		}
		for _, call := range client.Actions() {
			if call.GetVerb() != "get" {
				t.Fatal("direct writer mutated GitOps root")
			}
		}
	}
}
