package handlers

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/gorilla/websocket"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmcp "github.com/opensoha/soha/internal/domain/mcp"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
)

type ClusterReader interface {
	List(context.Context) ([]domaincluster.Summary, error)
	ListAccessible(context.Context, domainidentity.Principal) ([]domaincluster.Summary, error)
	Describe(context.Context, domainidentity.Principal, string) (domaincluster.Detail, error)
	CapabilityMatrix(context.Context, domainidentity.Principal) ([]domaincluster.CapabilityMatrixEntry, error)
}

type ClusterEditor interface {
	Register(context.Context, domainidentity.Principal, domaincluster.RegisterInput) (domaincluster.Summary, error)
	Update(context.Context, domainidentity.Principal, string, domaincluster.UpdateInput) (domaincluster.Summary, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type ClusterService interface {
	ClusterReader
	ClusterEditor
}

type AuditService interface {
	ListAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) ([]domainaudit.Entry, error)
	SummaryAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) (domainaudit.Summary, error)
	ExportCSVAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) (domainaudit.Export, error)
}

type EventService interface {
	List(context.Context, int) ([]domainevent.Envelope, error)
	Get(context.Context, string) (domainevent.Envelope, error)
	ValidateConnectorEventSinkToken(string) error
	IngestConnectorEvents(context.Context, domainevent.ConnectorEventIngestInput) (int, error)
}

type OperationService interface {
	ListAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) ([]domainoperation.Entry, error)
	SummaryAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) (domainoperation.Summary, error)
	ExportCSVAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) (domainoperation.Export, error)
}

type IntegrationService interface {
	ListCapabilities(context.Context) ([]domainmcp.Capability, error)
}

type podResourceHandler struct {
	reader      PodReader
	editor      PodResourceEditor
	diagnostics PodDiagnostics
}
type deploymentResourceHandler struct {
	reader DeploymentReader
	editor DeploymentEditor
}
type statefulSetResourceHandler struct {
	reader StatefulSetReader
	editor StatefulSetEditor
}
type daemonSetResourceHandler struct {
	reader DaemonSetReader
	editor DaemonSetEditor
}
type jobResourceHandler struct{ service JobService }
type cronJobResourceHandler struct{ service CronJobService }
type workloadInventoryResourceHandler struct {
	service WorkloadInventoryService
	generic GenericResourceService
}

type configMapResourceHandler struct {
	service ConfigMapService
	creator ResourceCreator
}
type secretResourceHandler struct {
	service SecretService
	creator ResourceCreator
}
type configurationInventoryResourceHandler struct{ service ConfigurationInventoryService }
type networkOverviewResourceHandler struct{ service NetworkOverviewService }
type networkInventoryResourceHandler struct{ service NetworkInventoryService }
type gatewayResourceHandler struct {
	routing GatewayRoutingService
	policy  GatewayPolicyService
}
type persistentVolumeClaimResourceHandler struct {
	service PersistentVolumeClaimService
	creator ResourceCreator
}
type persistentVolumeResourceHandler struct {
	service PersistentVolumeService
	creator ResourceCreator
}
type storageClassResourceHandler struct {
	service StorageClassService
	creator ResourceCreator
}
type namespacedRBACResourceHandler struct {
	service NamespacedRBACService
	creator ResourceCreator
}
type clusterRBACResourceHandler struct {
	service ClusterRBACService
	creator ResourceCreator
}
type crdResourceHandler struct {
	reader CRDReader
	editor CRDEditor
}
type helmCatalogResourceHandler struct{ service HelmCatalogService }
type helmReleaseResourceHandler struct {
	reader HelmReleaseReader
	editor HelmReleaseEditor
}
type clusterHandler struct{ service ClusterService }
type namespaceResourceHandler struct{ service NamespaceService }
type nodeResourceHandler struct {
	reader NodeReader
	editor NodeEditor
}
type genericResourceHandler struct{ service GenericResourceService }
type clusterEventResourceHandler struct{ service ClusterEventService }
type portForwardResourceHandler struct{ service PortForwardService }
type podStreamResourceHandler struct{ service PodStreamService }

