package runtime

import (
	"context"
	"encoding/json"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestManifestDeleteFencesUIDAndWaitsForFinalizers(t *testing.T) {
	for _, mode := range []string{"accepted", "replaced", "deleting", "external"} {
		t.Run(mode, func(t *testing.T) {
			live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "workloads.soha.io/v1alpha1", "kind": "WorkloadCronJob", "metadata": map[string]any{"name": "periodic", "namespace": "demo", "uid": "root-uid", "resourceVersion": "12"}}}
			live.SetFinalizers([]string{"operator.example/hold"})
			if mode == "deleting" {
				now := metav1.Now()
				live.SetDeletionTimestamp(&now)
			}
			if mode == "external" {
				live.SetLabels(map[string]string{"argocd.argoproj.io/instance": "external"})
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
			writes := 0
			client.PrependReactor("delete", "workloadcronjobs", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				deleteAction, ok := action.(ktesting.DeleteAction)
				if !ok {
					t.Fatal("unexpected delete action")
				}
				options := deleteAction.GetDeleteOptions()
				if options.Preconditions == nil || *options.Preconditions.UID != live.GetUID() || *options.Preconditions.ResourceVersion != "12" || options.PropagationPolicy == nil || *options.PropagationPolicy != metav1.DeletePropagationForeground {
					t.Fatalf("missing delete fences: %+v", options)
				}
				return true, nil, nil
			})
			uid := "root-uid"
			if mode == "replaced" {
				uid = "previous-uid"
			}
			resources := client.Resource(schema.GroupVersionResource{Group: "workloads.soha.io", Version: "v1alpha1", Resource: "workloadcronjobs"}).Namespace("demo")
			err := DeleteManifest(context.Background(), resources, "periodic", uid)
			if mode == "replaced" && !apierrors.IsConflict(err) || mode == "external" && err == nil || (mode == "accepted" || mode == "deleting") && err != nil {
				t.Fatalf("delete: %v", err)
			}
			if (mode == "accepted" && writes != 1) || (mode != "accepted" && writes != 0) {
				t.Fatalf("delete calls: %d", writes)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "delete" {
					t.Fatalf("unexpected finalizer write: %s", action.GetVerb())
				}
			}
		})
	}
}

func TestManifestUpdatePreservesCallerVersion(t *testing.T) {
	live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "namespace": "demo", "uid": "current", "resourceVersion": "12"}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
	desired := live.DeepCopy()
	desired.SetUID("previous")
	desired.SetResourceVersion("11")
	client.PrependReactor("update", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		update, ok := action.(ktesting.UpdateAction)
		if !ok {
			t.Fatal("unexpected update action")
		}
		write, ok := update.GetObject().(*unstructured.Unstructured)
		if !ok {
			t.Fatal("unexpected update object")
		}
		if write.GetUID() != "previous" || write.GetResourceVersion() != "11" {
			t.Fatal("caller version was overwritten")
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "settings", nil)
	})
	_, err := UpdateManifest(context.Background(), client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("demo"), desired)
	if !apierrors.IsConflict(err) {
		t.Fatalf("update: %v", err)
	}
}

func TestDirectManifestRequiresOneWriter(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*unstructured.Unstructured)
	}{
		{"controller", func(object *unstructured.Unstructured) {
			yes := true
			object.SetOwnerReferences([]metav1.OwnerReference{{Controller: &yes, UID: "owner"}})
		}},
		{"deleting", func(object *unstructured.Unstructured) { now := metav1.Now(); object.SetDeletionTimestamp(&now) }},
		{"Helm", func(object *unstructured.Unstructured) {
			object.SetAnnotations(map[string]string{"meta.helm.sh/release-name": "external"})
		}},
		{"Argo CD", func(object *unstructured.Unstructured) {
			object.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "external"})
		}},
		{"Flux", func(object *unstructured.Unstructured) {
			object.SetLabels(map[string]string{"kustomize.toolkit.fluxcd.io/name": "external"})
		}},
		{"GitOps manager", func(object *unstructured.Unstructured) {
			object.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "argocd-controller"}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			object := &unstructured.Unstructured{Object: map[string]any{}}
			if err := ValidateDirectManifestOwner(object); err != nil {
				t.Fatal(err)
			}
			test.change(object)
			if err := ValidateDirectManifestOwner(object); err == nil {
				t.Fatal("external ownership was accepted")
			}
		})
	}
}

func TestManifestWritePreservesDryRunAndIdentity(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "apply"}[exists], func(t *testing.T) {
			desired := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "namespace": "demo"}}}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme())
			verb := "create"
			if exists {
				live := desired.DeepCopy()
				live.SetUID("settings-uid")
				live.SetResourceVersion("12")
				client = fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
				verb = "patch"
			}
			writes := 0
			client.PrependReactor(verb, "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				if exists {
					patch, ok := action.(ktesting.PatchAction)
					if !ok {
						t.Fatal("unexpected patch action")
					}
					var body unstructured.Unstructured
					if err := json.Unmarshal(patch.GetPatch(), &body); err != nil {
						t.Fatal(err)
					}
					if body.GetUID() != "settings-uid" || body.GetResourceVersion() != "12" {
						t.Fatal("ownership read was not fenced")
					}
					patchOptions, ok := action.(interface{ GetPatchOptions() metav1.PatchOptions })
					if !ok {
						t.Fatal("unexpected patch options")
					}
					if len(patchOptions.GetPatchOptions().DryRun) != 1 {
						t.Fatal("patch lost dry-run")
					}
				} else {
					createOptions, ok := action.(interface{ GetCreateOptions() metav1.CreateOptions })
					if !ok {
						t.Fatal("unexpected create options")
					}
					if len(createOptions.GetCreateOptions().DryRun) != 1 {
						t.Fatal("create lost dry-run")
					}
				}
				return true, desired.DeepCopy(), nil
			})
			_, err := ApplyManifest(context.Background(), client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("demo"), desired, metav1.PatchOptions{FieldManager: "soha-test", DryRun: []string{metav1.DryRunAll}})
			if err != nil || writes != 1 || desired.GetUID() != "" || desired.GetResourceVersion() != "" {
				t.Fatalf("write = %d, %v; desired = %#v", writes, err, desired)
			}
		})
	}
}
