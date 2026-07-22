package resource

import (
	"context"
	"net/http"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type podLister interface {
	ListPods(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error)
}

type resourceYAMLApplier interface {
	applyResourceYAML(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceYAMLView, error)
}

// Workloads owns workload operations and only its workload-specific collaborators.
type Workloads struct {
	*resourceAccess
	*metricsSupport
	agent         AgentClientFactory[WorkloadAgent]
	configuration AgentClientFactory[ConfigurationAgent]
	directPods    DirectPods
	directConfig  DirectConfiguration
	direct        DirectWorkloads
	network       DirectNetworkReader
	yaml          resourceYAMLApplier
}

type Configuration struct {
	*resourceAccess
	agent    AgentClientFactory[ConfigurationAgent]
	direct   DirectConfiguration
	generic  DirectGenericResource
	creation *ResourceCreation
}

type Network struct {
	*resourceAccess
	*metricsSupport
	agent         AgentClientFactory[NetworkAgent]
	directReader  DirectNetworkReader
	gatewayReader DirectGatewayReader
	configuration AgentClientFactory[ConfigurationAgent]
	directConfig  DirectConfiguration
	pods          podLister
}

type Storage struct {
	*resourceAccess
	agent  AgentClientFactory[StorageAgent]
	direct DirectStorageReader
}

type RBAC struct {
	*resourceAccess
	agent  AgentClientFactory[RBACAgent]
	direct DirectRBACReader
}

type Helm struct {
	*resourceAccess
	agent      AgentClientFactory[HelmAgent]
	direct     DirectHelm
	httpClient *http.Client
}

type Inventory struct {
	*resourceAccess
	*metricsSupport
	agent  AgentClientFactory[InventoryAgent]
	direct DirectInventory
	yaml   resourceYAMLApplier
}

type CustomResources struct {
	*resourceAccess
	agent  AgentClientFactory[CustomResourceAgent]
	direct DirectCustomResource
}

type GenericResources struct {
	*resourceAccess
	agent  AgentClientFactory[GenericResourceAgent]
	direct DirectGenericResource
}

type Events struct {
	*resourceAccess
	agent  AgentClientFactory[EventAgent]
	direct DirectEventReader
}

type PortForwards struct {
	*resourceAccess
	agent      AgentClientFactory[PortForwardAgent]
	direct     DirectPortForwardStarter
	repository PortForwardRepository
}

// Runtime composes the capabilities used by workflow, delivery, Copilot, and
// AI gateway runtime inspection without exposing the complete root service.
type Runtime struct {
	*Workloads
	*Network
	*Storage
	*Inventory
	*Events
}

// Workloads returns the workload capability.
func (s *Service) Workloads() *Workloads {
	return s.workloads
}

// Configuration returns the configuration capability.
func (s *Service) Configuration() *Configuration {
	return s.configuration
}

// Network returns the network capability.
func (s *Service) Network() *Network {
	return s.network
}

// Storage returns the storage capability.
func (s *Service) Storage() *Storage {
	return s.storage
}

// RBAC returns the access-control capability.
func (s *Service) RBAC() *RBAC {
	return s.rbac
}

// Helm returns the Helm capability.
func (s *Service) Helm() *Helm {
	return s.helm
}

// Inventory returns the namespace and node capability.
func (s *Service) Inventory() *Inventory {
	return s.inventory
}

// CustomResources returns the CRD capability.
func (s *Service) CustomResources() *CustomResources {
	return s.customResources
}

// GenericResources returns the dynamic YAML capability.
func (s *Service) GenericResources() *GenericResources {
	return s.genericResources
}

// Events returns the Kubernetes event capability.
func (s *Service) Events() *Events {
	return s.events
}

// PortForwards returns the port-forward capability.
func (s *Service) PortForwards() *PortForwards {
	return s.portForwards
}

// Runtime returns the restricted cross-domain runtime composition.
func (s *Service) Runtime() *Runtime {
	return s.runtime
}

func newServiceCapabilities(deps Dependencies) *Service {
	access := &resourceAccess{
		directClusters: deps.Clusters, resolver: deps.Connections, authorizer: deps.Authorizer,
		permissions: deps.Permissions, audit: deps.Audit, operations: deps.Operations,
		namespaceLabels: newClusterNamespaceLabelResolver(deps.Agents.Inventory, deps.DirectInventory),
	}
	metrics := &metricsSupport{
		resourceAccess: access,
		resolver:       deps.Connections,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
	}
	genericResources := &GenericResources{
		resourceAccess: access, agent: deps.Agents.Generic, direct: deps.DirectGeneric,
	}
	creation := &ResourceCreation{
		resourceAccess: access,
		direct:         deps.DirectResourceCreate,
		agent:          deps.Agents.ResourceCreation,
		risk:           NewHighRiskResourcePolicy(deps.Permissions),
		operations:     deps.CreationOperations,
		batches:        deps.CreationBatches,
	}
	workloads := &Workloads{
		resourceAccess: access, metricsSupport: metrics,
		agent:         deps.Agents.Workloads,
		configuration: deps.Agents.Configuration, directPods: deps.DirectPods, direct: deps.DirectWorkloads,
		directConfig: deps.DirectConfiguration, network: deps.DirectNetwork, yaml: genericResources,
	}
	network := &Network{
		resourceAccess: access, metricsSupport: metrics,
		agent: deps.Agents.Network, directReader: deps.DirectNetwork, gatewayReader: deps.DirectGateway,
		configuration: deps.Agents.Configuration, directConfig: deps.DirectConfiguration, pods: workloads,
	}
	inventory := &Inventory{
		resourceAccess: access, metricsSupport: metrics,
		agent: deps.Agents.Inventory, direct: deps.DirectInventory, yaml: genericResources,
	}
	service := &Service{
		workloads:        workloads,
		configuration:    &Configuration{resourceAccess: access, agent: deps.Agents.Configuration, direct: deps.DirectConfiguration, generic: deps.DirectGeneric, creation: creation},
		network:          network,
		storage:          &Storage{resourceAccess: access, agent: deps.Agents.Storage, direct: deps.DirectStorage},
		rbac:             &RBAC{resourceAccess: access, agent: deps.Agents.RBAC, direct: deps.DirectRBAC},
		helm:             &Helm{resourceAccess: access, agent: deps.Agents.Helm, direct: deps.DirectHelm, httpClient: &http.Client{Timeout: 10 * time.Second}},
		inventory:        inventory,
		customResources:  &CustomResources{resourceAccess: access, agent: deps.Agents.CustomResources, direct: deps.DirectCustom},
		genericResources: genericResources,
		events:           &Events{resourceAccess: access, agent: deps.Agents.Events, direct: deps.DirectEvents},
		portForwards:     &PortForwards{resourceAccess: access, agent: deps.Agents.PortForwards, direct: deps.DirectTunnel, repository: deps.PortForwards},
		creation:         creation,
	}
	service.runtime = &Runtime{
		Workloads: workloads, Network: network, Storage: service.storage,
		Inventory: inventory, Events: service.events,
	}
	return service
}
