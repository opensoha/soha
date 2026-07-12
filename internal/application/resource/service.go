package resource

import (
	"context"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type ConnectionResolver interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type MonitoringSettingsResolver interface {
	ResolveMonitoringSettings(context.Context) (domainsettings.MonitoringSettings, error)
}

type RuntimePermissionAuthorizer interface {
	Authorize(context.Context, domainidentity.Principal, string) error
}

type Service struct {
	workloads        *Workloads
	configuration    *Configuration
	network          *Network
	storage          *Storage
	rbac             *RBAC
	helm             *Helm
	inventory        *Inventory
	customResources  *CustomResources
	genericResources *GenericResources
	events           *Events
	portForwards     *PortForwards
	runtime          *Runtime
}

type Dependencies struct {
	Clusters            ClusterMetadataProvider
	Agents              AgentClients
	Connections         ConnectionResolver
	Authorizer          domainaccess.Authorizer
	Permissions         RuntimePermissionAuthorizer
	Audit               AuditRecorder
	Operations          OperationRecorder
	Settings            MonitoringSettingsResolver
	PortForwards        PortForwardRepository
	DirectEvents        DirectEventReader
	DirectCustom        DirectCustomResource
	DirectConfiguration DirectConfiguration
	DirectGeneric       DirectGenericResource
	DirectGateway       DirectGatewayReader
	DirectHelm          DirectHelm
	DirectInventory     DirectInventory
	DirectNetwork       DirectNetworkReader
	DirectPods          DirectPods
	DirectRBAC          DirectRBACReader
	DirectStorage       DirectStorageReader
	DirectTunnel        DirectPortForwardStarter
	DirectWorkloads     DirectWorkloads
}

type crdResourceDefinition struct {
	CRDName    string
	Kind       string
	Group      string
	Version    string
	Resource   string
	Namespaced bool
}

func New(deps Dependencies) *Service {
	return newServiceCapabilities(deps)
}
