package runtime

import (
	"context"
	"fmt"

	contractresource "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func ValidateResourceMutationPreconditions(current *unstructured.Unstructured, expected contractresource.ResourceMutationPreconditions) error {
	if err := expected.Validate(); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if current == nil {
		return apierrors.NewBadRequest("resource identity is unavailable")
	}
	if string(current.GetUID()) != expected.ExpectedUID || current.GetResourceVersion() != expected.ExpectedResourceVersion {
		return apierrors.NewConflict(schema.GroupResource{Resource: current.GetKind()}, current.GetName(), fmt.Errorf("resource identity or version changed; refresh and review before retrying"))
	}
	return nil
}

func ValidateResourceEditPreconditions(current, desired *unstructured.Unstructured, expected contractresource.ResourceMutationPreconditions) error {
	if err := ValidateResourceMutationPreconditions(current, expected); err != nil {
		return err
	}
	if desired == nil {
		return apierrors.NewBadRequest("resource content is required")
	}
	for _, pair := range [][2]string{
		{desired.GetName(), current.GetName()}, {desired.GetNamespace(), current.GetNamespace()},
		{desired.GetKind(), current.GetKind()}, {desired.GetAPIVersion(), current.GetAPIVersion()},
		{string(desired.GetUID()), expected.ExpectedUID}, {desired.GetResourceVersion(), expected.ExpectedResourceVersion},
	} {
		if pair[0] != "" && pair[0] != pair[1] {
			return apierrors.NewBadRequest("YAML target or identity does not match the reviewed resource")
		}
	}
	// Conditional editing cannot be used as a finalizer-removal shortcut. SSA
	// omission can remove a field previously owned by this same editor, so every
	// existing finalizer must remain explicit in the reviewed manifest.
	for _, finalizer := range current.GetFinalizers() {
		found := false
		for _, retained := range desired.GetFinalizers() {
			if retained == finalizer {
				found = true
				break
			}
		}
		if !found {
			return apierrors.NewBadRequest("conditional YAML must retain every existing finalizer")
		}
	}
	return nil
}

func ResourceIdentityFrom(current *unstructured.Unstructured) contractresource.ResourceIdentity {
	if current == nil {
		return contractresource.ResourceIdentity{}
	}
	identity := contractresource.ResourceIdentity{
		APIVersion: current.GetAPIVersion(), Kind: current.GetKind(), Name: current.GetName(), Namespace: current.GetNamespace(),
		UID: string(current.GetUID()), ResourceVersion: current.GetResourceVersion(),
	}
	if timestamp := current.GetDeletionTimestamp(); timestamp != nil {
		value := timestamp.Time
		identity.DeletionTimestamp = &value
	}
	return identity
}

// DeleteResourceConditional preserves the existing explicit Pod deletion
// exception while retaining owner protection for other kinds. It never removes
// finalizers. Success acknowledges the delete request, not observed completion.
func DeleteResourceConditional(ctx context.Context, client dynamic.ResourceInterface, name string, expected contractresource.ResourceMutationPreconditions) error {
	if err := expected.Validate(); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	current, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := ValidateResourceMutationPreconditions(current, expected); err != nil {
		return err
	}
	if current.GetKind() != "Pod" {
		if err := validateExternalManifestOwner(current); err != nil {
			return err
		}
	}
	if current.GetDeletionTimestamp() != nil {
		return nil
	}
	uid, version := current.GetUID(), current.GetResourceVersion()
	options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}
	if current.GetAPIVersion() == "workloads.soha.io/v1alpha1" && current.GetKind() == "WorkloadCronJob" {
		foreground := metav1.DeletePropagationForeground
		options.PropagationPolicy = &foreground
	}
	return client.Delete(ctx, name, options)
}
