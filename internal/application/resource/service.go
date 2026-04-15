package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainaudit "github.com/kubecrux/kubecrux/internal/domain/audit"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
	agentinfra "github.com/kubecrux/kubecrux/internal/infrastructure/agent"
	informerinfra "github.com/kubecrux/kubecrux/internal/infrastructure/informer"
	k8sinfra "github.com/kubecrux/kubecrux/internal/infrastructure/kubernetes"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/requestctx"
	"github.com/kubecrux/kubecrux/internal/platform/streamlimit"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type ConnectionResolver interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type MonitoringSettingsResolver interface {
	ResolveMonitoringSettings(context.Context) (domainsettings.MonitoringSettings, error)
}

type Service struct {
	clusters   *k8sinfra.Manager
	cache      *informerinfra.Service
	agents     *agentinfra.Registry
	resolver   ConnectionResolver
	authorizer domainaccess.Authorizer
	audit      AuditRecorder
	settings   MonitoringSettingsResolver
	httpClient *http.Client
}

func New(clusters *k8sinfra.Manager, cache *informerinfra.Service, agents *agentinfra.Registry, resolver ConnectionResolver, authorizer domainaccess.Authorizer, audit AuditRecorder, settings MonitoringSettingsResolver) *Service {
	return &Service{
		clusters:   clusters,
		cache:      cache,
		agents:     agents,
		resolver:   resolver,
		authorizer: authorizer,
		audit:      audit,
		settings:   settings,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) ListNamespaces(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.NamespaceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "Namespace", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.NamespaceView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListNamespaces(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectNamespaces(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.NamespaceView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapNamespace(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.NamespaceView) string { return item.Name })
	populateAllowedActionsNamespaces(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "Namespace", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed namespaces via %s", source))
	return items, nil
}

func (s *Service) CreateNamespace(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, input.Name, "Namespace", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.NamespaceView{}, err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.NamespaceView{}, fmt.Errorf("%w: namespace mutation is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err := s.createDirectNamespace(ctx, clusterID, input)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, input.Name, "Namespace", input.Name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.NamespaceView{}, err
		}
		view := mapNamespace(*item, decision)
		_ = s.recordAudit(ctx, principal, clusterID, input.Name, "Namespace", input.Name, string(domainaccess.ActionUpdate), "success", "created namespace")
		return view, nil
	}
}

func (s *Service) UpdateNamespace(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Namespace", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.NamespaceView{}, err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.NamespaceView{}, fmt.Errorf("%w: namespace mutation is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err := s.updateDirectNamespace(ctx, clusterID, namespace, input)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.NamespaceView{}, err
		}
		view := mapNamespace(*item, decision)
		_ = s.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionUpdate), "success", "updated namespace")
		return view, nil
	}
}

func (s *Service) DeleteNamespace(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Namespace", domainaccess.ActionDelete)
	if err != nil {
		return err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return fmt.Errorf("%w: namespace mutation is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		if err := s.deleteDirectNamespace(ctx, clusterID, namespace); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		_ = s.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionDelete), "success", "deleted namespace")
		return nil
	}
}

func (s *Service) ListNodes(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.NodeView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.NodeView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListNodes(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectNodes(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		rawPods, _, err := s.listDirectPods(ctx, clusterID, metav1.NamespaceAll)
		if err != nil {
			return nil, err
		}
		items = buildNodeViews(rawItems, rawPods, decision)
		source = rawSource
	}
	populateAllowedActionsNodes(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "Node", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed nodes via %s", source))
	return items, nil
}

func (s *Service) UpdateNode(ctx context.Context, principal domainidentity.Principal, clusterID, nodeName string, input domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.NodeDetailView{}, fmt.Errorf("%w: node mutation is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err := s.updateDirectNode(ctx, clusterID, nodeName, input)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.NodeDetailView{}, err
		}
		view := s.buildNodeDetail(ctx, clusterID, *item, nil, domainaccess.Decision{AllowedActions: []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionUpdate, domainaccess.ActionDelete}})
		_ = s.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionUpdate), "success", "updated node labels and taints")
		return view, nil
	}
}

func (s *Service) DeleteNode(ctx context.Context, principal domainidentity.Principal, clusterID, nodeName string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionDelete)
	if err != nil {
		return err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return fmt.Errorf("%w: node deletion is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		if err := s.deleteDirectNode(ctx, clusterID, nodeName); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		_ = s.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionDelete), "success", "deleted node object")
		return nil
	}
}

func (s *Service) ListPods(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}

	items, source, err := s.listPodViews(ctx, clusterID, namespace, connection, decision, true)
	if err != nil {
		return nil, err
	}
	populateAllowedActionsPods(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pods via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetWorkloadOverview(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (domainresource.WorkloadOverviewView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionList)
	if err != nil {
		return domainresource.WorkloadOverviewView{}, err
	}

	items, source, err := s.listPodViews(ctx, clusterID, namespace, connection, decision, false)
	if err != nil {
		return domainresource.WorkloadOverviewView{}, err
	}
	view := buildWorkloadOverview(clusterID, namespace, source, items)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", "", string(domainaccess.ActionList), "success", fmt.Sprintf("summarized pod runtime via %s in namespace %s", source, displayNamespace(namespace)))
	return view, nil
}

func (s *Service) GetPodDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PodDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionView)
	if err != nil {
		return domainresource.PodDetailView{}, err
	}

	var (
		item   domainresource.PodDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PodDetailView{}, err
		}
		item, err = client.GetPodDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.PodDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectPod(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.PodDetailView{}, err
		}
		item = mapPodDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed pod detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) DeletePod(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionDelete)
	if err != nil {
		return err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return fmt.Errorf("%w: pod deletion is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		if err := s.deleteDirectPod(ctx, clusterID, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
	}

	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionDelete), "success", "deleted pod for rebuild")
	return nil
}

func (s *Service) GetPodLogs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}

	var (
		item   domainresource.PodLogsView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PodLogsView{}, err
		}
		item, err = client.GetPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, previous)
		if err != nil {
			return domainresource.PodLogsView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectPodLogs(ctx, clusterID, namespace, name, container, tailLines, sinceSeconds, previous)
		if err != nil {
			return domainresource.PodLogsView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "success", fmt.Sprintf("read pod logs via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) StreamPodLogs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return fmt.Errorf("%w: streaming pod logs are not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		if err := s.streamDirectPodLogs(ctx, clusterID, namespace, name, container, tailLines, sinceSeconds, stdout); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionLogs), "failure", err.Error())
			return err
		}
	}

	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "success", fmt.Sprintf("streamed pod logs in namespace %s", displayNamespace(namespace)))
	return nil
}

func (s *Service) ExecPod(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return domainresource.PodExecView{}, fmt.Errorf("%w: command is required", apperrors.ErrInvalidArgument)
	}

	var (
		item   domainresource.PodExecView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PodExecView{}, err
		}
		item, err = client.ExecPod(ctx, namespace, name, container, command, timeoutSeconds)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
			return domainresource.PodExecView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.execDirectPod(ctx, clusterID, namespace, name, container, command, timeoutSeconds)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
			return domainresource.PodExecView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "success", fmt.Sprintf("executed pod command via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) StreamPodTerminal(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue remotecommand.TerminalSizeQueue) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return err
	}
	shell = normalizeTerminalShell(shell)

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		err = fmt.Errorf("%w: interactive terminal is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		err = s.streamDirectPodTerminal(ctx, clusterID, namespace, name, container, shell, stdin, stdout, stderr, sizeQueue)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
		return err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "success", fmt.Sprintf("opened interactive pod terminal via %s in namespace %s", shell, displayNamespace(namespace)))
	return nil
}

