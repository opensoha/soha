package resourcebackend

import (
	"errors"
	"fmt"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
)

type Clusters struct {
	manager *k8sinfra.Manager
}

func NewClusters(manager *k8sinfra.Manager) *Clusters {
	return &Clusters{manager: manager}
}

func (c *Clusters) Metadata(clusterID string) (domaincluster.Summary, error) {
	return c.manager.Metadata(clusterID)
}

type Cache struct {
	*informerinfra.Service
}

func NewCache(cache *informerinfra.Service) *Cache {
	return &Cache{Service: cache}
}

func (*Cache) CacheUnavailable(err error) bool {
	return errors.Is(err, informerinfra.ErrCacheNotReady)
}

func NewAgentClients(registry *agentinfra.Registry) appresource.AgentClients {
	return appresource.AgentClients{
		Workloads:       agentFactory[appresource.WorkloadAgent](registry),
		Configuration:   agentFactory[appresource.ConfigurationAgent](registry),
		Network:         agentFactory[appresource.NetworkAgent](registry),
		Storage:         agentFactory[appresource.StorageAgent](registry),
		RBAC:            agentFactory[appresource.RBACAgent](registry),
		Helm:            agentFactory[appresource.HelmAgent](registry),
		Inventory:       agentFactory[appresource.InventoryAgent](registry),
		CustomResources: agentFactory[appresource.CustomResourceAgent](registry),
		Generic:         agentFactory[appresource.GenericResourceAgent](registry),
		Events:          agentFactory[appresource.EventAgent](registry),
		PortForwards:    agentFactory[appresource.PortForwardAgent](registry),
	}
}

func agentFactory[T any](registry *agentinfra.Registry) appresource.AgentClientFactory[T] {
	return func(connection domaincluster.Connection) (T, error) {
		var zero T
		if registry == nil {
			return zero, fmt.Errorf("agent registry is not configured")
		}
		client, err := registry.ClientFor(connection)
		if err != nil {
			return zero, err
		}
		typed, ok := any(client).(T)
		if !ok {
			return zero, fmt.Errorf("agent client does not satisfy requested resource capability")
		}
		return typed, nil
	}
}

var (
	_ appresource.ClusterMetadataProvider = (*Clusters)(nil)
	_ appresource.WorkloadAgent           = (*agentinfra.Client)(nil)
	_ appresource.ConfigurationAgent      = (*agentinfra.Client)(nil)
	_ appresource.NetworkAgent            = (*agentinfra.Client)(nil)
	_ appresource.StorageAgent            = (*agentinfra.Client)(nil)
	_ appresource.RBACAgent               = (*agentinfra.Client)(nil)
	_ appresource.HelmAgent               = (*agentinfra.Client)(nil)
	_ appresource.InventoryAgent          = (*agentinfra.Client)(nil)
	_ appresource.CustomResourceAgent     = (*agentinfra.Client)(nil)
	_ appresource.GenericResourceAgent    = (*agentinfra.Client)(nil)
	_ appresource.EventAgent              = (*agentinfra.Client)(nil)
	_ appresource.PortForwardAgent        = (*agentinfra.Client)(nil)
)
