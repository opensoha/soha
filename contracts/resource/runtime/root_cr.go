package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

const RootCRAgentCapability = "manifest.root-cr.v1"
const RootCRAgentProviderPrefix = "manifest_root_cr_v1."

// ApplyRootCR updates only spec after an explicit publication handoff. It reuses
// the workbench optimistic update semantics and preserves runtime metadata/status.
// A retry may acknowledge the same desired spec already applied to the same UID.
func ApplyRootCR(ctx context.Context, client dynamic.ResourceInterface, desired *unstructured.Unstructured, root sohaapi.ManifestRootResource, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	if err := contractresource.ValidateRootCRDocument(desired.Object, root); err != nil {
		return nil, err
	}
	if options.Force != nil && *options.Force {
		return nil, fmt.Errorf("root CR cannot force field ownership")
	}
	var applied *unstructured.Unstructured
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		live, err := client.Get(ctx, root.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if string(live.GetUID()) != root.UID {
			return fmt.Errorf("root CR UID changed; create a new plan")
		}
		if err := validateRootCROwner(live); err != nil {
			return err
		}
		sameSpec := rootSpecEqual(live.Object["spec"], desired.Object["spec"])
		if live.GetGeneration() != root.Generation && !sameSpec {
			return fmt.Errorf("root CR configuration changed; create a new plan")
		}
		if sameSpec {
			applied = live
			return nil
		}
		write := live.DeepCopy()
		write.Object["spec"] = desired.DeepCopy().Object["spec"]
		applied, err = client.Update(ctx, write, metav1.UpdateOptions{FieldManager: options.FieldManager, DryRun: options.DryRun, FieldValidation: options.FieldValidation})
		return err
	})
	return applied, err
}

func validateRootCROwner(live *unstructured.Unstructured) error {
	if err := ValidateDirectManifestOwner(live); err != nil {
		return err
	}
	manager := strings.ToLower(live.GetLabels()["app.kubernetes.io/managed-by"])
	if len(live.GetOwnerReferences()) > 0 || (manager != "" && manager != "soha") {
		return fmt.Errorf("%w: root CR has an external owner", ErrResourceOwnership)
	}
	for key := range live.GetAnnotations() {
		if strings.HasPrefix(key, "meta.helm.sh/") || strings.HasPrefix(key, "argocd.argoproj.io/") {
			return fmt.Errorf("%w: root CR has an external publisher", ErrResourceOwnership)
		}
	}
	return nil
}

func rootSpecEqual(left, right any) bool {
	a, aErr := json.Marshal(left)
	b, bErr := json.Marshal(right)
	return aErr == nil && bErr == nil && bytes.Equal(a, b)
}