func (s *Service) GetPodYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetPodYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectPodYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed pod yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyPodYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "Pod", name, content)
}

func (s *Service) ListDeployments(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.DeploymentView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListDeployments(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectDeployments(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.DeploymentView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapDeployment(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.DeploymentView) string { return item.Namespace })
	populateAllowedActionsDeployments(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed deployments via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetDeploymentDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return domainresource.DeploymentDetailView{}, err
	}

	var (
		item   domainresource.DeploymentDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DeploymentDetailView{}, err
		}
		item, err = client.GetDeploymentDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.DeploymentDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectDeployment(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.DeploymentDetailView{}, err
		}
		item = mapDeploymentDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed deployment detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) GetDeploymentYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetDeploymentYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectDeploymentYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed deployment yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyDeploymentYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "Deployment", name, content)
}

func (s *Service) GetStatefulSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.StatefulSetDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.StatefulSetDetailView{}, err
	}
	var (
		item   domainresource.StatefulSetDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.StatefulSetDetailView{}, err
		}
		item, err = client.GetStatefulSetDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.StatefulSetDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectStatefulSet(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.StatefulSetDetailView{}, err
		}
		item = mapStatefulSetDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed statefulset detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) GetStatefulSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetStatefulSetYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectStatefulSetYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed statefulset yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyStatefulSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "StatefulSet", name, content)
}

func (s *Service) GetDaemonSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	var (
		item   domainresource.DaemonSetDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DaemonSetDetailView{}, err
		}
		item, err = client.GetDaemonSetDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.DaemonSetDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectDaemonSet(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.DaemonSetDetailView{}, err
		}
		item = mapDaemonSetDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed daemonset detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) GetDaemonSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetDaemonSetYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectDaemonSetYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed daemonset yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyDaemonSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "DaemonSet", name, content)
}

func (s *Service) GetJobDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.JobDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Job", domainaccess.ActionView)
	if err != nil {
		return domainresource.JobDetailView{}, err
	}
	var (
		item   domainresource.JobDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.JobDetailView{}, err
		}
		item, err = client.GetJobDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.JobDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectJob(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.JobDetailView{}, err
		}
		item = mapJobDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Job", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed job detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) GetJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Job", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetJobYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectJobYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Job", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed job yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "Job", name, content)
}

func (s *Service) GetCronJobDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.CronJobDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionView)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	var (
		item   domainresource.CronJobDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.CronJobDetailView{}, err
		}
		item, err = client.GetCronJobDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.CronJobDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectCronJob(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.CronJobDetailView{}, err
		}
		item = mapCronJobDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed cronjob detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) GetCronJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetCronJobYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectCronJobYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed cronjob yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyCronJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "CronJob", name, content)
}

func (s *Service) GetDeploymentRolloutStatus(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}

	var (
		item   domainresource.DeploymentRolloutStatusView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DeploymentRolloutStatusView{}, err
		}
		item, err = client.GetDeploymentRolloutStatus(ctx, namespace, name)
		if err != nil {
			return domainresource.DeploymentRolloutStatusView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectDeploymentRolloutStatus(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.DeploymentRolloutStatusView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed deployment rollout status via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ListDeploymentRolloutHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.RolloutHistoryView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListDeploymentRolloutHistory(ctx, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectDeploymentRolloutHistory(ctx, clusterID, namespace, name)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("listed deployment rollout history via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) RollbackDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revision string) (domainresource.DeploymentRollbackView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: revision is required", apperrors.ErrInvalidArgument)
	}

	var source string
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DeploymentRollbackView{}, err
		}
		if err := client.RollbackDeployment(ctx, namespace, name, revision); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.rollbackDirectDeployment(ctx, clusterID, namespace, name, revision); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, err
		}
		source = "live"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionUpdate), "success", fmt.Sprintf("rolled back deployment to revision %s via %s", revision, source))
	return domainresource.DeploymentRollbackView{
		Name:           name,
		Namespace:      namespace,
		TargetRevision: revision,
		Message:        fmt.Sprintf("Rollback to revision %s requested.", revision),
		RequestedAt:    now,
	}, nil
}

func (s *Service) ListStatefulSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.StatefulSetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.StatefulSetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListStatefulSets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectStatefulSets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.StatefulSetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapStatefulSet(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.StatefulSetView) string { return item.Namespace })
	populateAllowedActionsStatefulSets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed statefulsets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListDaemonSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DaemonSetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.DaemonSetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListDaemonSets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectDaemonSets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.DaemonSetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapDaemonSet(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.DaemonSetView) string { return item.Namespace })
	populateAllowedActionsDaemonSets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed daemonsets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListJobs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.JobView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Job", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.JobView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListJobs(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectJobs(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.JobView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapJob(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.JobView) string { return item.Namespace })
	populateAllowedActionsJobs(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Job", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed jobs via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListCronJobs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.CronJobView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.CronJobView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListCronJobs(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectCronJobs(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.CronJobView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapCronJob(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.CronJobView) string { return item.Namespace })
	populateAllowedActionsCronJobs(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed cronjobs via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListServices(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Service", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ServiceView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListServices(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectServices(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ServiceView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapService(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ServiceView) string { return item.Namespace })
	populateAllowedActionsServices(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Service", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed services via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListIngresses(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.IngressView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Ingress", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.IngressView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListIngresses(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectIngresses(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.IngressView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapIngress(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.IngressView) string { return item.Namespace })
	populateAllowedActionsIngresses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Ingress", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed ingresses via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListPersistentVolumeClaims(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "PersistentVolumeClaim", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PersistentVolumeClaimView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPersistentVolumeClaims(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPersistentVolumeClaims(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PersistentVolumeClaimView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPersistentVolumeClaim(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PersistentVolumeClaimView) string { return item.Namespace })
	populateAllowedActionsPersistentVolumeClaims(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "PersistentVolumeClaim", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pvc via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListPersistentVolumes(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PersistentVolumeView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "PersistentVolume", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PersistentVolumeView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPersistentVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPersistentVolumes(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PersistentVolumeView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPersistentVolume(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsPersistentVolumes(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "PersistentVolume", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pv via %s", source))
	return items, nil
}

func (s *Service) ListStorageClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.StorageClassView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "StorageClass", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.StorageClassView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListStorageClasses(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectStorageClasses(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.StorageClassView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapStorageClass(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsStorageClasses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "StorageClass", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed storageclasses via %s", source))
	return items, nil
}

func (s *Service) ListCRDs(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.CRDView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "CRD", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.CRDView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListCRDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectCRDs(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	populateAllowedActionsCRDs(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "CRD", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed crds via %s", source))
	return items, nil
}

func (s *Service) ListHelmReleases(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.HelmReleaseView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHelmReleases(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectHelmReleases(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.HelmReleaseView) string { return item.Namespace })
	populateAllowedActionsHelmReleases(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed helm releases via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListClusterEvents(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Event", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ClusterEventView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListClusterEvents(ctx, namespace, limit)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectClusterEvents(ctx, clusterID, namespace, limit)
		if err != nil {
			return nil, err
		}
		items = rawItems
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ClusterEventView) string { return item.Namespace })
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Event", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed cluster events via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) RestartDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionRestart)
	if err != nil {
		return err
	}

	source := "direct"
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return err
		}
		if err := client.RestartDeployment(ctx, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRestart), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.restartDirectDeployment(ctx, clusterID, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRestart), "failure", err.Error())
			return err
		}
	}
	if err := s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRestart), "success", fmt.Sprintf("restarted deployment via %s", source)); err != nil {
		return fmt.Errorf("record restart deployment audit: %w", err)
	}
	return nil
}

func (s *Service) ScaleDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, replicas int32) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionScale)
	if err != nil {
		return err
	}

	source := "direct"
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return err
		}
		if err := client.ScaleDeployment(ctx, namespace, name, replicas); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionScale), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.scaleDirectDeployment(ctx, clusterID, namespace, name, replicas); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionScale), "failure", err.Error())
			return err
		}
	}
	if err := s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionScale), "success", fmt.Sprintf("scaled deployment to %d via %s", replicas, source)); err != nil {
		return fmt.Errorf("record scale deployment audit: %w", err)
	}
	return nil
}

