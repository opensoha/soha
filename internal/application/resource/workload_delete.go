package resource

import (
	"context"
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

type observedWorkloadDeleteAgent interface {
	DeleteObservedWorkload(context.Context, string, string, string, string) error
}

func deleteObservedAgentWorkload(ctx context.Context, client any, namespace, kind, name, expectedUID string) error {
	guarded, ok := client.(observedWorkloadDeleteAgent)
	if !ok {
		return fmt.Errorf("%w: Agent backend does not support UID preconditions", apperrors.ErrUnsupportedOperation)
	}
	return wrapAgentResourceError(guarded.DeleteObservedWorkload(ctx, namespace, kind, name, expectedUID))
}

func deleteObservedDirectDeployment(ctx context.Context, backend DirectGenericResource, clusterID, namespace, name, expectedUID string) error {
	guarded, ok := backend.(interface {
		DeleteDeploymentWithExpectedUID(context.Context, string, string, string, string) error
	})
	if !ok {
		return fmt.Errorf("%w: direct Deployment backend does not support UID preconditions", apperrors.ErrUnsupportedOperation)
	}
	return guarded.DeleteDeploymentWithExpectedUID(ctx, clusterID, namespace, name, expectedUID)
}
