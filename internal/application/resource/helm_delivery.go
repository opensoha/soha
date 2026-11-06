package resource

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type HelmDeliveryBackend interface {
	PrepareHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error)
	ExecuteHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error)
}

type helmDeliveryPreparer interface {
	PrepareHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error)
}

func (h *Helm) AuthorizeHelmDelivery(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot, mutate bool) (string, error) {
	action := domainaccess.ActionView
	if mutate {
		action = domainaccess.ActionUpdate
		if snapshot.Operation == sohaapi.HelmDeliverySnapshotOperationInstall {
			action = domainaccess.ActionCreate
		}
	}
	connection, _, err := h.authorize(ctx, principal, snapshot.ClusterID, snapshot.Namespace, "HelmRelease", action)
	if err != nil {
		return "", err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		if mutate {
			if snapshot.Operation == sohaapi.HelmDeliverySnapshotOperationRollback {
				action = domainaccess.ActionRollback
			}
			if err := h.requireClusterWideHelmMutation(ctx, principal, connection, snapshot.Namespace, action); err != nil {
				return "", err
			}
			if err := h.requireAgentHelmDeliveryOptIn(ctx, connection, snapshot.Operation); err != nil {
				return "", err
			}
		}
		if helmrelease.IsInitialization(snapshot) {
			if !slices.Contains(connection.Summary.Capabilities, helmrelease.InitializationCapability) {
				return "", fmt.Errorf("%w: Agent does not support create-only Helm initialization", apperrors.ErrUnsupportedOperation)
			}
			return helmrelease.InitializationProviderPrefix + snapshot.ClusterID, nil
		}
		return "helm_agent." + snapshot.ClusterID, nil
	}
	if helmrelease.IsInitialization(snapshot) {
		return domaindelivery.HelmDirectInitializationProvider, nil
	}
	return "helm_direct", nil
}

// Older Agents did not enforce an action allowlist in the claimed-task runner.
func (h *Helm) requireAgentHelmDeliveryOptIn(ctx context.Context, connection domaincluster.Connection, operation sohaapi.HelmDeliverySnapshotOperation) error {
	var requiredAction string
	switch operation {
	case sohaapi.HelmDeliverySnapshotOperationInstall:
		requiredAction = "platform.helm_releases.install"
	case sohaapi.HelmDeliverySnapshotOperationUpgrade:
		requiredAction = "platform.helm_releases.values_update"
	case sohaapi.HelmDeliverySnapshotOperationRollback:
		requiredAction = "platform.helm_releases.rollback"
	default:
		return fmt.Errorf("%w: invalid Helm delivery operation", apperrors.ErrInvalidArgument)
	}
	client, err := h.helmAgentClient(connection)
	if err != nil {
		return err
	}
	reader, ok := client.(interface {
		GetActionChecks(context.Context) ([]contractresource.AgentActionCheck, error)
	})
	if !ok {
		return fmt.Errorf("%w: Agent Helm action diagnostics unavailable", apperrors.ErrClusterUnready)
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	checks, err := reader.GetActionChecks(ctx)
	if err != nil {
		return fmt.Errorf("%w: Agent Helm action diagnostics unavailable", apperrors.ErrClusterUnready)
	}
	if !slices.ContainsFunc(checks, func(check contractresource.AgentActionCheck) bool {
		return check.Action == requiredAction && check.Implementation == "supported" && check.Configured == "allowed"
	}) {
		return fmt.Errorf("%w: Agent Helm delivery action is not allowlisted", apperrors.ErrAccessDenied)
	}
	return nil
}

func (h *Helm) PrepareHelmDelivery(ctx context.Context, principal domainidentity.Principal, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	connection, _, err := h.authorize(ctx, principal, input.Snapshot.ClusterID, input.Snapshot.Namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return input, err
	}
	var backend helmDeliveryPreparer
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := h.helmAgentClient(connection)
		if err != nil {
			return input, err
		}
		backend, _ = client.(helmDeliveryPreparer)
	} else {
		backend, _ = h.direct.(HelmDeliveryBackend)
	}
	if backend == nil {
		return input, fmt.Errorf("%w: this runtime does not support application Helm delivery", apperrors.ErrClusterUnready)
	}
	return backend.PrepareHelmDelivery(ctx, input)
}

func (h *Helm) ExecuteHelmDelivery(ctx context.Context, principal domainidentity.Principal, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	if _, err := h.AuthorizeHelmDelivery(ctx, principal, input.Snapshot, input.Action == sohaapi.Apply); err != nil {
		return sohaapi.HelmExecutionTaskResult{Stopped: true}, err
	}
	connection, _, err := h.authorize(ctx, principal, input.Snapshot.ClusterID, input.Snapshot.Namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return sohaapi.HelmExecutionTaskResult{Stopped: true}, err
	}
	var backend HelmDeliveryBackend
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		if input.Action == sohaapi.Apply {
			return sohaapi.HelmExecutionTaskResult{}, fmt.Errorf("%w: Agent Helm mutations require a claimed task", apperrors.ErrInvalidArgument)
		}
		client, err := h.helmAgentClient(connection)
		if err != nil {
			return sohaapi.HelmExecutionTaskResult{}, err
		}
		backend, _ = client.(HelmDeliveryBackend)
	} else {
		backend, _ = h.direct.(HelmDeliveryBackend)
	}
	if backend == nil {
		return sohaapi.HelmExecutionTaskResult{}, apperrors.ErrClusterUnready
	}
	return backend.ExecuteHelmDelivery(ctx, input)
}