func (s *Service) listDirectNamespaces(ctx context.Context, clusterID string) ([]corev1.Namespace, string, error) {
	if items, err := s.cache.ListNamespaces(clusterID); err == nil {
		return items, "cache", nil
	} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
		return nil, "cache", err
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Namespaces().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}

func (s *Service) listDirectNodes(ctx context.Context, clusterID string) ([]corev1.Node, string, error) {
	if items, err := s.cache.ListNodes(clusterID); err == nil {
		return items, "cache", nil
	} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
		return nil, "cache", err
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Nodes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}

func (s *Service) listDirectPods(ctx context.Context, clusterID, namespace string) ([]corev1.Pod, string, error) {
	if items, err := s.cache.ListPods(clusterID, namespace); err == nil {
		return items, "cache", nil
	} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
		return nil, "cache", err
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := listPodsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, "live", err
	}
	return items, "live", nil
}

func (s *Service) getDirectPod(ctx context.Context, clusterID, namespace, name string) (*corev1.Pod, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) deleteDirectPod(ctx context.Context, clusterID, namespace, name string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Pods(namespace).Delete(queryCtx, name, metav1.DeleteOptions{})
}

func (s *Service) getDirectNode(ctx context.Context, clusterID, name string) (*corev1.Node, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Nodes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) getDirectPodLogs(ctx context.Context, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.PodLogsView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	options := &corev1.PodLogOptions{Container: container, Previous: previous}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if sinceSeconds > 0 {
		options.SinceSeconds = &sinceSeconds
	}
	stream, err := bundle.Typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(queryCtx)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	defer stream.Close()
	content, totalBytes, contentTruncated, err := streamlimit.ReadString(stream, domainresource.PodLogsMaxContentBytes)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	return domainresource.PodLogsView{
		PodName:      name,
		Namespace:    namespace,
		Container:    container,
		Content:      content,
		ContentBytes: totalBytes,
		MaxBytes:     domainresource.PodLogsMaxContentBytes,
		TailLines:    tailLines,
		Previous:     previous,
		Truncated:    tailLines > 0 || contentTruncated,
	}, nil
}

func (s *Service) execDirectPod(ctx context.Context, clusterID, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.PodExecView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	request := bundle.Typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{"/bin/sh", "-lc", command},
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, http.MethodPost, request.URL())
	if err != nil {
		return domainresource.PodExecView{}, err
	}

	stdout := streamlimit.NewLimitedBuffer(domainresource.PodExecMaxOutputBytes)
	stderr := streamlimit.NewLimitedBuffer(domainresource.PodExecMaxOutputBytes)
	execErr := executor.StreamWithContext(queryCtx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
	exitMessage := ""
	if execErr != nil {
		exitMessage = execErr.Error()
	}
	return domainresource.PodExecView{
		PodName:         name,
		Namespace:       namespace,
		Container:       container,
		Command:         command,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutBytes:     stdout.TotalBytes(),
		StderrBytes:     stderr.TotalBytes(),
		MaxBytes:        domainresource.PodExecMaxOutputBytes,
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
		Success:         execErr == nil,
		ExitMessage:     exitMessage,
		ExecutedAt:      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) streamDirectPodLogs(ctx context.Context, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	options := &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
	}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if sinceSeconds > 0 {
		options.SinceSeconds = &sinceSeconds
	}
	stream, err := bundle.Typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	_, err = io.Copy(stdout, stream)
	return err
}

func (s *Service) streamDirectPodTerminal(ctx context.Context, clusterID, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue remotecommand.TerminalSizeQueue) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}

	request := bundle.Typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{shell},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, http.MethodPost, request.URL())
	if err != nil {
		return err
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	})
}

func normalizeTerminalShell(shell string) string {
	switch strings.TrimSpace(shell) {
	case "/bin/bash":
		return "/bin/bash"
	case "/bin/ash":
		return "/bin/ash"
	case "/busybox/sh":
		return "/busybox/sh"
	default:
		return "/bin/sh"
	}
}

func (s *Service) getDirectPodYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectPod(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      "Pod",
		Name:      name,
		Namespace: namespace,
		Content:   string(content),
	}, nil
}

func (s *Service) listDirectDeployments(ctx context.Context, clusterID, namespace string) ([]appsv1.Deployment, string, error) {
	if strings.TrimSpace(namespace) == "" {
		items, err := s.listDeploymentsAcrossNamespaces(ctx, clusterID)
		if err != nil {
			return nil, "live", err
		}
		return items, "live", nil
	}
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListDeployments(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := listDeploymentsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, "live", err
	}
	return items, "live", nil
}

func (s *Service) getDirectDeployment(ctx context.Context, clusterID, namespace, name string) (*appsv1.Deployment, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) getDirectDeploymentYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      "Deployment",
		Name:      name,
		Namespace: namespace,
		Content:   string(content),
	}, nil
}

func (s *Service) getDirectDeploymentRolloutStatus(ctx context.Context, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	item, err := s.getDirectDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}
	return mapDeploymentRolloutStatus(*item), nil
}

func (s *Service) getDirectStatefulSet(ctx context.Context, clusterID, namespace, name string) (*appsv1.StatefulSet, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) getDirectStatefulSetYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectStatefulSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "StatefulSet", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (s *Service) getDirectDaemonSet(ctx context.Context, clusterID, namespace, name string) (*appsv1.DaemonSet, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) getDirectDaemonSetYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectDaemonSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "DaemonSet", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (s *Service) getDirectJob(ctx context.Context, clusterID, namespace, name string) (*batchv1.Job, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.BatchV1().Jobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) getDirectJobYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "Job", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (s *Service) getDirectCronJob(ctx context.Context, clusterID, namespace, name string) (*batchv1.CronJob, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) getDirectCronJobYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectCronJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "CronJob", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (s *Service) applyResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if strings.TrimSpace(content) == "" {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml apply is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err := s.applyDirectResourceYAML(ctx, clusterID, namespace, kind, name, content)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionUpdate), "success", "applied resource yaml")
		return item, nil
	}
}

func (s *Service) applyDirectResourceYAML(ctx context.Context, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	item.SetKind(kind)
	if item.GetName() == "" {
		item.SetName(name)
	}
	if item.GetNamespace() == "" {
		item.SetNamespace(namespace)
	}
	if item.GetName() != name {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.name does not match target resource", apperrors.ErrInvalidArgument)
	}
	if item.GetNamespace() != namespace {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace does not match target resource", apperrors.ErrInvalidArgument)
	}
	gvr, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource := bundle.Dynamic.Resource(gvr).Namespace(namespace)
	if item.GetResourceVersion() == "" {
		current, err := resource.Get(queryCtx, name, metav1.GetOptions{})
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item.SetResourceVersion(current.GetResourceVersion())
	}
	updated, err := resource.Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	rendered, err := yaml.Marshal(updated.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
		Content:   string(rendered),
	}, nil
}

