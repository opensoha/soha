package runtime

import (
	"context"
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

func TestManifestInitializationCreatesWithoutAdoption(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "namespace": "test"}}}
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "concurrently-created"}[existing], func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(runtime.NewScheme())
			if existing {
				if err := client.Tracker().Create(gvr, object.DeepCopy(), "test"); err != nil {
					t.Fatal(err)
				}
			}
			_, err := CreateManifest(t.Context(), client.Resource(gvr).Namespace("test"), object.DeepCopy(), metav1.CreateOptions{})
			if existing && !apierrors.IsAlreadyExists(err) || !existing && err != nil {
				t.Fatalf("create existing=%v: %v", existing, err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "create" {
					t.Fatalf("initialization used %s instead of atomic create", action.GetVerb())
				}
			}
		})
	}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		typed, ok := action.(interface{ GetCreateOptions() metav1.CreateOptions })
		if !ok {
			t.Fatalf("missing create options: %T", action)
		}
		if options := typed.GetCreateOptions(); len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
			t.Fatal("preflight lost server dry-run")
		}
		return true, object.DeepCopy(), nil
	})
	if _, err := CreateManifest(context.Background(), client.Resource(gvr).Namespace("test"), object, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
		t.Fatal(err)
	}
}

func TestManifestInitializationRejectsUnsafeModesAndReplacedUID(t *testing.T) {
	p := sohaapi.ManifestExecutionTaskPayload{InitializationPlanID: "plan", Action: sohaapi.ManifestTaskActionApply}
	if err := ValidateManifestInitialization(p); err != nil {
		t.Fatal(err)
	}
	for _, action := range []sohaapi.ManifestTaskAction{sohaapi.ManifestTaskActionRepair, sohaapi.ManifestTaskActionRollback, sohaapi.ManifestTaskActionAdopt} {
		p.Action = action
		if err := ValidateManifestInitialization(p); err == nil {
			t.Fatalf("accepted initialization %s", action)
		}
	}
	p.Action, p.ForceConflicts = sohaapi.ManifestTaskActionApply, true
	if ValidateManifestInitialization(p) == nil {
		t.Fatal("initialization accepted forced conflicts")
	}
	live := &unstructured.Unstructured{}
	live.SetUID("created-uid")
	if err := ValidateInitializedUID(live, "created-uid"); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"", "replaced-uid"} {
		if ValidateInitializedUID(live, expected) == nil {
			t.Fatal("unproven initialization UID was accepted")
		}
	}
}
