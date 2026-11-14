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
	"k8s.io/client-go/util/retry"
)

// ManifestAgentProviderPrefix prevents legacy runners from claiming operations
// that require frozen controller resources and ownership-fenced writes.
const ManifestAgentProviderPrefix = "manifest_agent_v3."
const ManifestAgentCapability = "manifest.execution.v3"

var ErrResourceOwnership = errors.New("resource mutation is blocked by ownership")

// ApplyManifest serializes the ownership check with the write. Creation uses
// create semantics so a concurrent external owner cannot be silently adopted.
func ApplyManifest(ctx context.Context, client dynamic.ResourceInterface, desired *unstructured.Unstructured, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	if IsArgoApplication(desired) {
		return nil, fmt.Errorf("%w: Argo CD Applications require frozen GitOps execution", ErrResourceOwnership)
	}
	if IsArgoRollout(desired) || desired.GetAnnotations()[RolloutOwnerAnnotation] != "" {
		return nil, fmt.Errorf("%w: progressive resources require frozen rollout execution", ErrResourceOwnership)
	}
	live, err := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := client.Create(ctx, desired, metav1.CreateOptions{FieldManager: options.FieldManager, DryRun: options.DryRun, FieldValidation: options.FieldValidation})
		if createErr != nil || len(options.DryRun) != 0 {
			return created, createErr
		}
		live, err = created, nil
	}
	if err != nil {
		return nil, err
	}
	expectedUID := live.GetUID()
	var applied *unstructured.Unstructured
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, readErr := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
		if readErr != nil {
			return readErr
		}
		if current.GetUID() != expectedUID {
			return fmt.Errorf("resource was replaced during apply")
		}
		if ownerErr := ValidateDirectManifestOwner(current); ownerErr != nil {
			return ownerErr
		}
		applied, readErr = applyObservedManifest(ctx, client, desired, current, options)
		return readErr
	})
	return applied, err
}

func applyObservedManifest(ctx context.Context, client dynamic.ResourceInterface, desired, live *unstructured.Unstructured, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	if len(options.DryRun) == 0 {
		// Native migration moves only this manager's Create/Update ownership to
		// Apply and guards resourceVersion; other field owners stay intact.
		upgrade, err := csaupgrade.UpgradeManagedFieldsPatch(live, sets.New(options.FieldManager), options.FieldManager)
		if err != nil {
			return nil, err
		}
		if len(upgrade) != 0 {
			live, err = client.Patch(ctx, desired.GetName(), types.JSONPatchType, upgrade, metav1.PatchOptions{FieldManager: options.FieldManager})
			if err != nil {
				return nil, err
			}
		}
	}
	return patchOwnedManifest(ctx, client, desired, live, options)
}

func patchOwnedManifest(ctx context.Context, client dynamic.ResourceInterface, desired, live *unstructured.Unstructured, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	write := desired.DeepCopy()
	write.SetResourceVersion(live.GetResourceVersion())
	write.SetUID(live.GetUID())
	body, err := json.Marshal(write.Object)
	if err != nil {
		return nil, err
	}
	return client.Patch(ctx, desired.GetName(), types.ApplyPatchType, body, options)
}

// ValidateDirectManifestOwner keeps direct SSA from becoming a second writer
// for a controller's child, a Helm release or a registered GitOps resource.
func ValidateDirectManifestOwner(object metav1.Object) error {
	if object == nil {
		return nil
	}
	if object.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%w: resource is being deleted; wait for finalizers to finish", ErrResourceOwnership)
	}
	return validateExternalManifestOwner(object)
}

func validateExternalManifestOwner(object metav1.Object) error {
	if resource, ok := object.(*unstructured.Unstructured); ok && IsArgoRollout(resource) {
		return fmt.Errorf("%w: Argo Rollouts require frozen progressive execution", ErrResourceOwnership)
	}
	if object.GetAnnotations()[RolloutOwnerAnnotation] != "" {
		return fmt.Errorf("%w: resource belongs to a progressive rollout", ErrResourceOwnership)
	}
	if resource, ok := object.(*unstructured.Unstructured); ok && IsArgoApplication(resource) {
		return fmt.Errorf("%w: Argo CD Applications require frozen GitOps execution", ErrResourceOwnership)
	}
	if metav1.GetControllerOf(object) != nil {
		return fmt.Errorf("%w: resource is managed by a controller; update its owning resource", ErrResourceOwnership)
	}
	labels, annotations := object.GetLabels(), object.GetAnnotations()
	if labels["app.kubernetes.io/managed-by"] == "Helm" || annotations["meta.helm.sh/release-name"] != "" ||
		labels["argocd.argoproj.io/instance"] != "" || annotations["argocd.argoproj.io/tracking-id"] != "" ||
		labels["kustomize.toolkit.fluxcd.io/name"] != "" || labels["helm.toolkit.fluxcd.io/name"] != "" {
		return fmt.Errorf("%w: resource has an external delivery owner", ErrResourceOwnership)
	}
	for _, field := range object.GetManagedFields() {
		if strings.HasPrefix(field.Manager, "argocd") || field.Manager == "kustomize-controller" || field.Manager == "helm-controller" {
			return fmt.Errorf("%w: resource is managed by a GitOps controller", ErrResourceOwnership)
		}
	}
	return nil
}

// UpdateManifest preserves the user's optimistic lock and refuses updates to
// externally managed or deleting roots, including attempts to remove finalizers.
func UpdateManifest(ctx context.Context, client dynamic.ResourceInterface, desired *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	live, err := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if err := ValidateDirectManifestOwner(live); err != nil {
		return nil, err
	}
	write := desired.DeepCopy()
	if write.GetUID() == "" {
		write.SetUID(live.GetUID())
	}
	if write.GetResourceVersion() == "" {
		write.SetResourceVersion(live.GetResourceVersion())
	}
	return client.Update(ctx, write, metav1.UpdateOptions{})
}

// DeleteManifest requests deletion of the observed root. A nil error confirms
// acceptance only; finalizers are never removed and completion must be observed.
func DeleteManifest(ctx context.Context, client dynamic.ResourceInterface, name, expectedUID string) error {
	live, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	uid, version := live.GetUID(), live.GetResourceVersion()
	if uid == "" || version == "" || expectedUID != "" && string(uid) != expectedUID {
		return apierrors.NewConflict(schema.GroupResource{Resource: live.GetKind()}, name, fmt.Errorf("resource identity changed; refresh before deleting"))
	}
	if err := validateExternalManifestOwner(live); err != nil {
		return err
	}
	if live.GetDeletionTimestamp() != nil {
		return nil
	}
	options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}
	if live.GetAPIVersion() == "workloads.soha.io/v1alpha1" && live.GetKind() == "WorkloadCronJob" {
		foreground := metav1.DeletePropagationForeground
		options.PropagationPolicy = &foreground
	}
	return client.Delete(ctx, name, options)
}