func resourceGVRForKind(kind string) (schema.GroupVersionResource, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, nil
	case "deployment":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, nil
	case "statefulset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, nil
	case "daemonset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, nil
	case "job":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, nil
	case "cronjob":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("%w: yaml apply does not support kind %s", apperrors.ErrInvalidArgument, kind)
	}
}

func (s *Service) listDirectDeploymentRolloutHistory(ctx context.Context, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]domainresource.RolloutHistoryView, 0)
	for _, item := range replicaSets.Items {
		if !ownedByDeployment(item.OwnerReferences, deployment.UID) {
			continue
		}
		images := make([]string, 0, len(item.Spec.Template.Spec.Containers))
		for _, container := range item.Spec.Template.Spec.Containers {
			images = append(images, fmt.Sprintf("%s=%s", container.Name, container.Image))
		}
		replicas := int32(0)
		if item.Spec.Replicas != nil {
			replicas = *item.Spec.Replicas
		}
		items = append(items, domainresource.RolloutHistoryView{
			Name:          item.Name,
			Namespace:     item.Namespace,
			Revision:      item.Annotations["deployment.kubernetes.io/revision"],
			Images:        images,
			Replicas:      replicas,
			ReadyReplicas: item.Status.ReadyReplicas,
			CreatedAt:     item.CreationTimestamp.Time.Format(time.RFC3339),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func (s *Service) rollbackDirectDeployment(ctx context.Context, clusterID, namespace, name, revision string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var target *appsv1.ReplicaSet
	for index := range replicaSets.Items {
		item := &replicaSets.Items[index]
		if !ownedByDeployment(item.OwnerReferences, deployment.UID) {
			continue
		}
		if item.Annotations["deployment.kubernetes.io/revision"] == revision {
			target = item
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: target revision %s not found", apperrors.ErrNotFound, revision)
	}
	deployment.Spec.Template = *target.Spec.Template.DeepCopy()
	if deployment.Spec.Template.Labels != nil {
		delete(deployment.Spec.Template.Labels, "pod-template-hash")
	}
	queryCtxUpdate, cancelUpdate := context.WithTimeout(ctx, 5*time.Second)
	defer cancelUpdate()
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtxUpdate, deployment, metav1.UpdateOptions{})
	return err
}

func (s *Service) listDirectStatefulSets(ctx context.Context, clusterID, namespace string) ([]appsv1.StatefulSet, string, error) {
	if strings.TrimSpace(namespace) == "" {
		items, err := s.listStatefulSetsAcrossNamespaces(ctx, clusterID)
		if err != nil {
			return nil, "live", err
		}
		return items, "live", nil
	}
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListStatefulSets(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := listStatefulSetsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, "live", err
	}
	return items, "live", nil
}

func (s *Service) listDirectDaemonSets(ctx context.Context, clusterID, namespace string) ([]appsv1.DaemonSet, error) {
	if strings.TrimSpace(namespace) == "" {
		return s.listDaemonSetsAcrossNamespaces(ctx, clusterID)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := listDaemonSetsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listDirectJobs(ctx context.Context, clusterID, namespace string) ([]batchv1.Job, error) {
	if strings.TrimSpace(namespace) == "" {
		return s.listJobsAcrossNamespaces(ctx, clusterID)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := listJobsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listDirectCronJobs(ctx context.Context, clusterID, namespace string) ([]batchv1.CronJob, error) {
	if strings.TrimSpace(namespace) == "" {
		return s.listCronJobsAcrossNamespaces(ctx, clusterID)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := listCronJobsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) listPodsAcrossNamespaces(ctx context.Context, clusterID string) ([]corev1.Pod, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.Pod, error) {
		return listPodsLive(queryCtx, bundle, namespace)
	})
}

func (s *Service) listDeploymentsAcrossNamespaces(ctx context.Context, clusterID string) ([]appsv1.Deployment, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.Deployment, error) {
		return listDeploymentsLive(queryCtx, bundle, namespace)
	})
}

func (s *Service) listStatefulSetsAcrossNamespaces(ctx context.Context, clusterID string) ([]appsv1.StatefulSet, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.StatefulSet, error) {
		return listStatefulSetsLive(queryCtx, bundle, namespace)
	})
}

func (s *Service) listDaemonSetsAcrossNamespaces(ctx context.Context, clusterID string) ([]appsv1.DaemonSet, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.DaemonSet, error) {
		return listDaemonSetsLive(queryCtx, bundle, namespace)
	})
}

func (s *Service) listJobsAcrossNamespaces(ctx context.Context, clusterID string) ([]batchv1.Job, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.Job, error) {
		return listJobsLive(queryCtx, bundle, namespace)
	})
}

func (s *Service) listCronJobsAcrossNamespaces(ctx context.Context, clusterID string) ([]batchv1.CronJob, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.CronJob, error) {
		return listCronJobsLive(queryCtx, bundle, namespace)
	})
}

func listAcrossNamespaces[T any](ctx context.Context, s *Service, clusterID string, listFn func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	namespaces, _, err := s.listDirectNamespaces(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}

	items := make([]T, 0)
	for _, namespace := range namespaces {
		name := strings.TrimSpace(namespace.Name)
		if name == "" {
			continue
		}
		queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		result, listErr := listFn(queryCtx, bundle, name)
		cancel()
		if listErr != nil {
			return nil, listErr
		}
		items = append(items, result...)
	}
	return items, nil
}

func listPodsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.Pod, error) {
	var list corev1.PodList
	if err := bundle.Typed.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listDeploymentsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.Deployment, error) {
	var list appsv1.DeploymentList
	if err := bundle.Typed.AppsV1().RESTClient().Get().
		Namespace(namespace).
		Resource("deployments").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listStatefulSetsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.StatefulSet, error) {
	var list appsv1.StatefulSetList
	if err := bundle.Typed.AppsV1().RESTClient().Get().
		Namespace(namespace).
		Resource("statefulsets").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listDaemonSetsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.DaemonSet, error) {
	var list appsv1.DaemonSetList
	if err := bundle.Typed.AppsV1().RESTClient().Get().
		Namespace(namespace).
		Resource("daemonsets").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listJobsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.Job, error) {
	var list batchv1.JobList
	if err := bundle.Typed.BatchV1().RESTClient().Get().
		Namespace(namespace).
		Resource("jobs").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listCronJobsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.CronJob, error) {
	var list batchv1.CronJobList
	if err := bundle.Typed.BatchV1().RESTClient().Get().
		Namespace(namespace).
		Resource("cronjobs").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (s *Service) listDirectCRDs(ctx context.Context, clusterID string) ([]domainresource.CRDView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	gvr := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	items, err := bundle.Dynamic.Resource(gvr).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.CRDView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapCRD(item))
	}
	return views, nil
}

func (s *Service) listDirectHelmReleases(ctx context.Context, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	secrets, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HelmReleaseView, 0, len(secrets.Items))
	for _, item := range secrets.Items {
		views = append(views, mapHelmRelease(item.Name, item.Namespace, item.Labels, item.CreationTimestamp.Time, "secret"))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Namespace != views[j].Namespace {
			return views[i].Namespace < views[j].Namespace
		}
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].Revision > views[j].Revision
	})
	return dedupeHelmReleases(views), nil
}

