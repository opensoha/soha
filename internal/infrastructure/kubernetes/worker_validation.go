package kubernetes

import (
	"fmt"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ValidateWorkerPoolKubernetesSpec checks Kubernetes syntax without a cluster connection.
func ValidateWorkerPoolKubernetesSpec(spec sohaapi.VirtualizationWorkerPoolSpec) error {
	for key, value := range spec.Labels {
		if len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 {
			return fmt.Errorf("%w: worker pool labels must use valid Kubernetes keys and values", apperrors.ErrInvalidArgument)
		}
	}
	for _, daemon := range spec.RequiredDaemonSets {
		if len(validation.IsDNS1123Label(daemon.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(daemon.Name)) != 0 {
			return fmt.Errorf("%w: worker pool requires valid daemon names and namespaces", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}
