package resource

import (
	"context"
	"fmt"

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