func (s *Service) listDirectServices(ctx context.Context, clusterID, namespace string) ([]corev1.Service, string, error) {
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListServices(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Services(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}

func (s *Service) listDirectIngresses(ctx context.Context, clusterID, namespace string) ([]networkingv1.Ingress, string, error) {
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListIngresses(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().Ingresses(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}

func (s *Service) listDirectClusterEvents(ctx context.Context, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, string, error) {
	var (
		rawItems []corev1.Event
		source   string
	)
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListEvents(clusterID, namespace); err == nil {
			rawItems = items
			source = "cache"
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		} else {
			bundle, err := s.clusters.Bundle(ctx, clusterID)
			if err != nil {
				return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
			}
			queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			items, err := bundle.Typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, "live", err
			}
			rawItems = items.Items
			source = "live"
		}
	} else {
		bundle, err := s.clusters.Bundle(ctx, clusterID)
		if err != nil {
			return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		items, err := bundle.Typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, "live", err
		}
		rawItems = items.Items
		source = "live"
	}
	views := make([]domainresource.ClusterEventView, 0, len(rawItems))
	for _, item := range rawItems {
		views = append(views, mapClusterEvent(item))
	}
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].LastTimestamp > views[j].LastTimestamp
	})
	if limit > 0 && len(views) > limit {
		views = views[:limit]
	}
	return views, source, nil
}

// Namespace-scoped cache is reliable, but the current all-namespaces path can
// return incomplete data from the informer branch. Use live queries there.
func shouldUseInformerCache(namespace string) bool {
	return strings.TrimSpace(namespace) != ""
}

func shouldPopulatePodUsageSummaries(namespace string) bool {
	return false
}

func (s *Service) listDirectPersistentVolumeClaims(ctx context.Context, clusterID, namespace string) ([]corev1.PersistentVolumeClaim, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().PersistentVolumeClaims(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectPersistentVolumes(ctx context.Context, clusterID string) ([]corev1.PersistentVolume, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().PersistentVolumes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectStorageClasses(ctx context.Context, clusterID string) ([]storagev1.StorageClass, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.StorageV1().StorageClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) restartDirectDeployment(ctx context.Context, clusterID, namespace, name string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (s *Service) scaleDirectDeployment(ctx context.Context, clusterID, namespace, name string, replicas int32) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	deployment.Spec.Replicas = &replicas
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (s *Service) createDirectNamespace(ctx context.Context, clusterID string, input domainresource.NamespaceUpsertInput) (*corev1.Namespace, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: namespace name is required", apperrors.ErrInvalidArgument)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      sanitizeStringMap(input.Labels),
			Annotations: sanitizeStringMap(input.Annotations),
		},
	}
	return bundle.Typed.CoreV1().Namespaces().Create(queryCtx, item, metav1.CreateOptions{})
}

func (s *Service) updateDirectNamespace(ctx context.Context, clusterID, namespace string, input domainresource.NamespaceUpsertInput) (*corev1.Namespace, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Namespaces().Get(queryCtx, namespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	item.Labels = sanitizeStringMap(input.Labels)
	item.Annotations = sanitizeStringMap(input.Annotations)
	return bundle.Typed.CoreV1().Namespaces().Update(queryCtx, item, metav1.UpdateOptions{})
}

func (s *Service) deleteDirectNamespace(ctx context.Context, clusterID, namespace string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Namespaces().Delete(queryCtx, namespace, metav1.DeleteOptions{})
}

func (s *Service) updateDirectNode(ctx context.Context, clusterID, nodeName string, input domainresource.NodeUpdateInput) (*corev1.Node, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Nodes().Get(queryCtx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	item.Labels = sanitizeStringMap(input.Labels)
	item.Spec.Taints = make([]corev1.Taint, 0, len(input.Taints))
	for _, taint := range input.Taints {
		key := strings.TrimSpace(taint.Key)
		effect := strings.TrimSpace(taint.Effect)
		if key == "" || effect == "" {
			continue
		}
		item.Spec.Taints = append(item.Spec.Taints, corev1.Taint{
			Key:    key,
			Value:  strings.TrimSpace(taint.Value),
			Effect: corev1.TaintEffect(effect),
		})
	}
	return bundle.Typed.CoreV1().Nodes().Update(queryCtx, item, metav1.UpdateOptions{})
}

func (s *Service) deleteDirectNode(ctx context.Context, clusterID, nodeName string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Nodes().Delete(queryCtx, nodeName, metav1.DeleteOptions{})
}

func sanitizeStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		out[trimmedKey] = strings.TrimSpace(value)
	}
	return out
}

func (s *Service) agentClient(connection domaincluster.Connection) (*agentinfra.Client, error) {
	if s.agents == nil {
		return nil, fmt.Errorf("%w: agent registry is not configured", apperrors.ErrClusterUnready)
	}
	client, err := s.agents.ClientFor(connection)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	return client, nil
}

func (s *Service) loadConnection(ctx context.Context, clusterID string) (domaincluster.Connection, error) {
	if s.resolver != nil {
		connection, err := s.resolver.GetConnection(ctx, clusterID)
		if err == nil {
			if connection.Summary.ConnectionMode == "" {
				connection.Summary.ConnectionMode = domaincluster.ConnectionModeDirectKubeconfig
			}
			return connection, nil
		}
	}
	summary, err := s.clusters.Metadata(clusterID)
	if err != nil {
		return domaincluster.Connection{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return domaincluster.Connection{Summary: summary}, nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind string, action domainaccess.Action) (domaincluster.Connection, domainaccess.Decision, error) {
	connection, err := s.loadConnection(ctx, clusterID)
	if err != nil {
		return domaincluster.Connection{}, domainaccess.Decision{}, err
	}
	request := domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   connection.Summary.ID,
			Region:      connection.Summary.Region,
			Environment: connection.Summary.Environment,
			Labels:      connection.Summary.Labels,
		},
		Namespace: domainaccess.NamespaceAttributes{Namespace: namespace},
		Resource:  domainaccess.ResourceAttributes{Kind: kind},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	}
	decision, err := s.authorizer.Authorize(ctx, request)
	if err != nil {
		return domaincluster.Connection{}, domainaccess.Decision{}, err
	}
	if !decision.Allowed {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, "", string(action), "deny", decision.Reason)
		return domaincluster.Connection{}, domainaccess.Decision{}, fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return connection, decision, nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, action, result, summary string) error {
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ClusterID:     clusterID,
		Namespace:     namespace,
		ResourceKind:  kind,
		ResourceName:  name,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"source": meta.Source,
		},
	})
}