type PlatformHandler struct {
	*podResourceHandler
	*deploymentResourceHandler
	*statefulSetResourceHandler
	*daemonSetResourceHandler
	*jobResourceHandler
	*cronJobResourceHandler
	*workloadInventoryResourceHandler
	*configMapResourceHandler
	*secretResourceHandler
	*configurationInventoryResourceHandler
	*networkOverviewResourceHandler
	*networkInventoryResourceHandler
	*gatewayResourceHandler
	*persistentVolumeClaimResourceHandler
	*persistentVolumeResourceHandler
	*storageClassResourceHandler
	*namespacedRBACResourceHandler
	*clusterRBACResourceHandler
	*crdResourceHandler
	*helmCatalogResourceHandler
	*helmReleaseResourceHandler
	*clusterHandler
	*namespaceResourceHandler
	*nodeResourceHandler
	*genericResourceHandler
	*clusterEventResourceHandler
	*portForwardResourceHandler
	*podStreamResourceHandler

	audit       AuditService
	events      EventService
	operations  OperationService
	integration IntegrationService
}

// PlatformDependencies groups the independently replaceable platform capabilities.
type PlatformDependencies struct {
	Clusters    ClusterService
	Resources   ResourceServices
	Audit       AuditService
	Events      EventService
	Operations  OperationService
	Integration IntegrationService
}

var podTerminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return allowWebSocketOrigin(r)
	},
}

const (
	podLogPingInterval   = 20 * time.Second
	podLogPongWait       = 45 * time.Second
	podLogReconnectDelay = 1200 * time.Millisecond
)

// NewPlatformHandlerWithResources is the capability-oriented constructor.
func NewPlatformHandlerWithResources(deps PlatformDependencies) (*PlatformHandler, error) {
	if err := validatePlatformDependencies(deps); err != nil {
		return nil, err
	}
	resources := deps.Resources
	return &PlatformHandler{
		podResourceHandler: &podResourceHandler{
			reader: resources.PodReader, editor: resources.PodEditor, diagnostics: resources.PodDiagnostics,
		},
		deploymentResourceHandler: &deploymentResourceHandler{
			reader: resources.DeploymentReader, editor: resources.DeploymentEditor,
		},
		statefulSetResourceHandler: &statefulSetResourceHandler{
			reader: resources.StatefulSetReader, editor: resources.StatefulSetEditor,
		},
		daemonSetResourceHandler: &daemonSetResourceHandler{
			reader: resources.DaemonSetReader, editor: resources.DaemonSetEditor,
		},
		jobResourceHandler:     &jobResourceHandler{service: resources.Jobs},
		cronJobResourceHandler: &cronJobResourceHandler{service: resources.CronJobs},
		workloadInventoryResourceHandler: &workloadInventoryResourceHandler{
			service: resources.WorkloadInventory,
			generic: resources.Generic,
		},
		configMapResourceHandler: &configMapResourceHandler{
			service: resources.ConfigMaps,
			creator: resources.Creator,
		},
		secretResourceHandler: &secretResourceHandler{
			service: resources.Secrets,
			creator: resources.Creator,
		},
		configurationInventoryResourceHandler: &configurationInventoryResourceHandler{service: resources.ConfigurationInventory},
		networkOverviewResourceHandler:        &networkOverviewResourceHandler{service: resources.NetworkOverview},
		networkInventoryResourceHandler:       &networkInventoryResourceHandler{service: resources.NetworkInventory},
		gatewayResourceHandler: &gatewayResourceHandler{
			routing: resources.GatewayRouting,
			policy:  resources.GatewayPolicy,
		},
		persistentVolumeClaimResourceHandler: &persistentVolumeClaimResourceHandler{
			service: resources.PersistentVolumeClaims,
			creator: resources.Creator,
		},
		persistentVolumeResourceHandler: &persistentVolumeResourceHandler{
			service: resources.PersistentVolumes,
			creator: resources.Creator,
		},
		storageClassResourceHandler: &storageClassResourceHandler{
			service: resources.StorageClasses,
			creator: resources.Creator,
		},
		namespacedRBACResourceHandler: &namespacedRBACResourceHandler{
			service: resources.NamespacedRBAC,
			creator: resources.Creator,
		},
		clusterRBACResourceHandler: &clusterRBACResourceHandler{
			service: resources.ClusterRBAC,
			creator: resources.Creator,
		},
		crdResourceHandler:          &crdResourceHandler{reader: resources.CRDReader, editor: resources.CRDEditor},
		helmCatalogResourceHandler:  &helmCatalogResourceHandler{service: resources.Helm},
		helmReleaseResourceHandler:  &helmReleaseResourceHandler{reader: resources.HelmReleaseReader, editor: resources.HelmReleaseEditor},
		clusterHandler:              &clusterHandler{service: deps.Clusters},
		namespaceResourceHandler:    &namespaceResourceHandler{service: resources.Namespaces},
		nodeResourceHandler:         &nodeResourceHandler{reader: resources.NodeReader, editor: resources.NodeEditor},
		genericResourceHandler:      &genericResourceHandler{service: resources.Generic},
		clusterEventResourceHandler: &clusterEventResourceHandler{service: resources.Events},
		portForwardResourceHandler:  &portForwardResourceHandler{service: resources.PortForwards},
		podStreamResourceHandler:    &podStreamResourceHandler{service: resources.PodStreams},
		audit:                       deps.Audit,
		events:                      deps.Events,
		operations:                  deps.Operations,
		integration:                 deps.Integration,
	}, nil
}

