package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestInitializationUsesCreateAndRejectsReplacedUID(t *testing.T) {
	content := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app","namespace":"team"},"data":{"key":"value"}}`
	digest := sha256.Sum256([]byte(content))
	document := sohaapi.ManifestRenderedDocument{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team", Name: "app", Content: content, ContentDigest: hex.EncodeToString(digest[:])}
	payload := sohaapi.ManifestExecutionTaskPayload{InitializationPlanID: "first", Action: "apply", ClusterID: "cluster", Namespace: "team", Documents: []sohaapi.ManifestRenderedDocument{document}}
	mapper := manifestTestMapper{}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(ktesting.CreateAction)
		if !ok {
			t.Fatalf("unexpected create action %T", action)
		}
		desired, ok := create.GetObject().(*unstructured.Unstructured)
		if !ok {
			t.Fatalf("unexpected create object %T", create.GetObject())
		}
		object := desired.DeepCopy()
		object.SetUID(types.UID("created"))
		object.SetResourceVersion("1")
		typed, ok := action.(interface{ GetCreateOptions() metav1.CreateOptions })
		if !ok {
			t.Fatalf("missing create options %T", action)
		}
		options := typed.GetCreateOptions()
		if len(options.DryRun) > 0 {
			return true, object, nil
		}
		return true, object, client.Tracker().Create(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, object, "team")
	})
	client.PrependReactor("patch", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		a, ok := action.(ktesting.PatchAction)
		if !ok {
			t.Fatalf("unexpected patch action %T", action)
		}
		if a.GetPatchType() != types.ApplyPatchType {
			t.Fatalf("unexpected patch %s", a.GetPatchType())
		}
		var object unstructured.Unstructured
		if err := json.Unmarshal(a.GetPatch(), &object.Object); err != nil {
			t.Fatal(err)
		}
		if object.GetUID() != "created" || object.GetResourceVersion() != "1" {
			t.Fatal("handoff lost created identity")
		}
		return true, &object, nil
	})
	executor := &Client{dynamic: client}
	result, err := executor.applyManifest(t.Context(), mapper, payload)
	if err != nil || result.InitializationPlanID != "first" || len(result.Inventory) != 1 || result.Inventory[0].UID != "created" {
		t.Fatalf("initial create receipt: %+v %v", result, err)
	}
	if actions := client.Actions(); len(actions) != 4 || actions[0].GetVerb() != "create" || actions[1].GetVerb() != "get" || actions[2].GetVerb() != "get" || actions[3].GetVerb() != "patch" {
		t.Fatalf("expected create then guarded SDK handoff, got %v", actions)
	}
	payload.Action = "observe"
	payload.Inventory = result.Inventory
	_, err = executor.observeManifest(t.Context(), mapper, payload)
	if err != nil {
		t.Fatal(err)
	}
	live, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("team").Get(context.Background(), "app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	live.SetUID(types.UID("replacement"))
	if err := client.Tracker().Update(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, live, "team"); err != nil {
		t.Fatal(err)
	}
	_, err = executor.observeManifest(t.Context(), mapper, payload)
	if err == nil {
		t.Fatal("same-name replacement confirmed original initialization")
	}
}

func TestInitializationHandoffRetainsCreateOnlyBoundary(t *testing.T) {
	for _, scenario := range []string{"ready", "exists", "dry_run", "replaced_first_get", "replaced_second_get", "disappeared", "foreign_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			client, desired, counts := newInitializationHandoffFixture(t, scenario)
			gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
			force := false
			options := metav1.PatchOptions{FieldManager: "test-init", Force: &force}
			if scenario == "dry_run" {
				options.DryRun = []string{metav1.DryRunAll}
			}
			_, err := createInitializedManifest(t.Context(), client.Resource(gvr).Namespace("team"), desired, options)
			success := scenario == "ready" || scenario == "dry_run"
			if (err == nil) != success {
				t.Fatalf("unexpected result %v", err)
			}
			if counts.creates != 1 {
				t.Fatalf("create count %d", counts.creates)
			}
			if !success && scenario != "foreign_conflict" && counts.patches != 0 {
				t.Fatal("unsafe handoff write")
			}
			if scenario == "dry_run" && (counts.gets != 0 || counts.patches != 0) {
				t.Fatal("preflight attempted ownership mutation")
			}
		})
	}
}

type initializationActionCounts struct {
	creates, gets, patches int
}

func newInitializationHandoffFixture(t *testing.T, scenario string) (*fake.FakeDynamicClient, *unstructured.Unstructured, *initializationActionCounts) {
	t.Helper()
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	desired := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "app", "namespace": "team"}, "data": map[string]any{"key": "value"}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	counts := &initializationActionCounts{}
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		counts.creates++
		if counts.creates != 1 {
			t.Fatal("initialization attempted recreation")
		}
		if scenario == "exists" {
			return true, nil, apierrors.NewAlreadyExists(gvr.GroupResource(), "app")
		}
		object := desired.DeepCopy()
		object.SetUID("created")
		object.SetResourceVersion("1")
		return true, object, nil
	})
	client.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		counts.gets++
		if scenario == "disappeared" {
			return true, nil, apierrors.NewNotFound(gvr.GroupResource(), "app")
		}
		object := desired.DeepCopy()
		object.SetUID("created")
		object.SetResourceVersion("1")
		if scenario == "replaced_first_get" || scenario == "replaced_second_get" && counts.gets > 1 {
			object.SetUID("replacement")
		}
		return true, object, nil
	})
	client.PrependReactor("patch", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		counts.patches++
		a, ok := action.(ktesting.PatchAction)
		if !ok {
			t.Fatalf("unexpected patch action %T", action)
		}
		typed, ok := action.(interface{ GetPatchOptions() metav1.PatchOptions })
		if !ok {
			t.Fatalf("missing patch options %T", action)
		}
		options := typed.GetPatchOptions()
		if options.FieldManager != "test-init" || options.Force == nil || *options.Force {
			t.Fatal("ownership handoff changed manager or forced conflicts")
		}
		if a.GetPatchType() != types.ApplyPatchType {
			t.Fatal("unexpected patch")
		}
		if scenario == "foreign_conflict" {
			return true, nil, apierrors.NewConflict(gvr.GroupResource(), "app", fmt.Errorf("foreign manager owns data"))
		}
		var object unstructured.Unstructured
		if err := json.Unmarshal(a.GetPatch(), &object.Object); err != nil {
			t.Fatal(err)
		}
		if object.GetUID() != "created" || object.GetResourceVersion() != "1" {
			t.Fatal("handoff lost identity")
		}
		return true, &object, nil
	})
	return client, desired, counts
}