func filterScopedNamespaceItems[T any](items []T, decision domainaccess.Decision, namespaceOf func(T) string) []T {
	if decision.ResourceScope == nil || len(decision.ResourceScope.Namespaces) == 0 {
		return items
	}
	allowed := make(map[string]struct{}, len(decision.ResourceScope.Namespaces))
	for _, namespace := range decision.ResourceScope.Namespaces {
		allowed[namespace] = struct{}{}
	}
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[namespaceOf(item)]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func mapNamespace(item corev1.Namespace, decision domainaccess.Decision) domainresource.NamespaceView {
	return domainresource.NamespaceView{
		Name:           item.Name,
		Status:         string(item.Status.Phase),
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapPod(item corev1.Pod, decision domainaccess.Decision) domainresource.PodView {
	ready := 0
	restarts := int32(0)
	claims := make([]string, 0)
	for _, status := range item.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
	}
	for _, volume := range item.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && strings.TrimSpace(volume.PersistentVolumeClaim.ClaimName) != "" {
			claims = append(claims, volume.PersistentVolumeClaim.ClaimName)
		}
	}
	return domainresource.PodView{
		Name:                   item.Name,
		Namespace:              item.Namespace,
		Phase:                  string(item.Status.Phase),
		NodeName:               item.Spec.NodeName,
		PodIP:                  item.Status.PodIP,
		CreatedAt:              item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:                 item.Labels,
		PersistentVolumeClaims: claims,
		ReadyContainers:        fmt.Sprintf("%d/%d", ready, len(item.Status.ContainerStatuses)),
		Restarts:               restarts,
		AgeSeconds:             secondsSince(item.CreationTimestamp.Time),
		AllowedActions:         stringifyActions(decision.AllowedActions),
	}
}

func buildWorkloadOverview(clusterID, namespace, source string, items []domainresource.PodView) domainresource.WorkloadOverviewView {
	view := domainresource.WorkloadOverviewView{
		ClusterID:   clusterID,
		Namespace:   strings.TrimSpace(namespace),
		Source:      source,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	namespaceSummary := make(map[string]*domainresource.WorkloadOverviewNamespaceView, len(items))
	problematicPods := make([]domainresource.WorkloadOverviewPodView, 0)

	for _, item := range items {
		view.TotalPods++
		phase := normalizedPodPhase(item.Phase)
		switch phase {
		case "Running":
			view.RunningPods++
		case "Pending":
			view.PendingPods++
		case "Succeeded":
			view.SucceededPods++
		case "Failed":
			view.FailedPods++
		default:
			view.UnknownPods++
		}
		if item.Restarts > 0 {
			view.RestartingPods++
		}
		if podNeedsAttention(item) {
			view.AtRiskPods++
			problematicPods = append(problematicPods, domainresource.WorkloadOverviewPodView{
				Name:            item.Name,
				Namespace:       item.Namespace,
				Phase:           phase,
				ReadyContainers: item.ReadyContainers,
				Restarts:        item.Restarts,
				NodeName:        item.NodeName,
				AgeSeconds:      item.AgeSeconds,
			})
		}

		summary, ok := namespaceSummary[item.Namespace]
		if !ok {
			summary = &domainresource.WorkloadOverviewNamespaceView{Namespace: item.Namespace}
			namespaceSummary[item.Namespace] = summary
		}
		summary.TotalPods++
		if phase == "Running" {
			summary.RunningPods++
		}
		if item.Restarts > 0 {
			summary.RestartingPods++
		}
		if podNeedsAttention(item) {
			summary.AtRiskPods++
		}
	}

	view.NamespaceBreakdown = make([]domainresource.WorkloadOverviewNamespaceView, 0, len(namespaceSummary))
	for _, item := range namespaceSummary {
		view.NamespaceBreakdown = append(view.NamespaceBreakdown, *item)
	}
	sort.SliceStable(view.NamespaceBreakdown, func(i, j int) bool {
		if view.NamespaceBreakdown[i].AtRiskPods != view.NamespaceBreakdown[j].AtRiskPods {
			return view.NamespaceBreakdown[i].AtRiskPods > view.NamespaceBreakdown[j].AtRiskPods
		}
		if view.NamespaceBreakdown[i].RestartingPods != view.NamespaceBreakdown[j].RestartingPods {
			return view.NamespaceBreakdown[i].RestartingPods > view.NamespaceBreakdown[j].RestartingPods
		}
		if view.NamespaceBreakdown[i].TotalPods != view.NamespaceBreakdown[j].TotalPods {
			return view.NamespaceBreakdown[i].TotalPods > view.NamespaceBreakdown[j].TotalPods
		}
		return view.NamespaceBreakdown[i].Namespace < view.NamespaceBreakdown[j].Namespace
	})
	if len(view.NamespaceBreakdown) > 6 {
		view.NamespaceBreakdown = view.NamespaceBreakdown[:6]
	}

	sort.SliceStable(problematicPods, func(i, j int) bool {
		if problematicPods[i].Restarts != problematicPods[j].Restarts {
			return problematicPods[i].Restarts > problematicPods[j].Restarts
		}
		if podPhaseSeverity(problematicPods[i].Phase) != podPhaseSeverity(problematicPods[j].Phase) {
			return podPhaseSeverity(problematicPods[i].Phase) > podPhaseSeverity(problematicPods[j].Phase)
		}
		if problematicPods[i].AgeSeconds != problematicPods[j].AgeSeconds {
			return problematicPods[i].AgeSeconds > problematicPods[j].AgeSeconds
		}
		if problematicPods[i].Namespace != problematicPods[j].Namespace {
			return problematicPods[i].Namespace < problematicPods[j].Namespace
		}
		return problematicPods[i].Name < problematicPods[j].Name
	})
	if len(problematicPods) > 8 {
		problematicPods = problematicPods[:8]
	}
	view.ProblematicPods = problematicPods
	return view
}

func normalizedPodPhase(phase string) string {
	trimmed := strings.TrimSpace(phase)
	if trimmed == "" {
		return "Unknown"
	}
	return trimmed
}

func podNeedsAttention(item domainresource.PodView) bool {
	if item.Restarts > 0 {
		return true
	}
	switch normalizedPodPhase(item.Phase) {
	case "Pending", "Failed", "Unknown":
		return true
	default:
		return false
	}
}

func podPhaseSeverity(phase string) int {
	switch normalizedPodPhase(phase) {
	case "Failed":
		return 4
	case "Pending":
		return 3
	case "Unknown":
		return 2
	case "Running":
		return 1
	default:
		return 0
	}
}

func mapPodDetail(item corev1.Pod, decision domainaccess.Decision) domainresource.PodDetailView {
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Containers))
	statusMap := make(map[string]corev1.ContainerStatus, len(item.Status.ContainerStatuses))
	for _, status := range item.Status.ContainerStatuses {
		statusMap[status.Name] = status
	}
	for _, container := range item.Spec.Containers {
		containerStatus := statusMap[container.Name]
		containers = append(containers, domainresource.WorkloadContainerView{
			Name:         container.Name,
			Image:        container.Image,
			Ready:        containerStatus.Ready,
			RestartCount: containerStatus.RestartCount,
			State:        containerState(containerStatus.State),
			LastState:    containerState(containerStatus.LastTerminationState),
		})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	startTime := ""
	if item.Status.StartTime != nil {
		startTime = item.Status.StartTime.Time.Format(time.RFC3339)
	}
	return domainresource.PodDetailView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		Phase:              string(item.Status.Phase),
		PodIP:              item.Status.PodIP,
		HostIP:             item.Status.HostIP,
		NodeName:           item.Spec.NodeName,
		ServiceAccountName: item.Spec.ServiceAccountName,
		QOSClass:           string(item.Status.QOSClass),
		StartTime:          startTime,
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Containers:         containers,
		Conditions:         conditions,
		AllowedActions:     stringifyActions(decision.AllowedActions),
	}
}

func mapDeployment(item appsv1.Deployment, decision domainaccess.Decision) domainresource.DeploymentView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.DeploymentView{Name: item.Name, Namespace: item.Namespace, Labels: item.Labels, DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas, UpdatedReplicas: item.Status.UpdatedReplicas, Available: item.Status.AvailableReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}

func mapDeploymentDetail(item appsv1.Deployment, decision domainaccess.Decision) domainresource.DeploymentDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Template.Spec.Containers))
	for _, container := range item.Spec.Template.Spec.Containers {
		containers = append(containers, domainresource.WorkloadContainerView{
			Name:  container.Name,
			Image: container.Image,
		})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentDetailView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		DesiredReplicas:    desired,
		ReadyReplicas:      item.Status.ReadyReplicas,
		UpdatedReplicas:    item.Status.UpdatedReplicas,
		AvailableReplicas:  item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration,
		Strategy:           string(item.Spec.Strategy.Type),
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Selector:           item.Spec.Selector.MatchLabels,
		Containers:         containers,
		Conditions:         conditions,
		AllowedActions:     stringifyActions(decision.AllowedActions),
	}
}

