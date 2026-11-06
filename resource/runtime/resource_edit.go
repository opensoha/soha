package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/csaupgrade"
	"k8s.io/utils/ptr"
)

// BindResourceEditIdentity keeps legacy name-based edits and validates observed edits
// before attaching the live object's optimistic concurrency preconditions.
func BindResourceEditIdentity(current, desired *unstructured.Unstructured, resource schema.GroupResource, requireObserved bool) error {
	if requireObserved {
		if desired.GetUID() == "" || desired.GetResourceVersion() == "" {
			return apierrors.NewBadRequest("observed YAML must include metadata.uid and metadata.resourceVersion")
		}
		if desired.GetUID() != current.GetUID() || desired.GetResourceVersion() != current.GetResourceVersion() {
			return apierrors.NewConflict(resource, current.GetName(), errors.New("resource changed since the YAML was read"))
		}
	}
	desired.SetUID(current.GetUID())
	desired.SetResourceVersion(current.GetResourceVersion())
	return nil
}

// IsResourceEditOwnershipConflict separates a dry-run ownership report from stale identity.
func IsResourceEditOwnershipConflict(err error) bool {
	return apierrors.HasStatusCause(err, metav1.CauseTypeFieldManagerConflict)
}

// ApplyResourceEdit preserves SSA conflicts while migrating this editor's own Create/Update fields.
// Dry-run migration validates a complete manifest with Update; it never changes live ownership.
func ApplyResourceEdit(ctx context.Context, resource dynamic.ResourceInterface, current, desired *unstructured.Unstructured, manager string, dryRun bool) (*unstructured.Unstructured, error) {
	patch, err := json.Marshal(desired.Object)
	if err != nil {
		return nil, err
	}
	options := metav1.PatchOptions{FieldManager: manager, Force: ptr.To(false)}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	migration, err := resourceEditMigrationPatch(current, manager)
	if err != nil {
		return nil, err
	}
	if len(migration) == 0 {
		return resource.Patch(ctx, desired.GetName(), types.ApplyPatchType, patch, options)
	}
	if dryRun {
		// Probe foreign ownership first; an initial list merge can also fail validation
		// until this creator's Update ownership is migrated.
		probed, probeErr := resource.Patch(ctx, desired.GetName(), types.ApplyPatchType, patch, options)
		if probeErr == nil {
			return probed, nil
		}
		if !resourceEditPortMergeInvalid(current, probeErr) && !onlyResourceEditConflicts(current, probeErr, manager) {
			return nil, probeErr
		}
		return resource.Update(ctx, desired, metav1.UpdateOptions{FieldManager: manager, DryRun: []string{metav1.DryRunAll}})
	}
	migrated, err := resource.Patch(ctx, desired.GetName(), types.JSONPatchType, migration, metav1.PatchOptions{})
	if err != nil {
		return nil, err
	}
	desired = desired.DeepCopy()
	desired.SetResourceVersion(migrated.GetResourceVersion())
	patch, err = json.Marshal(desired.Object)
	if err != nil {
		return nil, err
	}
	return resource.Patch(ctx, desired.GetName(), types.ApplyPatchType, patch, options)
}

func onlyResourceEditConflicts(current *unstructured.Unstructured, conflict error, manager string) bool {
	var status apierrors.APIStatus
	if !apierrors.IsConflict(conflict) || !errors.As(conflict, &status) {
		return false
	}
	details := status.Status().Details
	if details == nil || len(details.Causes) == 0 {
		return false
	}
	prefix := fmt.Sprintf("conflict with %q using %s", manager, current.GetAPIVersion())
	for _, cause := range details.Causes {
		if cause.Type != metav1.CauseTypeFieldManagerConflict || cause.Field == "" || (cause.Message != prefix && !strings.HasPrefix(cause.Message, prefix+" at ")) {
			return false
		}
	}
	return true
}

func resourceEditMigrationPatch(current *unstructured.Unstructured, manager string) ([]byte, error) {
	if manager == "" || current.GetUID() == "" || current.GetResourceVersion() == "" {
		return nil, nil
	}
	updates := 0
	for _, entry := range current.GetManagedFields() {
		if entry.Manager != manager || entry.Subresource != "" {
			continue
		}
		// client-go's upgrader discards other Update versions; fail closed instead.
		if entry.APIVersion != current.GetAPIVersion() {
			return nil, nil
		}
		if entry.Operation == metav1.ManagedFieldsOperationUpdate {
			updates++
		}
	}
	if updates != 1 {
		return nil, nil
	}
	patch, err := csaupgrade.UpgradeManagedFieldsPatch(current, sets.New(manager), manager)
	if err != nil || len(patch) == 0 {
		return patch, err
	}
	var operations []map[string]any
	if err := json.Unmarshal(patch, &operations); err != nil {
		return nil, err
	}
	operations = append([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": string(current.GetUID())}}, operations...)
	return json.Marshal(operations)
}

func resourceEditPortMergeInvalid(current *unstructured.Unstructured, err error) bool {
	var status apierrors.APIStatus
	if current.GetKind() != "Service" || !apierrors.IsInvalid(err) || !errors.As(err, &status) {
		return false
	}
	details := status.Status().Details
	if details == nil || len(details.Causes) == 0 {
		return false
	}
	for _, cause := range details.Causes {
		if cause.Type != metav1.CauseTypeFieldValueRequired || !strings.HasPrefix(cause.Field, "spec.ports[") || !strings.HasSuffix(cause.Field, "].name") {
			return false
		}
	}
	return true
}
