package resource

import (
	"context"
	"fmt"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type clusterNamespaceLabelResolver struct {
	agent  AgentClientFactory[InventoryAgent]
	direct DirectInventory
}

func newClusterNamespaceLabelResolver(agent AgentClientFactory[InventoryAgent], direct DirectInventory) namespaceLabelResolver {
	if agent == nil && direct == nil {
		return nil
	}
	return &clusterNamespaceLabelResolver{agent: agent, direct: direct}
}

func (r *clusterNamespaceLabelResolver) Resolve(ctx context.Context, connection domaincluster.Connection, namespace string) (map[string]string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		if r.agent == nil {
			return nil, fmt.Errorf("%w: agent namespace inventory is unavailable", apperrors.ErrUnsupportedOperation)
		}
		client, err := r.agent(connection)
		if err != nil {
			return nil, err
		}
		items, err := client.ListNamespaces(ctx)
		if err != nil {
			return nil, err
		}
		return findNamespaceLabels(items, namespace)
	}
	if r.direct == nil {
		return nil, fmt.Errorf("%w: direct namespace inventory is unavailable", apperrors.ErrClusterUnready)
	}
	items, _, err := r.direct.ListNamespaces(ctx, connection.Summary.ID)
	if err != nil {
		return nil, err
	}
	return findNamespaceLabels(items, namespace)
}

func findNamespaceLabels(items []domainresource.NamespaceView, namespace string) (map[string]string, error) {
	for _, item := range items {
		if item.Name == namespace {
			return item.Labels, nil
		}
	}
	return nil, fmt.Errorf("%w: namespace not found", apperrors.ErrNotFound)
}