func mapDeploymentRolloutStatus(item appsv1.Deployment) domainresource.DeploymentRolloutStatusView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	status := "progressing"
	message := "rollout is progressing"
	for _, condition := range item.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue && item.Status.UpdatedReplicas == desired && item.Status.AvailableReplicas == desired {
			status = "healthy"
			message = "deployment is fully available"
		}
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			status = "degraded"
			message = condition.Message
		}
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentRolloutStatusView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		Revision:           item.Annotations["deployment.kubernetes.io/revision"],
		Status:             status,
		Message:            message,
		DesiredReplicas:    desired,
		UpdatedReplicas:    item.Status.UpdatedReplicas,
		ReadyReplicas:      item.Status.ReadyReplicas,
		AvailableReplicas:  item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration,
		Conditions:         conditions,
	}
}

func mapStatefulSet(item appsv1.StatefulSet, decision domainaccess.Decision) domainresource.StatefulSetView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetView{Name: item.Name, Namespace: item.Namespace, ServiceName: item.Spec.ServiceName, DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas, CurrentReplicas: item.Status.CurrentReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}

func mapStatefulSetDetail(item appsv1.StatefulSet, decision domainaccess.Decision) domainresource.StatefulSetDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetDetailView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ServiceName:     item.Spec.ServiceName,
		DesiredReplicas: desired,
		ReadyReplicas:   item.Status.ReadyReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		UpdateStrategy:  string(item.Spec.UpdateStrategy.Type),
		CurrentRevision: item.Status.CurrentRevision,
		UpdateRevision:  item.Status.UpdateRevision,
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        item.Spec.Selector.MatchLabels,
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}

func mapDaemonSet(item appsv1.DaemonSet, decision domainaccess.Decision) domainresource.DaemonSetView {
	return domainresource.DaemonSetView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		DesiredNumber:   item.Status.DesiredNumberScheduled,
		CurrentNumber:   item.Status.CurrentNumberScheduled,
		ReadyNumber:     item.Status.NumberReady,
		AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber:   item.Status.UpdatedNumberScheduled,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}

func mapDaemonSetDetail(item appsv1.DaemonSet, decision domainaccess.Decision) domainresource.DaemonSetDetailView {
	selector := map[string]string{}
	if item.Spec.Selector != nil {
		selector = item.Spec.Selector.MatchLabels
	}
	return domainresource.DaemonSetDetailView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		DesiredNumber:   item.Status.DesiredNumberScheduled,
		CurrentNumber:   item.Status.CurrentNumberScheduled,
		ReadyNumber:     item.Status.NumberReady,
		AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber:   item.Status.UpdatedNumberScheduled,
		UpdateStrategy:  string(item.Spec.UpdateStrategy.Type),
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        selector,
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}

func mapJob(item batchv1.Job, decision domainaccess.Decision) domainresource.JobView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	return domainresource.JobView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Completions:    completions,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		Active:         item.Status.Active,
		CompletionMode: completionMode,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapJobDetail(item batchv1.Job, decision domainaccess.Decision) domainresource.JobDetailView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	parallelism := int32(1)
	if item.Spec.Parallelism != nil {
		parallelism = *item.Spec.Parallelism
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	startTime := ""
	if item.Status.StartTime != nil {
		startTime = item.Status.StartTime.Time.Format(time.RFC3339)
	}
	completionTime := ""
	if item.Status.CompletionTime != nil {
		completionTime = item.Status.CompletionTime.Time.Format(time.RFC3339)
	}
	return domainresource.JobDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Completions:    completions,
		Parallelism:    parallelism,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		Active:         item.Status.Active,
		CompletionMode: completionMode,
		StartTime:      startTime,
		CompletionTime: completionTime,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapCronJob(item batchv1.CronJob, decision domainaccess.Decision) domainresource.CronJobView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Time.Format(time.RFC3339)
	}
	return domainresource.CronJobView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Schedule:         item.Spec.Schedule,
		Suspend:          item.Spec.Suspend != nil && *item.Spec.Suspend,
		ActiveJobs:       int32(len(item.Status.Active)),
		LastScheduleTime: lastScheduleTime,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}

func mapCronJobDetail(item batchv1.CronJob, decision domainaccess.Decision) domainresource.CronJobDetailView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Time.Format(time.RFC3339)
	}
	timeZone := ""
	if item.Spec.TimeZone != nil {
		timeZone = *item.Spec.TimeZone
	}
	return domainresource.CronJobDetailView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		Schedule:          item.Spec.Schedule,
		Suspend:           item.Spec.Suspend != nil && *item.Spec.Suspend,
		ActiveJobs:        int32(len(item.Status.Active)),
		LastScheduleTime:  lastScheduleTime,
		ConcurrencyPolicy: string(item.Spec.ConcurrencyPolicy),
		TimeZone:          timeZone,
		Labels:            item.Labels,
		Annotations:       item.Annotations,
		AllowedActions:    stringifyActions(decision.AllowedActions),
	}
}

func mapService(item corev1.Service, decision domainaccess.Decision) domainresource.ServiceView {
	ports := make([]string, 0, len(item.Spec.Ports))
	for _, port := range item.Spec.Ports {
		name := port.Name
		if name != "" {
			name = name + ":"
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, port.Port, strings.ToLower(string(port.Protocol))))
	}
	return domainresource.ServiceView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Spec.Type), ClusterIP: item.Spec.ClusterIP, Ports: ports, Selector: item.Spec.Selector, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}

func mapIngress(item networkingv1.Ingress, decision domainaccess.Decision) domainresource.IngressView {
	hosts := make([]string, 0, len(item.Spec.Rules))
	for _, rule := range item.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	addresses := make([]string, 0, len(item.Status.LoadBalancer.Ingress))
	for _, ingress := range item.Status.LoadBalancer.Ingress {
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
			continue
		}
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
	}
	className := ""
	if item.Spec.IngressClassName != nil {
		className = *item.Spec.IngressClassName
	}
	return domainresource.IngressView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ClassName:       className,
		Hosts:           hosts,
		Address:         strings.Join(addresses, ", "),
		BackendServices: extractIngressBackendServices(item),
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}

