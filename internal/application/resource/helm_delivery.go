package resource

import (
	"context"
	"fmt"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
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
		return "helm_agent." + snapshot.ClusterID, nil
	}
	return "helm_direct", nil
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
