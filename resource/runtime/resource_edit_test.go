package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

const testEditManager = "opensoha-resource-edit/v1"

func editFixture() *unstructured.Unstructured {
	item := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "sample", "namespace": "demo", "uid": "original", "resourceVersion": "1"}, "data": map[string]any{"phase": "created"}}}
	item.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: testEditManager, Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: metav1.NewFieldsV1(`{"f:data":{"f:phase":{}}}`)}})
	return item
}

func editConflict(manager string) error {
	return apierrors.NewApplyConflict([]metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict, Field: ".data.phase", Message: fmt.Sprintf("conflict with %q using v1", manager)}}, "field conflict")
}

func TestBindResourceEditIdentity(t *testing.T) {
	resource := schema.GroupResource{Resource: "configmaps"}
	current := editFixture()
	for _, tc := range []struct {
		name, uid, version string
		strict             bool
		wantError          func(error) bool
	}{
		{name: "legacy name only"},
		{name: "observed current object", uid: "original", version: "1", strict: true},
		{name: "missing observed identity", uid: "original", strict: true, wantError: apierrors.IsBadRequest},
		{name: "same name replacement", uid: "deleted", version: "1", strict: true, wantError: apierrors.IsConflict},
		{name: "same UID newer version", uid: "original", version: "0", strict: true, wantError: apierrors.IsConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := current.DeepCopy()
			desired.SetUID(types.UID(tc.uid))
			desired.SetResourceVersion(tc.version)
			err := BindResourceEditIdentity(current, desired, resource, tc.strict)
			if tc.wantError != nil {
				if !tc.wantError(err) || string(desired.GetUID()) != tc.uid || desired.GetResourceVersion() != tc.version {
					t.Fatalf("stale request unexpectedly rebound: uid=%q version=%q err=%v", desired.GetUID(), desired.GetResourceVersion(), err)
				}
				return
			}
			if err != nil || desired.GetUID() != current.GetUID() || desired.GetResourceVersion() != current.GetResourceVersion() {
				t.Fatalf("current identity not preserved: uid=%q version=%q err=%v", desired.GetUID(), desired.GetResourceVersion(), err)
			}
		})
	}
}

func TestApplyResourceEditMigratesOnlyItsOwnCreationFields(t *testing.T) {
	for _, test := range []struct {
		name                                                     string
		dryRun, foreign, race, retryFail, probePass, portInvalid bool
	}{
		{name: "write"}, {name: "dry-run", dryRun: true}, {name: "successful-ssa-preview", dryRun: true, probePass: true}, {name: "port-list-preview", dryRun: true, portInvalid: true},
		{name: "external", foreign: true}, {name: "external-dry-run", foreign: true, dryRun: true},
		{name: "concurrent-migration", race: true}, {name: "concurrent-retry", retryFail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			live := editFixture()
			if test.portInvalid {
				live.SetKind("Service")
			}
			if test.foreign {
				owners := live.GetManagedFields()
				owners = append(owners, metav1.ManagedFieldsEntry{Manager: "helm", Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: metav1.NewFieldsV1(`{"f:data":{"f:external":{}}}`)})
				live.SetManagedFields(owners)
			}
			desired := live.DeepCopy()
			desired.SetManagedFields(nil)
			desired.Object["data"] = map[string]any{"phase": "updated"}
			client := dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), live)
			applyCalls, migrations, updates := 0, 0, 0
			client.PrependReactor("patch", "configmaps", func(action clienttesting.Action) (bool, k8sruntime.Object, error) {
				patch, ok := action.(clienttesting.PatchActionImpl)
				if !ok {
					t.Fatalf("unexpected action %T", action)
				}
				if patch.GetPatchType() == types.JSONPatchType {
					migrations++
					assertEditMigration(t, patch.GetPatch(), test.foreign)
					if test.race {
						return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "sample", errors.New("concurrent update"))
					}
					moved := live.DeepCopy()
					moved.SetResourceVersion("2")
					return true, moved, nil
				}
				applyCalls++
				options := patch.GetPatchOptions()
				if options.Force == nil || *options.Force || options.FieldManager != testEditManager || (len(options.DryRun) > 0) != test.dryRun {
					t.Fatalf("unsafe apply options: %+v", options)
				}
				if test.probePass {
					return true, desired, nil
				}
				if test.portInvalid {
					return true, nil, apierrors.NewInvalid(schema.GroupKind{Kind: "Service"}, "sample", field.ErrorList{field.Required(field.NewPath("spec", "ports").Index(0).Child("name"), "")})
				}
				if test.dryRun || test.foreign {
					manager := testEditManager
					if test.foreign {
						manager = "helm"
					}
					return true, nil, editConflict(manager)
				}
				if test.retryFail {
					return true, nil, editConflict("external-controller")
				}
				var body unstructured.Unstructured
				if err := json.Unmarshal(patch.GetPatch(), &body); err != nil || body.GetResourceVersion() != "2" || body.GetUID() != live.GetUID() {
					t.Fatalf("retry lost observed identity: %s %v", patch.GetPatch(), err)
				}
				return true, &body, nil
			})
			client.PrependReactor("update", "configmaps", func(action clienttesting.Action) (bool, k8sruntime.Object, error) {
				update, ok := action.(clienttesting.UpdateActionImpl)
				if !ok {
					t.Fatalf("unexpected update %T", action)
				}
				options := update.GetUpdateOptions()
				if !test.dryRun || len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll || options.FieldManager != testEditManager {
					t.Fatalf("migration validation wrote live data: %+v", options)
				}
				updates++
				return true, update.GetObject(), nil
			})
			resource := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("demo")
			_, err := ApplyResourceEdit(context.Background(), resource, live, desired, testEditManager, test.dryRun)
			if (err != nil) != (test.foreign || test.race || test.retryFail) {
				t.Fatalf("result: %v", err)
			}
			wantMigrations, wantUpdates, wantApply := 1, 0, 1
			if test.foreign {
				wantMigrations, wantApply = 1, 1
			}
			if test.dryRun {
				wantMigrations, wantApply = 0, 1
				if !test.foreign && !test.probePass {
					wantUpdates = 1
				}
			}
			if test.race {
				wantApply = 0
			}
			if migrations != wantMigrations || updates != wantUpdates || applyCalls != wantApply || desired.GetResourceVersion() != "1" {
				t.Fatalf("apply=%d migrate=%d validate=%d; caller RV=%s", applyCalls, migrations, updates, desired.GetResourceVersion())
			}
		})
	}
}