func mapPersistentVolumeClaim(item corev1.PersistentVolumeClaim, decision domainaccess.Decision) domainresource.PersistentVolumeClaimView {
	requested := ""
	if quantity, ok := item.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		requested = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	return domainresource.PersistentVolumeClaimView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Status:         string(item.Status.Phase),
		VolumeName:     item.Spec.VolumeName,
		StorageClass:   storageClass,
		AccessModes:    accessModes,
		Requested:      requested,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapPersistentVolume(item corev1.PersistentVolume, decision domainaccess.Decision) domainresource.PersistentVolumeView {
	capacity := ""
	if quantity, ok := item.Spec.Capacity[corev1.ResourceStorage]; ok {
		capacity = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	claimRef := ""
	if item.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name)
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeView{
		Name:           item.Name,
		Status:         string(item.Status.Phase),
		StorageClass:   item.Spec.StorageClassName,
		ClaimRef:       claimRef,
		AccessModes:    accessModes,
		Capacity:       capacity,
		ReclaimPolicy:  string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode:     volumeMode,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapStorageClass(item storagev1.StorageClass, decision domainaccess.Decision) domainresource.StorageClassView {
	reclaimPolicy := ""
	if item.ReclaimPolicy != nil {
		reclaimPolicy = string(*item.ReclaimPolicy)
	}
	volumeBindingMode := ""
	if item.VolumeBindingMode != nil {
		volumeBindingMode = string(*item.VolumeBindingMode)
	}
	allowVolumeExpansion := false
	if item.AllowVolumeExpansion != nil {
		allowVolumeExpansion = *item.AllowVolumeExpansion
	}
	return domainresource.StorageClassView{
		Name:                 item.Name,
		Provisioner:          item.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    volumeBindingMode,
		AllowVolumeExpansion: allowVolumeExpansion,
		Parameters:           item.Parameters,
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
		AllowedActions:       stringifyActions(decision.AllowedActions),
	}
}

func mapCRD(item unstructured.Unstructured) domainresource.CRDView {
	group, _, _ := unstructured.NestedString(item.Object, "spec", "group")
	scope, _, _ := unstructured.NestedString(item.Object, "spec", "scope")
	kind, _, _ := unstructured.NestedString(item.Object, "spec", "names", "kind")
	plural, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
	versionItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "versions")
	versions := make([]string, 0, len(versionItems))
	for _, raw := range versionItems {
		value, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := value["name"].(string)
		if strings.TrimSpace(name) != "" {
			versions = append(versions, name)
		}
	}
	return domainresource.CRDView{
		Name:       item.GetName(),
		Group:      group,
		Scope:      scope,
		Kind:       kind,
		Plural:     plural,
		Versions:   versions,
		AgeSeconds: secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapHelmRelease(name, namespace string, labels map[string]string, createdAt time.Time, storageDriver string) domainresource.HelmReleaseView {
	releaseName := strings.TrimSpace(labels["name"])
	if releaseName == "" {
		releaseName = parseHelmReleaseName(name)
	}
	revision := strings.TrimSpace(labels["version"])
	if revision == "" {
		revision = parseHelmRevision(name)
	}
	status := strings.TrimSpace(labels["status"])
	if status == "" {
		status = "unknown"
	}
	chart := strings.TrimSpace(labels["helm.sh/chart"])
	appVersion := strings.TrimSpace(labels["app.kubernetes.io/version"])
	return domainresource.HelmReleaseView{
		Name:          releaseName,
		Namespace:     namespace,
		Revision:      revision,
		Status:        status,
		Chart:         chart,
		AppVersion:    appVersion,
		StorageDriver: storageDriver,
		AgeSeconds:    secondsSince(createdAt),
	}
}

func extractIngressBackendServices(item networkingv1.Ingress) []string {
	services := make([]string, 0, len(item.Spec.Rules)+1)
	seen := make(map[string]struct{}, len(item.Spec.Rules)+1)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		services = append(services, name)
	}
	if item.Spec.DefaultBackend != nil && item.Spec.DefaultBackend.Service != nil {
		add(item.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				add(path.Backend.Service.Name)
			}
		}
	}
	sort.Strings(services)
	return services
}

func mapNode(item corev1.Node, decision domainaccess.Decision) domainresource.NodeView {
	roles := make([]string, 0)
	for key := range item.Labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(key, "node-role.kubernetes.io/"))
		}
	}
	sort.Strings(roles)
	internalIP := ""
	for _, address := range item.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			internalIP = address.Address
			break
		}
	}
	status := "unknown"
	for _, condition := range item.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				status = "ready"
			} else {
				status = "not_ready"
			}
			break
		}
	}
	return domainresource.NodeView{Name: item.Name, Status: status, Roles: roles, Version: item.Status.NodeInfo.KubeletVersion, InternalIP: internalIP, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}

func mapClusterEvent(item corev1.Event) domainresource.ClusterEventView {
	last := item.LastTimestamp.Time
	if last.IsZero() {
		last = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	return domainresource.ClusterEventView{Name: item.Name, Namespace: item.Namespace, Type: item.Type, Reason: item.Reason, InvolvedKind: item.InvolvedObject.Kind, InvolvedName: item.InvolvedObject.Name, Message: item.Message, Count: item.Count, LastTimestamp: last.UTC().Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func secondsSince(timestamp time.Time) int64 {
	return int64(time.Since(timestamp).Seconds())
}

func stringifyActions(actions []domainaccess.Action) []string {
	values := make([]string, 0, len(actions))
	for _, action := range actions {
		values = append(values, string(action))
	}
	return values
}

func displayNamespace(namespace string) string {
	if strings.TrimSpace(namespace) == "" {
		return "all-namespaces"
	}
	return namespace
}

func containerState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return "running"
	case state.Waiting != nil:
		if state.Waiting.Reason != "" {
			return "waiting:" + state.Waiting.Reason
		}
		return "waiting"
	case state.Terminated != nil:
		if state.Terminated.Reason != "" {
			return "terminated:" + state.Terminated.Reason
		}
		return "terminated"
	default:
		return ""
	}
}

func ownedByDeployment(owners []metav1.OwnerReference, uid types.UID) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == "Deployment" {
			return true
		}
	}
	return false
}

func (s *Service) listPodViews(ctx context.Context, clusterID, namespace string, connection domaincluster.Connection, decision domainaccess.Decision, includeUsage bool) ([]domainresource.PodView, string, error) {
	var (
		items  []domainresource.PodView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, "", err
		}
		items, err = client.ListPods(ctx, namespace)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectPods(ctx, clusterID, namespace)
		if err != nil {
			return nil, "", err
		}
		items = make([]domainresource.PodView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPod(item, decision))
		}
		if includeUsage && shouldPopulatePodUsageSummaries(namespace) {
			metricsCtx, metricsCancel := context.WithTimeout(ctx, 1200*time.Millisecond)
			if metrics := s.listPodUsageSummaries(metricsCtx, clusterID, namespace, items); len(metrics) > 0 {
				for index := range items {
					if usage, ok := metrics[podMetricsKey(items[index].Namespace, items[index].Name)]; ok {
						items[index].CPU = usage.CPU
						items[index].Memory = usage.Memory
					}
				}
			}
			metricsCancel()
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PodView) string { return item.Namespace })
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	return items, source, nil
}

func populateAllowedActionsNamespaces(items []domainresource.NamespaceView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsPods(items []domainresource.PodView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsDeployments(items []domainresource.DeploymentView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsStatefulSets(items []domainresource.StatefulSetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsDaemonSets(items []domainresource.DaemonSetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsJobs(items []domainresource.JobView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsCronJobs(items []domainresource.CronJobView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsServices(items []domainresource.ServiceView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsIngresses(items []domainresource.IngressView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsNodes(items []domainresource.NodeView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsPersistentVolumeClaims(items []domainresource.PersistentVolumeClaimView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsPersistentVolumes(items []domainresource.PersistentVolumeView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsStorageClasses(items []domainresource.StorageClassView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsCRDs(items []domainresource.CRDView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsHelmReleases(items []domainresource.HelmReleaseView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func parseHelmReleaseName(name string) string {
	trimmed := strings.TrimPrefix(name, "sh.helm.release.v1.")
	if trimmed == name {
		return name
	}
	index := strings.LastIndex(trimmed, ".v")
	if index <= 0 {
		return trimmed
	}
	return trimmed[:index]
}

func parseHelmRevision(name string) string {
	index := strings.LastIndex(name, ".v")
	if index <= 0 {
		return ""
	}
	return name[index+2:]
}

func dedupeHelmReleases(items []domainresource.HelmReleaseView) []domainresource.HelmReleaseView {
	seen := make(map[string]struct{}, len(items))
	result := make([]domainresource.HelmReleaseView, 0, len(items))
	for _, item := range items {
		key := item.Namespace + "/" + item.Name + "/" + item.Revision
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}
