package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestRootCRPublicationFences(t *testing.T) {
	for _, mode := range []string{"apply", "dry-run", "retry", "replaced", "changed", "deleted", "deleting", "helm", "git", "controller", "status", "finalizer", "force", "conflict-replaced"} {
		t.Run(mode, func(t *testing.T) {
			root, live, desired, options := rootPublicationFixture(mode)
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
			gvr := schema.GroupVersionResource{Group: "unknown.example", Version: "v1", Resource: "databases"}
			resources := client.Resource(gvr).Namespace("team")
			if mode == "deleted" {
				if err := resources.Delete(context.Background(), root.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				client.ClearActions()
			}
			writes := rootPublicationUpdates(t, client, root, live, mode, gvr)
			_, err := ApplyRootCR(context.Background(), resources, desired, root, options)
			valid := mode == "apply" || mode == "dry-run" || mode == "retry"
			if valid && err != nil || !valid && err == nil {
				t.Fatalf("error=%v valid=%v", err, valid)
			}
			expected := 0
			if mode == "apply" || mode == "dry-run" || mode == "conflict-replaced" {
				expected = 1
			}
			if *writes != expected {
				t.Fatalf("writes=%d want=%d", *writes, expected)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "update" {
					t.Fatalf("unexpected mutation: %s", action.GetVerb())
				}
			}
		})
	}
}

func rootPublicationFixture(mode string) (sohaapi.ManifestRootResource, *unstructured.Unstructured, *unstructured.Unstructured, metav1.PatchOptions) {
	root := sohaapi.ManifestRootResource{APIVersion: "unknown.example/v1", Kind: "Database", Namespace: "team", Name: "db", UID: "uid-1", Generation: 2, CrdName: "databases.unknown.example"}
	live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": root.APIVersion, "kind": root.Kind, "metadata": map[string]any{"namespace": root.Namespace, "name": root.Name, "uid": root.UID, "resourceVersion": "12", "generation": int64(2)}, "spec": map[string]any{"size": int64(1)}}}
	desired := &unstructured.Unstructured{Object: map[string]any{"apiVersion": root.APIVersion, "kind": root.Kind, "metadata": map[string]any{"namespace": root.Namespace, "name": root.Name}, "spec": map[string]any{"size": int64(2)}}}
	force := mode == "force"
	options := metav1.PatchOptions{FieldManager: "opensoha-manifest/binding", Force: &force}
	switch mode {
	case "dry-run":
		options.DryRun = []string{metav1.DryRunAll}
	case "retry":
		live.Object["spec"] = desired.Object["spec"]
		live.SetGeneration(3)
	case "replaced":
		live.SetUID("uid-2")
	case "changed":
		live.SetGeneration(3)
	case "deleting":
		now := metav1.Now()
		live.SetDeletionTimestamp(&now)
	case "helm":
		live.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "Helm"})
	case "git":
		live.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "argocd-controller"}})
	case "controller":
		yes := true
		live.SetOwnerReferences([]metav1.OwnerReference{{UID: "parent", Controller: &yes}})
	case "status":
		desired.Object["status"] = map[string]any{"ready": true}
	case "finalizer":
		desired.SetFinalizers([]string{"operator/hold"})
	}
	return root, live, desired, options
}

func rootPublicationUpdates(t *testing.T, client *fake.FakeDynamicClient, root sohaapi.ManifestRootResource, live *unstructured.Unstructured, mode string, gvr schema.GroupVersionResource) *int {
	t.Helper()
	writes := new(int)
	client.PrependReactor("update", "databases", func(action ktesting.Action) (bool, runtime.Object, error) {
		*writes++
		update, ok := action.(ktesting.UpdateActionImpl)
		if !ok {
			t.Fatalf("expected update action, got %T", action)
		}
		object, ok := update.GetObject().(*unstructured.Unstructured)
		if !ok {
			t.Fatalf("expected unstructured root, got %T", update.GetObject())
		}
		if mode == "dry-run" && len(update.GetUpdateOptions().DryRun) != 1 {
			t.Fatal("preflight performed a real update")
		}
		body := object.Object
		if string(object.GetUID()) != root.UID || object.GetResourceVersion() != "12" || !reflect.DeepEqual(object.Object["status"], live.Object["status"]) {
			t.Fatalf("unsafe patch: %v", body)
		}
		if mode == "conflict-replaced" {
			replaced := live.DeepCopy()
			replaced.SetUID("uid-2")
			if err := client.Tracker().Update(gvr, replaced, "team"); err != nil {
				t.Fatal(err)
			}
			return true, nil, apierrors.NewConflict(gvr.GroupResource(), root.Name, nil)
		}
		return true, object, nil
	})
	return writes
}
