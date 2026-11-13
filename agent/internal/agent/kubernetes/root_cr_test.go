package kubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

type rootCRTestMapper struct{ gvr schema.GroupVersionResource }

func (m rootCRTestMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return &meta.RESTMapping{Resource: m.gvr, Scope: meta.RESTScopeNamespace}, nil
}
func TestGenericRootCRRuntimeWritesOnlyItsUIDAndKeepsHealthUnknown(t *testing.T) {
	for _, mode := range []string{"preflight", "apply", "observe", "replaced", "force", "children", "repair"} {
		t.Run(mode, func(t *testing.T) {
			payload := rootRuntimePayload(mode)
			err := validateRootManifestPayload(payload)
			if mode == "force" || mode == "children" || mode == "repair" {
				if err == nil {
					t.Fatal("unsafe runtime task accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			client, mapper, writes := rootRuntimeClient(t, payload.RootResource, mode)
			var result sohaapi.ManifestExecutionTaskResult
			service := &Client{dynamic: client}
			switch payload.Action {
			case "preflight":
				result, err = service.preflightManifest(t.Context(), mapper, payload)
			case "observe":
				result, err = service.observeManifest(t.Context(), mapper, payload)
			default:
				result, err = service.applyManifest(t.Context(), mapper, payload)
			}
			assertRootRuntimeResult(t, mode, result, err, *writes)
		})
	}
}

func rootRuntimePayload(mode string) sohaapi.ManifestExecutionTaskPayload {
	root := &sohaapi.ManifestRootResource{APIVersion: "unknown.example/v1", Kind: "Database", Namespace: "team", Name: "db", UID: "root-uid", Generation: 2, CrdName: "databases.unknown.example", TargetID: "target", DeliveryPlanID: "plan"}
	content := `{"apiVersion":"unknown.example/v1","kind":"Database","metadata":{"name":"db","namespace":"team"},"spec":{"password":"new-synthetic-secret"}}`
	sum := sha256.Sum256([]byte(content))
	document := sohaapi.ManifestRenderedDocument{APIVersion: root.APIVersion, Kind: root.Kind, Namespace: "team", Name: "db", Content: content, ContentDigest: hex.EncodeToString(sum[:])}
	action := mode
	if mode != "preflight" && mode != "observe" && mode != "repair" {
		action = "apply"
	}
	payload := sohaapi.ManifestExecutionTaskPayload{RootResource: root, Action: sohaapi.ManifestTaskAction(action), ClusterID: "cluster", Namespace: "team", Generation: 1, Documents: []sohaapi.ManifestRenderedDocument{document}}
	if mode == "force" {
		payload.ForceConflicts = true
	}
	if mode == "children" {
		payload.Documents = append(payload.Documents, document)
	}
	return payload
}

func rootRuntimeClient(t *testing.T, root *sohaapi.ManifestRootResource, mode string) (*fake.FakeDynamicClient, rootCRTestMapper, *int) {
	t.Helper()
	live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": root.APIVersion, "kind": root.Kind, "metadata": map[string]any{"name": "db", "namespace": "team", "uid": "root-uid", "resourceVersion": "12", "generation": int64(2)}, "spec": map[string]any{"password": "old-synthetic-secret"}, "status": map[string]any{"observedGeneration": int64(2), "conditions": []any{map[string]any{"type": "Ready", "status": "True", "observedGeneration": int64(2)}}}}}
	if mode == "replaced" {
		live.SetUID("replacement")
	}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
	mapper := rootCRTestMapper{gvr: schema.GroupVersionResource{Group: "unknown.example", Version: "v1", Resource: "databases"}}
	writes := new(int)
	client.PrependReactor("update", "databases", func(call ktesting.Action) (bool, runtime.Object, error) {
		*writes++
		update, ok := call.(ktesting.UpdateAction)
		if !ok {
			t.Fatalf("expected update action, got %T", call)
		}
		result, ok := update.GetObject().(*unstructured.Unstructured)
		if !ok {
			t.Fatalf("expected unstructured root, got %T", update.GetObject())
		}
		if string(result.GetUID()) != "root-uid" || result.GetResourceVersion() != "12" || !reflect.DeepEqual(result.Object["status"], live.Object["status"]) || !reflect.DeepEqual(result.Object["metadata"], live.Object["metadata"]) {
			t.Fatalf("unsafe root update: %v", result.Object)
		}
		return true, result, nil
	})
	return client, mapper, writes
}

func assertRootRuntimeResult(t *testing.T, mode string, result sohaapi.ManifestExecutionTaskResult, err error, writes int) {
	t.Helper()
	if mode == "replaced" {
		if err == nil || writes != 0 {
			t.Fatal("replaced root was written")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if mode == "observe" {
		if writes != 0 || len(result.Inventory) != 1 || result.Inventory[0].Health != "unknown" {
			t.Fatalf("observation conflated with readiness: %+v", result)
		}
		raw, _ := json.Marshal(result)
		if !result.Drift.Drifted || bytes.Contains(raw, []byte("synthetic-secret")) {
			t.Fatalf("drift lost or configuration leaked: %s", raw)
		}
	} else if mode == "apply" && (writes != 1 || result.Inventory[0].Health != "unknown") {
		t.Fatalf("apply result: %+v", result)
	}
}
