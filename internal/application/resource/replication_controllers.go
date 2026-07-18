package resource

import (
	"context"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var replicationControllerListSpec = newConfigurationListSpec(
	"ReplicationController",
	"listed replicationcontrollers",
	ConfigurationAgent.ListReplicationControllers,
	DirectConfiguration.ListReplicationControllers,
	replicationControllerNamespace,
	replicationControllerActions,
	setReplicationControllerActions,
)

func (w *Workloads) ListReplicationControllers(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ReplicationControllerView, error) {
	return listConfigurationCapability(ctx, w.resourceAccess, principal, clusterID, namespace, w.configurationAgentClient, w.directConfiguration, replicationControllerListSpec)
}

func (w *Workloads) GetReplicationControllerDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ReplicationControllerDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ReplicationControllerDetailView]{
		kind: "ReplicationController", auditText: "viewed replicationcontroller detail",
		agent: func(client WorkloadAgent) (domainresource.ReplicationControllerDetailView, error) {
			return client.GetReplicationControllerDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.ReplicationControllerDetailView, string, error) {
			return liveWorkload(func() (domainresource.ReplicationControllerDetailView, error) {
				return w.direct.GetReplicationControllerDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.ReplicationControllerDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func replicationControllerNamespace(item domainresource.ReplicationControllerView) string {
	return item.Namespace
}

func replicationControllerActions(item domainresource.ReplicationControllerView) []string {
	return item.AllowedActions
}

func setReplicationControllerActions(item *domainresource.ReplicationControllerView, actions []string) {
	item.AllowedActions = actions
}

func (w *Workloads) directConfiguration() (DirectConfiguration, error) {
	if w.directConfig == nil {
		return nil, fmt.Errorf("%w: direct configuration adapter is not configured", apperrors.ErrClusterUnready)
	}
	return w.directConfig, nil
}
