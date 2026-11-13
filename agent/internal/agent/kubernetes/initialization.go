package kubernetes

import (
	"context"
	"fmt"

	resourceruntime "github.com/opensoha/soha/contracts/resource/runtime"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// initializedResource limits the SDK's native ownership handoff to the exact
// object returned by Create. Disappearance must never enter its create fallback.
type initializedResource struct {
	dynamic.ResourceInterface
	uid string
}

func (r initializedResource) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	live, err := r.ResourceInterface.Get(ctx, name, options, subresources...)
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("%w: initialized resource disappeared", resourceruntime.ErrResourceOwnership)
	}
	if err != nil {
		return nil, err
	}
	if err := resourceruntime.ValidateInitializedUID(live, r.uid); err != nil {
		return nil, err
	}
	return live, nil
}

func createInitializedManifest(ctx context.Context, client dynamic.ResourceInterface, desired *unstructured.Unstructured, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	created, err := resourceruntime.CreateManifest(ctx, client, desired, metav1.CreateOptions{FieldManager: options.FieldManager, DryRun: options.DryRun})
	if err != nil || len(options.DryRun) != 0 {
		return created, err
	}
	// Reuse the released SDK's self-manager migration and UID/RV guarded SSA.
	// No force, takeover, recreation or distinct rendering path is introduced.
	return resourceruntime.ApplyManifest(ctx, initializedResource{ResourceInterface: client, uid: string(created.GetUID())}, desired, options)
}