func validatePlatformDependencies(deps PlatformDependencies) error {
	required := map[string]any{
		"clusters": deps.Clusters, "audit": deps.Audit, "events": deps.Events,
		"operations": deps.Operations, "integration": deps.Integration,
		"pod reader": deps.Resources.PodReader, "pod editor": deps.Resources.PodEditor,
		"pod diagnostics": deps.Resources.PodDiagnostics, "pod streams": deps.Resources.PodStreams,
		"deployment reader": deps.Resources.DeploymentReader, "deployment editor": deps.Resources.DeploymentEditor,
		"statefulset reader": deps.Resources.StatefulSetReader, "statefulset editor": deps.Resources.StatefulSetEditor,
		"daemonset reader": deps.Resources.DaemonSetReader, "daemonset editor": deps.Resources.DaemonSetEditor,
		"jobs": deps.Resources.Jobs, "cronjobs": deps.Resources.CronJobs,
		"workload inventory": deps.Resources.WorkloadInventory, "resource creator": deps.Resources.Creator,
		"configmaps": deps.Resources.ConfigMaps, "secrets": deps.Resources.Secrets,
		"configuration inventory": deps.Resources.ConfigurationInventory,
		"network overview":        deps.Resources.NetworkOverview, "network inventory": deps.Resources.NetworkInventory,
		"gateway routing": deps.Resources.GatewayRouting, "gateway policy": deps.Resources.GatewayPolicy,
		"persistent volume claims": deps.Resources.PersistentVolumeClaims,
		"persistent volumes":       deps.Resources.PersistentVolumes, "storage classes": deps.Resources.StorageClasses,
		"namespaced rbac": deps.Resources.NamespacedRBAC, "cluster rbac": deps.Resources.ClusterRBAC,
		"crd reader": deps.Resources.CRDReader, "crd editor": deps.Resources.CRDEditor,
		"helm catalog": deps.Resources.Helm, "helm release reader": deps.Resources.HelmReleaseReader,
		"helm release editor": deps.Resources.HelmReleaseEditor, "namespaces": deps.Resources.Namespaces,
		"node reader": deps.Resources.NodeReader, "node editor": deps.Resources.NodeEditor,
		"generic resources": deps.Resources.Generic, "cluster events": deps.Resources.Events,
		"port forwards": deps.Resources.PortForwards,
	}
	for name, dependency := range required {
		if isNilPlatformDependency(dependency) {
			return fmt.Errorf("platform handler: %s dependency is required", name)
		}
	}
	return nil
}

func isNilPlatformDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
