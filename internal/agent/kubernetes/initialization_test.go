package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
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
	executor := &Client{dynamic: client}
	result, err := executor.applyManifest(t.Context(), mapper, payload)
	if err != nil || result.InitializationPlanID != "first" || len(result.Inventory) != 1 || result.Inventory[0].UID != "created" {
		t.Fatalf("initial create receipt: %+v %v", result, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "create" {
			t.Fatalf("unsafe first write: %s", action.GetVerb())
		}
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
