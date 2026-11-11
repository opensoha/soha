package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const ManifestInitializationCapability = "manifest.initialize.v1"
const ManifestInitializationProviderPrefix = "manifest_initialize_v1."

// ValidateManifestInitialization fences the new mode from legacy/controller writes.
// Callers still own current authorization, immutable Plan validation and admission.
func ValidateManifestInitialization(p sohaapi.ManifestExecutionTaskPayload) error {
	if p.InitializationPlanID == "" {
		return nil
	}
	if strings.TrimSpace(p.InitializationPlanID) != p.InitializationPlanID || len(p.InitializationPlanID) > 128 || p.ForceConflicts || p.RootResource != nil || p.RolloutControl != nil || len(p.GitOpsDocuments) != 0 {
		return fmt.Errorf("%w: invalid initialization boundary", ErrResourceOwnership)
	}
	if p.Action != sohaapi.ManifestTaskActionPreflight && p.Action != sohaapi.ManifestTaskActionApply && p.Action != sohaapi.ManifestTaskActionObserve {
		return fmt.Errorf("%w: initialization requires preflight, apply or observation", ErrResourceOwnership)
	}
	if IsRolloutTask(p) {
		return fmt.Errorf("%w: controller delivery cannot guarantee create-only initialization", ErrResourceOwnership)
	}
	for _, document := range p.Documents {
		if initializationController(document.APIVersion, document.Kind) {
			return fmt.Errorf("%w: controller delivery cannot guarantee create-only initialization", ErrResourceOwnership)
		}
	}
	return nil
}

func initializationController(apiVersion, kind string) bool {
	return strings.HasPrefix(apiVersion, "kustomize.toolkit.fluxcd.io/") || strings.HasPrefix(apiVersion, "helm.toolkit.fluxcd.io/") || strings.HasPrefix(apiVersion, "argoproj.io/") && (kind == "Application" || kind == "Rollout")
}

// CreateManifest never falls back to SSA or adopts a concurrent object. Kubernetes
// validates non-existence atomically for both dry-run and the actual POST.
func CreateManifest(ctx context.Context, client dynamic.ResourceInterface, desired *unstructured.Unstructured, options metav1.CreateOptions) (*unstructured.Unstructured, error) {
	if desired.GetUID() != "" || desired.GetResourceVersion() != "" || desired.GetGenerateName() != "" {
		return nil, fmt.Errorf("%w: initialization requires a new named resource", ErrResourceOwnership)
	}
	if initializationController(desired.GetAPIVersion(), desired.GetKind()) {
		return nil, fmt.Errorf("%w: controller delivery requires its existing publication path", ErrResourceOwnership)
	}
	if err := ValidateDirectManifestOwner(desired); err != nil {
		return nil, err
	}
	return client.Create(ctx, desired, options)
}

// ValidateInitializedUID prevents a healthy same-name replacement from confirming
// the baseline of a different object created by the approved initialization.
func ValidateInitializedUID(live metav1.Object, expectedUID string) error {
	if live == nil || expectedUID == "" || live.GetUID() == "" || string(live.GetUID()) != expectedUID {
		return fmt.Errorf("%w: initialized resource identity is missing or changed", ErrResourceOwnership)
	}
	return nil
}