func assertEditMigration(t *testing.T, payload []byte, foreign bool) {
	t.Helper()
	var ops []struct {
		Op, Path string
		Value    json.RawMessage
	}
	if err := json.Unmarshal(payload, &ops); err != nil || len(ops) != 3 {
		t.Fatalf("invalid migration: %s %v", payload, err)
	}
	if ops[0].Op != "test" || ops[0].Path != "/metadata/uid" || string(ops[0].Value) != `"original"` || ops[2].Path != "/metadata/resourceVersion" || string(ops[2].Value) != `"1"` {
		t.Fatalf("migration lacks UID/RV protection: %s", payload)
	}
	var owners []metav1.ManagedFieldsEntry
	if err := json.Unmarshal(ops[1].Value, &owners); err != nil || len(owners) < 1 || owners[0].Manager != testEditManager || owners[0].Operation != metav1.ManagedFieldsOperationApply {
		t.Fatalf("unexpected ownership migration: %s %v", payload, err)
	}
	if foreign && (len(owners) != 2 || owners[1].Manager != "helm" || owners[1].Operation != metav1.ManagedFieldsOperationApply || owners[1].FieldsV1.GetRawString() != `{"f:data":{"f:external":{}}}`) {
		t.Fatal("migration changed external ownership")
	}
}

func TestResourceEditMigrationFailsClosed(t *testing.T) {
	for _, missing := range []string{"uid", "resourceVersion"} {
		live := editFixture()
		unstructured.RemoveNestedField(live.Object, "metadata", missing)
		if patch, err := resourceEditMigrationPatch(live, testEditManager); err != nil || patch != nil {
			t.Fatalf("migrated without %s: %s %v", missing, patch, err)
		}
	}
	for _, manager := range []string{"helm", testEditManager + "-other", testEditManager + `" using v1`} {
		if onlyResourceEditConflicts(editFixture(), editConflict(manager), testEditManager) {
			t.Fatalf("misidentified foreign manager %q", manager)
		}
	}
	for _, entry := range []metav1.ManagedFieldsEntry{
		{Manager: testEditManager, Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1beta1"},
		{Manager: testEditManager, Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1beta1"},
	} {
		live := editFixture()
		live.SetManagedFields(append(live.GetManagedFields(), entry))
		if patch, err := resourceEditMigrationPatch(live, testEditManager); err != nil || patch != nil {
			t.Fatalf("discarded another version's ownership: %s %v", patch, err)
		}
	}
	if onlyResourceEditConflicts(editFixture(), apierrors.NewApplyConflict(nil, "unknown conflict"), testEditManager) {
		t.Fatal("accepted incomplete conflict evidence")
	}
}
