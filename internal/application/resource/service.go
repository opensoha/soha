package resource

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"

	appaccess "github.com/soha/soha/internal/application/access"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	domainresource "github.com/soha/soha/internal/domain/resource"
	domainsettings "github.com/soha/soha/internal/domain/settings"
	agentinfra "github.com/soha/soha/internal/infrastructure/agent"
	informerinfra "github.com/soha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
	"github.com/soha/soha/internal/platform/streamlimit"
	portforwardrepo "github.com/soha/soha/internal/repository/portforward"
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

type Service struct {
	clusters     *k8sinfra.Manager
	cache        *informerinfra.Service
	agents       *agentinfra.Registry
	resolver     ConnectionResolver
	authorizer   domainaccess.Authorizer
	permissions  *appaccess.PermissionResolver
	audit        AuditRecorder
	operations   OperationRecorder
	settings     MonitoringSettingsResolver
	httpClient   *http.Client
	portForwards PortForwardRepository
}

type PortForwardRepository interface {
	List(ctx context.Context) ([]portforwardrepo.Record, error)
	Upsert(ctx context.Context, rec portforwardrepo.Record) error
	Delete(ctx context.Context, sessionID string) error
	MarkStatus(ctx context.Context, sessionID, status, lastErr string) error
}

type crdResourceDefinition struct {
	CRDName    string
	Kind       string
	Group      string
	Version    string
	Resource   string
	Namespaced bool
}

func New(clusters *k8sinfra.Manager, cache *informerinfra.Service, agents *agentinfra.Registry, resolver ConnectionResolver, authorizer domainaccess.Authorizer, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder, settings MonitoringSettingsResolver) *Service {
	return &Service{
		clusters:    clusters,
		cache:       cache,
		agents:      agents,
		resolver:    resolver,
		authorizer:  authorizer,
		permissions: permissions,
		audit:       audit,
		operations:  operations,
		settings:    settings,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) SetPortForwardRepository(repo PortForwardRepository) {
	s.portForwards = repo
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
		s.recordOperation(ctx, principal, "platform.namespace.create", clusterID, input.Name, "Namespace", input.Name, "created namespace", nil)
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
		s.recordOperation(ctx, principal, "platform.namespace.update", clusterID, namespace, "Namespace", namespace, "updated namespace", nil)
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
		s.recordOperation(ctx, principal, "platform.namespace.delete", clusterID, namespace, "Namespace", namespace, "deleted namespace", nil)
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
		s.recordOperation(ctx, principal, "platform.node.update", clusterID, "", "Node", nodeName, "updated node labels and taints", nil)
		return view, nil
	}
}

func (s *Service) GetNodeYAML(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: node yaml is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err = s.getDirectNodeYAML(ctx, clusterID, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "Node", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed node yaml via %s", source))
	return item, nil
}

func (s *Service) ApplyNodeYAML(ctx context.Context, principal domainidentity.Principal, clusterID, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, "", "Node", name, content)
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
		s.recordOperation(ctx, principal, "platform.node.delete", clusterID, "", "Node", nodeName, "deleted node object", nil)
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
		item = s.buildPodDetailView(ctx, clusterID, decision, *rawItem)
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
	if err := s.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentRollback); err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionRollback)
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
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.rollbackDirectDeployment(ctx, clusterID, namespace, name, revision); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, err
		}
		source = "live"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "success", fmt.Sprintf("rolled back deployment to revision %s via %s", revision, source))
	s.recordOperation(ctx, principal, "platform.deployment.rollback", connection.Summary.ID, namespace, "Deployment", name, fmt.Sprintf("rolled back deployment to revision %s via %s", revision, source), map[string]any{"revision": revision})
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

func (s *Service) ListReplicaSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ReplicaSetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ReplicaSet", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ReplicaSetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListReplicaSets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectReplicaSets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ReplicaSetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapReplicaSet(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ReplicaSetView) string { return item.Namespace })
	populateAllowedActionsReplicaSets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ReplicaSet", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed replicasets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListConfigMaps(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ConfigMapView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ConfigMap", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ConfigMapView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListConfigMaps(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectConfigMaps(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ConfigMapView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapConfigMap(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ConfigMapView) string { return item.Namespace })
	populateAllowedActionsConfigMaps(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ConfigMap", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed configmaps via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListSecrets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.SecretView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Secret", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.SecretView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListSecrets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectSecrets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.SecretView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapSecret(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.SecretView) string { return item.Namespace })
	populateAllowedActionsSecrets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Secret", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed secrets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetConfigMapDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ConfigMapDetailView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "ConfigMap", domainaccess.ActionView)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.ConfigMapDetailView{}, fmt.Errorf("%w: configmap detail is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().ConfigMaps(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	binaryData := make(map[string]string, len(item.BinaryData))
	for k, v := range item.BinaryData {
		binaryData[k] = base64.StdEncoding.EncodeToString(v)
	}
	immutable := item.Immutable != nil && *item.Immutable
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ConfigMap", name, string(domainaccess.ActionView), "success", "viewed configmap detail")
	return domainresource.ConfigMapDetailView{
		Name:        item.Name,
		Namespace:   item.Namespace,
		Labels:      item.Labels,
		Annotations: item.Annotations,
		Data:        item.Data,
		BinaryData:  binaryData,
		Immutable:   immutable,
		CreatedAt:   item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
	}, nil
}

func (s *Service) GetSecretDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.SecretDetailView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Secret", domainaccess.ActionView)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.SecretDetailView{}, fmt.Errorf("%w: secret detail is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.SecretDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Secrets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	data := make(map[string]string, len(item.Data))
	for k, v := range item.Data {
		data[k] = base64.StdEncoding.EncodeToString(v)
	}
	immutable := item.Immutable != nil && *item.Immutable
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Secret", name, string(domainaccess.ActionView), "success", "viewed secret detail")
	return domainresource.SecretDetailView{
		Name:        item.Name,
		Namespace:   item.Namespace,
		Type:        string(item.Type),
		Labels:      item.Labels,
		Annotations: item.Annotations,
		Data:        data,
		Immutable:   immutable,
		CreatedAt:   item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
	}, nil
}

// CreateResourceFromYAML creates a new resource in the cluster from YAML content
// for any kind registered in resourceGVRForKind. For namespace-scoped resources,
// the namespace argument is used when metadata.namespace is empty in the YAML.
func (s *Service) CreateResourceFromYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, content string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionCreate)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if strings.TrimSpace(content) == "" {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml create is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	if item.GetKind() == "" {
		item.SetKind(kind)
	}
	if !strings.EqualFold(item.GetKind(), kind) {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml kind %s does not match target %s", apperrors.ErrInvalidArgument, item.GetKind(), kind)
	}
	if strings.TrimSpace(item.GetName()) == "" {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.name is required", apperrors.ErrInvalidArgument)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		ns := item.GetNamespace()
		if ns == "" {
			ns = namespace
			item.SetNamespace(ns)
		}
		if strings.TrimSpace(ns) == "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: namespace is required for namespace-scoped resource", apperrors.ErrInvalidArgument)
		}
		resource = bundle.Dynamic.Resource(gvr).Namespace(ns)
	} else {
		if strings.TrimSpace(item.GetNamespace()) != "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace must be empty for cluster-scoped resource", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace("")
		resource = bundle.Dynamic.Resource(gvr)
	}
	item.SetResourceVersion("")
	created, err := resource.Create(queryCtx, item, metav1.CreateOptions{})
	if err != nil {
		_ = s.recordAudit(ctx, principal, clusterID, item.GetNamespace(), kind, item.GetName(), string(domainaccess.ActionCreate), "failure", err.Error())
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(created.Object, "metadata", "managedFields")
	rendered, err := yaml.Marshal(created.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, created.GetNamespace(), kind, created.GetName(), string(domainaccess.ActionCreate), "success", "created resource from yaml")
	s.recordOperation(ctx, principal, "platform.resource.create", connection.Summary.ID, created.GetNamespace(), kind, created.GetName(), "created resource from yaml", nil)
	return domainresource.ResourceYAMLView{
		Kind:      kind,
		Name:      created.GetName(),
		Namespace: created.GetNamespace(),
		Content:   string(rendered),
	}, nil
}

func (s *Service) ListServiceAccounts(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceAccountView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ServiceAccount", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ServiceAccountView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListServiceAccounts(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectServiceAccounts(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ServiceAccountView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapServiceAccount(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ServiceAccountView) string { return item.Namespace })
	populateAllowedActionsServiceAccounts(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ServiceAccount", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed serviceaccounts via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetServiceAccountDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ServiceAccountDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.ServiceAccountDetailView{}, fmt.Errorf("%w: namespace is required for serviceaccount detail", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ServiceAccount", domainaccess.ActionView)
	if err != nil {
		return domainresource.ServiceAccountDetailView{}, err
	}
	var (
		item   domainresource.ServiceAccountDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ServiceAccountDetailView{}, err
		}
		item, err = client.GetServiceAccountDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.ServiceAccountDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectServiceAccount(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ServiceAccountDetailView{}, err
		}
		item = mapServiceAccountDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ServiceAccount", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed serviceaccount detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ListRoles(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.RoleView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Role", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.RoleView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListRoles(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectRoles(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.RoleView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapRole(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.RoleView) string { return item.Namespace })
	populateAllowedActionsRoles(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Role", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed roles via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetRoleDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.RoleDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.RoleDetailView{}, fmt.Errorf("%w: namespace is required for role detail", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Role", domainaccess.ActionView)
	if err != nil {
		return domainresource.RoleDetailView{}, err
	}
	var (
		item   domainresource.RoleDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.RoleDetailView{}, err
		}
		item, err = client.GetRoleDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.RoleDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectRole(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.RoleDetailView{}, err
		}
		item = mapRoleDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Role", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed role detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ListRoleBindings(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.RoleBindingView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "RoleBinding", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.RoleBindingView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListRoleBindings(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectRoleBindings(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.RoleBindingView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapRoleBinding(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.RoleBindingView) string { return item.Namespace })
	populateAllowedActionsRoleBindings(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "RoleBinding", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed rolebindings via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetRoleBindingDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.RoleBindingDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.RoleBindingDetailView{}, fmt.Errorf("%w: namespace is required for rolebinding detail", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "RoleBinding", domainaccess.ActionView)
	if err != nil {
		return domainresource.RoleBindingDetailView{}, err
	}
	var (
		item   domainresource.RoleBindingDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.RoleBindingDetailView{}, err
		}
		item, err = client.GetRoleBindingDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.RoleBindingDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectRoleBinding(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.RoleBindingDetailView{}, err
		}
		item = mapRoleBindingDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "RoleBinding", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed rolebinding detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ListHorizontalPodAutoscalers(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HorizontalPodAutoscalerView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HorizontalPodAutoscaler", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.HorizontalPodAutoscalerView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHorizontalPodAutoscalers(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectHorizontalPodAutoscalers(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.HorizontalPodAutoscalerView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapHorizontalPodAutoscaler(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.HorizontalPodAutoscalerView) string { return item.Namespace })
	populateAllowedActionsHorizontalPodAutoscalers(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HorizontalPodAutoscaler", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed hpas via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListPodDisruptionBudgets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodDisruptionBudgetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "PodDisruptionBudget", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PodDisruptionBudgetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPodDisruptionBudgets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPodDisruptionBudgets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PodDisruptionBudgetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPodDisruptionBudget(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PodDisruptionBudgetView) string { return item.Namespace })
	populateAllowedActionsPodDisruptionBudgets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "PodDisruptionBudget", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pod disruption budgets via %s in namespace %s", source, displayNamespace(namespace)))
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

func (s *Service) ListEndpointSlices(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.EndpointSliceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "EndpointSlice", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.EndpointSliceView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListEndpointSlices(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectEndpointSlices(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.EndpointSliceView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapEndpointSlice(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.EndpointSliceView) string { return item.Namespace })
	populateAllowedActionsEndpointSlices(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "EndpointSlice", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed endpoint slices via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListNetworkPolicies(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.NetworkPolicyView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "NetworkPolicy", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.NetworkPolicyView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListNetworkPolicies(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectNetworkPolicies(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.NetworkPolicyView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapNetworkPolicy(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.NetworkPolicyView) string { return item.Namespace })
	populateAllowedActionsNetworkPolicies(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "NetworkPolicy", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed network policies via %s in namespace %s", source, displayNamespace(namespace)))
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

func (s *Service) GetPersistentVolumeClaimDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PersistentVolumeClaimDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.PersistentVolumeClaimDetailView{}, fmt.Errorf("%w: namespace is required for persistentvolumeclaim detail", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "PersistentVolumeClaim", domainaccess.ActionView)
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.PersistentVolumeClaimDetailView{}, fmt.Errorf("%w: persistentvolumeclaim detail is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	}
	rawItem, err := s.getDirectPersistentVolumeClaim(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	item := mapPersistentVolumeClaimDetail(*rawItem, decision)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "PersistentVolumeClaim", name, string(domainaccess.ActionView), "success", "viewed persistentvolumeclaim detail")
	return item, nil
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

func (s *Service) GetPersistentVolumeDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.PersistentVolumeDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "PersistentVolume", domainaccess.ActionView)
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.PersistentVolumeDetailView{}, fmt.Errorf("%w: persistentvolume detail is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	}
	rawItem, err := s.getDirectPersistentVolume(ctx, clusterID, name)
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	item := mapPersistentVolumeDetail(*rawItem, decision)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "PersistentVolume", name, string(domainaccess.ActionView), "success", "viewed persistentvolume detail")
	return item, nil
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

func (s *Service) GetStorageClassDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.StorageClassDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "StorageClass", domainaccess.ActionView)
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.StorageClassDetailView{}, fmt.Errorf("%w: storageclass detail is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	}
	rawItem, err := s.getDirectStorageClass(ctx, clusterID, name)
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	item := mapStorageClassDetail(*rawItem, decision)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "StorageClass", name, string(domainaccess.ActionView), "success", "viewed storageclass detail")
	return item, nil
}

func (s *Service) ListIngressClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.IngressClassView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "IngressClass", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.IngressClassView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListIngressClasses(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectIngressClasses(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.IngressClassView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapIngressClass(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsIngressClasses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "IngressClass", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed ingressclasses via %s", source))
	return items, nil
}

func (s *Service) ListPriorityClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PriorityClassView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "PriorityClass", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PriorityClassView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPriorityClasses(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPriorityClasses(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PriorityClassView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPriorityClass(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsPriorityClasses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "PriorityClass", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed priorityclasses via %s", source))
	return items, nil
}

func (s *Service) ListRuntimeClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.RuntimeClassView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "RuntimeClass", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.RuntimeClassView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListRuntimeClasses(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectRuntimeClasses(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.RuntimeClassView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapRuntimeClass(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsRuntimeClasses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "RuntimeClass", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed runtimeclasses via %s", source))
	return items, nil
}

func (s *Service) ListClusterRoles(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.ClusterRoleView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "ClusterRole", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ClusterRoleView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListClusterRoles(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectClusterRoles(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ClusterRoleView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapClusterRole(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsClusterRoles(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "ClusterRole", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed clusterroles via %s", source))
	return items, nil
}

func (s *Service) GetClusterRoleDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.ClusterRoleDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "ClusterRole", domainaccess.ActionView)
	if err != nil {
		return domainresource.ClusterRoleDetailView{}, err
	}
	var (
		item   domainresource.ClusterRoleDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ClusterRoleDetailView{}, err
		}
		item, err = client.GetClusterRoleDetail(ctx, name)
		if err != nil {
			return domainresource.ClusterRoleDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectClusterRole(ctx, clusterID, name)
		if err != nil {
			return domainresource.ClusterRoleDetailView{}, err
		}
		item = mapClusterRoleDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "ClusterRole", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed clusterrole detail via %s", source))
	return item, nil
}

func (s *Service) ListClusterRoleBindings(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.ClusterRoleBindingView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "ClusterRoleBinding", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ClusterRoleBindingView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListClusterRoleBindings(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectClusterRoleBindings(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ClusterRoleBindingView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapClusterRoleBinding(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsClusterRoleBindings(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "ClusterRoleBinding", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed clusterrolebindings via %s", source))
	return items, nil
}

func (s *Service) GetClusterRoleBindingDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.ClusterRoleBindingDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "ClusterRoleBinding", domainaccess.ActionView)
	if err != nil {
		return domainresource.ClusterRoleBindingDetailView{}, err
	}
	var (
		item   domainresource.ClusterRoleBindingDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ClusterRoleBindingDetailView{}, err
		}
		item, err = client.GetClusterRoleBindingDetail(ctx, name)
		if err != nil {
			return domainresource.ClusterRoleBindingDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectClusterRoleBinding(ctx, clusterID, name)
		if err != nil {
			return domainresource.ClusterRoleBindingDetailView{}, err
		}
		item = mapClusterRoleBindingDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "ClusterRoleBinding", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed clusterrolebinding detail via %s", source))
	return item, nil
}

func (s *Service) ListMutatingWebhookConfigurations(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.MutatingWebhookConfigurationView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "MutatingWebhookConfiguration", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.MutatingWebhookConfigurationView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListMutatingWebhookConfigurations(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectMutatingWebhookConfigurations(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.MutatingWebhookConfigurationView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapMutatingWebhookConfiguration(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsMutatingWebhookConfigurations(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "MutatingWebhookConfiguration", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed mutatingwebhookconfigurations via %s", source))
	return items, nil
}

func (s *Service) ListValidatingWebhookConfigurations(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "ValidatingWebhookConfiguration", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ValidatingWebhookConfigurationView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListValidatingWebhookConfigurations(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectValidatingWebhookConfigurations(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ValidatingWebhookConfigurationView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapValidatingWebhookConfiguration(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsValidatingWebhookConfigurations(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "ValidatingWebhookConfiguration", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed validatingwebhookconfigurations via %s", source))
	return items, nil
}

func (s *Service) ListResourceQuotas(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceQuotaView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ResourceQuota", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ResourceQuotaView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListResourceQuotas(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectResourceQuotas(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ResourceQuotaView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapResourceQuota(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ResourceQuotaView) string { return item.Namespace })
	populateAllowedActionsResourceQuotas(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ResourceQuota", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed resourcequotas via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListLimitRanges(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.LimitRangeView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "LimitRange", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.LimitRangeView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListLimitRanges(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectLimitRanges(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.LimitRangeView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapLimitRange(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.LimitRangeView) string { return item.Namespace })
	populateAllowedActionsLimitRanges(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "LimitRange", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed limitranges via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListLeases(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.LeaseView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Lease", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.LeaseView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListLeases(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectLeases(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.LeaseView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapLease(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.LeaseView) string { return item.Namespace })
	populateAllowedActionsLeases(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Lease", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed leases via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListReplicationControllers(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ReplicationControllerView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ReplicationController", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ReplicationControllerView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListReplicationControllers(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectReplicationControllers(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ReplicationControllerView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapReplicationController(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ReplicationControllerView) string { return item.Namespace })
	populateAllowedActionsReplicationControllers(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ReplicationController", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed replicationcontrollers via %s in namespace %s", source, displayNamespace(namespace)))
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

func (s *Service) ListCRDResources(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace string) ([]domainresource.CustomResourceView, error) {
	connection, err := s.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	definition, err := s.resolveCRDResourceDefinition(ctx, clusterID, crdName)
	if err != nil {
		return nil, err
	}
	_, decision, err := s.authorize(ctx, principal, clusterID, normalizeCustomResourceNamespaceForAuth(namespace, definition.Namespaced), definition.Kind, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.CustomResourceView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return nil, fmt.Errorf("%w: custom-resource listing is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		items, err = s.listDirectCRDResources(ctx, clusterID, definition, namespace, decision)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, normalizeCustomResourceNamespaceForAudit(namespace, definition.Namespaced), definition.Kind, "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed custom resources for crd %s via %s", crdName, source))
	return items, nil
}

func (s *Service) CreateCRDResourceFromYAML(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, content string) (domainresource.ResourceYAMLView, error) {
	connection, err := s.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	definition, err := s.resolveCRDResourceDefinition(ctx, clusterID, crdName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	item, effectiveNamespace, err := buildCustomResourceFromYAML(definition, content, namespace, "")
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, _, err := s.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionCreate); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: custom-resource create is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		created, err := s.createDirectCustomResource(ctx, clusterID, definition, item, effectiveNamespace)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, effectiveNamespace, definition.Kind, item.GetName(), string(domainaccess.ActionCreate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, created.Name, string(domainaccess.ActionCreate), "success", "created custom resource from yaml")
		s.recordOperation(ctx, principal, "platform.custom_resource.create", connection.Summary.ID, effectiveNamespace, definition.Kind, created.Name, "created custom resource from yaml", map[string]any{"crdName": crdName})
		return created, nil
	}
}

func (s *Service) GetCRDResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, err := s.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	definition, err := s.resolveCRDResourceDefinition(ctx, clusterID, crdName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	effectiveNamespace, err := requiredCustomResourceNamespace(definition, namespace)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, _, err := s.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionView); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: custom-resource yaml view is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err := s.getDirectCustomResourceYAML(ctx, clusterID, definition, effectiveNamespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionView), "success", "viewed custom resource yaml")
		return item, nil
	}
}

func (s *Service) ApplyCRDResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	connection, err := s.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	definition, err := s.resolveCRDResourceDefinition(ctx, clusterID, crdName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	item, effectiveNamespace, err := buildCustomResourceFromYAML(definition, content, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, _, err := s.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionUpdate); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: custom-resource yaml apply is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		updated, err := s.applyDirectCustomResourceYAML(ctx, clusterID, definition, item, effectiveNamespace, name)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionUpdate), "success", "applied custom resource yaml")
		s.recordOperation(ctx, principal, "platform.custom_resource.apply", connection.Summary.ID, effectiveNamespace, definition.Kind, name, "applied custom resource yaml", map[string]any{"crdName": crdName})
		return updated, nil
	}
}

func (s *Service) DeleteCRDResource(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, name string) error {
	connection, err := s.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return err
	}
	definition, err := s.resolveCRDResourceDefinition(ctx, clusterID, crdName)
	if err != nil {
		return err
	}
	effectiveNamespace, err := requiredCustomResourceNamespace(definition, namespace)
	if err != nil {
		return err
	}
	if _, _, err := s.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionDelete); err != nil {
		return err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return fmt.Errorf("%w: custom-resource delete is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		if err := s.deleteDirectCustomResource(ctx, clusterID, definition, effectiveNamespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionDelete), "success", "deleted custom resource")
		s.recordOperation(ctx, principal, "platform.custom_resource.delete", connection.Summary.ID, effectiveNamespace, definition.Kind, name, "deleted custom resource", map[string]any{"crdName": crdName})
		return nil
	}
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
	if err := s.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentRestart); err != nil {
		return err
	}
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
	s.recordOperation(ctx, principal, "platform.deployment.restart", clusterID, namespace, "Deployment", name, fmt.Sprintf("restarted deployment via %s", source), nil)
	return nil
}

func (s *Service) ScaleDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, replicas int32) error {
	if err := s.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentScale); err != nil {
		return err
	}
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
	s.recordOperation(ctx, principal, "platform.deployment.scale", clusterID, namespace, "Deployment", name, fmt.Sprintf("scaled deployment to %d via %s", replicas, source), map[string]any{"replicas": replicas})
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

func (s *Service) getDirectNodeYAML(ctx context.Context, clusterID, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectNode(ctx, clusterID, name)
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
		Kind:    "Node",
		Name:    name,
		Content: string(content),
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

// ApplyResourceYAMLByKind applies updated YAML for any kind registered in
// resourceGVRForKind via the dynamic client.
func (s *Service) ApplyResourceYAMLByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, kind, name, content)
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
		s.recordOperation(ctx, principal, "platform.resource.apply", connection.Summary.ID, namespace, kind, name, "applied resource yaml", nil)
		return item, nil
	}
}

// GetResourceYAML fetches the YAML representation of any kind registered in
// resourceGVRForKind via the dynamic client. Namespace may be empty for
// cluster-scoped kinds.
func (s *Service) GetResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml view is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		item, err := s.getDirectResourceYAMLByKind(ctx, clusterID, namespace, kind, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed resource yaml")
		return item, nil
	}
}

// DeleteResourceByKind deletes any kind registered in resourceGVRForKind.
func (s *Service) DeleteResourceByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return fmt.Errorf("%w: delete is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
	default:
		if err := s.deleteDirectResourceByKind(ctx, clusterID, namespace, kind, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionDelete), "success", "deleted resource")
		s.recordOperation(ctx, principal, "platform.resource.delete", connection.Summary.ID, namespace, kind, name, "deleted resource", nil)
		return nil
	}
}

func (s *Service) getDirectResourceYAMLByKind(ctx context.Context, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		resource = bundle.Dynamic.Resource(gvr).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(gvr)
	}
	item, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
	content, err := yaml.Marshal(item.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      kind,
		Name:      name,
		Namespace: item.GetNamespace(),
		Content:   string(content),
	}, nil
}

func (s *Service) getDirectCustomResourceYAML(ctx context.Context, clusterID string, definition crdResourceDefinition, namespace, name string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if definition.Namespaced {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource())
	}
	item, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
	content, err := yaml.Marshal(item.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      definition.Kind,
		Name:      item.GetName(),
		Namespace: item.GetNamespace(),
		Content:   string(content),
	}, nil
}

func (s *Service) deleteDirectResourceByKind(ctx context.Context, clusterID, namespace, kind, name string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		resource = bundle.Dynamic.Resource(gvr).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(gvr)
	}
	return resource.Delete(queryCtx, name, metav1.DeleteOptions{})
}

func (s *Service) deleteDirectCustomResource(ctx context.Context, clusterID string, definition crdResourceDefinition, namespace, name string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if definition.Namespaced {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource())
	}
	return resource.Delete(queryCtx, name, metav1.DeleteOptions{})
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
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		if item.GetNamespace() == "" {
			item.SetNamespace(namespace)
		}
		if item.GetNamespace() != namespace {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace does not match target resource", apperrors.ErrInvalidArgument)
		}
		resource = bundle.Dynamic.Resource(gvr).Namespace(namespace)
	} else {
		if strings.TrimSpace(item.GetNamespace()) != "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace must be empty for cluster-scoped resource", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace("")
		resource = bundle.Dynamic.Resource(gvr)
	}
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
		Namespace: item.GetNamespace(),
		Content:   string(rendered),
	}, nil
}

func (s *Service) applyDirectCustomResourceYAML(ctx context.Context, clusterID string, definition crdResourceDefinition, item *unstructured.Unstructured, namespace, name string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if definition.Namespaced {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource())
	}
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
		Kind:      definition.Kind,
		Name:      updated.GetName(),
		Namespace: updated.GetNamespace(),
		Content:   string(rendered),
	}, nil
}

func (s *Service) createDirectCustomResource(ctx context.Context, clusterID string, definition crdResourceDefinition, item *unstructured.Unstructured, namespace string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if definition.Namespaced {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource())
	}
	item.SetResourceVersion("")
	created, err := resource.Create(queryCtx, item, metav1.CreateOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	rendered, err := yaml.Marshal(created.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      definition.Kind,
		Name:      created.GetName(),
		Namespace: created.GetNamespace(),
		Content:   string(rendered),
	}, nil
}

func resourceGVRForKind(kind string) (schema.GroupVersionResource, bool, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, true, nil
	case "node":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}, false, nil
	case "deployment":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true, nil
	case "statefulset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true, nil
	case "daemonset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true, nil
	case "job":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, true, nil
	case "cronjob":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true, nil
	case "configmap":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true, nil
	case "secret":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true, nil
	case "serviceaccount":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, true, nil
	case "replicationcontroller":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "replicationcontrollers"}, true, nil
	case "service":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true, nil
	case "role":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true, nil
	case "rolebinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true, nil
	case "resourcequota":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "resourcequotas"}, true, nil
	case "limitrange":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "limitranges"}, true, nil
	case "lease":
		return schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}, true, nil
	case "ingress":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true, nil
	case "endpointslice":
		return schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}, true, nil
	case "networkpolicy":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, true, nil
	case "ingressclass":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}, false, nil
	case "gatewayclass":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}, false, nil
	case "gateway":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}, true, nil
	case "httproute":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, true, nil
	case "backendtlspolicy":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "backendtlspolicies"}, true, nil
	case "grpcroute":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}, true, nil
	case "referencegrant":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "referencegrants"}, true, nil
	case "priorityclass":
		return schema.GroupVersionResource{Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses"}, false, nil
	case "runtimeclass":
		return schema.GroupVersionResource{Group: "node.k8s.io", Version: "v1", Resource: "runtimeclasses"}, false, nil
	case "clusterrole":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false, nil
	case "clusterrolebinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false, nil
	case "mutatingwebhookconfiguration":
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}, false, nil
	case "validatingwebhookconfiguration":
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}, false, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("%w: yaml apply does not support kind %s", apperrors.ErrInvalidArgument, kind)
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

func (s *Service) listDirectReplicaSets(ctx context.Context, clusterID, namespace string) ([]appsv1.ReplicaSet, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.ReplicaSet, error) {
			items, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectConfigMaps(ctx context.Context, clusterID, namespace string) ([]corev1.ConfigMap, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ConfigMap, error) {
			items, err := bundle.Typed.CoreV1().ConfigMaps(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().ConfigMaps(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectSecrets(ctx context.Context, clusterID, namespace string) ([]corev1.Secret, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.Secret, error) {
			items, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectServiceAccounts(ctx context.Context, clusterID, namespace string) ([]corev1.ServiceAccount, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ServiceAccount, error) {
			items, err := bundle.Typed.CoreV1().ServiceAccounts(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().ServiceAccounts(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) getDirectServiceAccount(ctx context.Context, clusterID, namespace, name string) (*corev1.ServiceAccount, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().ServiceAccounts(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) listDirectRoles(ctx context.Context, clusterID, namespace string) ([]rbacv1.Role, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]rbacv1.Role, error) {
			items, err := bundle.Typed.RbacV1().Roles(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.RbacV1().Roles(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) getDirectRole(ctx context.Context, clusterID, namespace, name string) (*rbacv1.Role, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().Roles(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) listDirectRoleBindings(ctx context.Context, clusterID, namespace string) ([]rbacv1.RoleBinding, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]rbacv1.RoleBinding, error) {
			items, err := bundle.Typed.RbacV1().RoleBindings(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.RbacV1().RoleBindings(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) getDirectRoleBinding(ctx context.Context, clusterID, namespace, name string) (*rbacv1.RoleBinding, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().RoleBindings(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) listDirectHorizontalPodAutoscalers(ctx context.Context, clusterID, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
			items, err := bundle.Typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectPodDisruptionBudgets(ctx context.Context, clusterID, namespace string) ([]policyv1.PodDisruptionBudget, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]policyv1.PodDisruptionBudget, error) {
			items, err := bundle.Typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
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

func (s *Service) listDirectCRDResources(ctx context.Context, clusterID string, definition crdResourceDefinition, namespace string, decision domainaccess.Decision) ([]domainresource.CustomResourceView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	if definition.Namespaced && strings.TrimSpace(namespace) == "" {
		items, err := listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]unstructured.Unstructured, error) {
			list, listErr := bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(namespace).List(queryCtx, metav1.ListOptions{})
			if listErr != nil {
				return nil, listErr
			}
			return list.Items, nil
		})
		if err != nil {
			return nil, err
		}
		views := make([]domainresource.CustomResourceView, 0, len(items))
		for _, item := range items {
			views = append(views, mapCustomResource(item, definition, decision))
		}
		return filterScopedNamespaceItems(views, decision, func(item domainresource.CustomResourceView) string { return item.Namespace }), nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if definition.Namespaced {
		effectiveNamespace, err := requiredCustomResourceNamespace(definition, namespace)
		if err != nil {
			return nil, err
		}
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(effectiveNamespace)
	} else {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource())
	}
	items, err := resource.List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.CustomResourceView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapCustomResource(item, definition, decision))
	}
	if definition.Namespaced {
		return filterScopedNamespaceItems(views, decision, func(item domainresource.CustomResourceView) string { return item.Namespace }), nil
	}
	return views, nil
}

func (s *Service) resolveCRDResourceDefinition(ctx context.Context, clusterID, crdName string) (crdResourceDefinition, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return crdResourceDefinition{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	crdGVR := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	item, err := bundle.Dynamic.Resource(crdGVR).Get(queryCtx, crdName, metav1.GetOptions{})
	if err != nil {
		return crdResourceDefinition{}, err
	}
	return parseCRDResourceDefinition(*item)
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

func (s *Service) listDirectEndpointSlices(ctx context.Context, clusterID, namespace string) ([]discoveryv1.EndpointSlice, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]discoveryv1.EndpointSlice, error) {
			items, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectNetworkPolicies(ctx context.Context, clusterID, namespace string) ([]networkingv1.NetworkPolicy, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]networkingv1.NetworkPolicy, error) {
			items, err := bundle.Typed.NetworkingV1().NetworkPolicies(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().NetworkPolicies(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
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
	return strings.TrimSpace(namespace) != ""
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

func (s *Service) getDirectPersistentVolumeClaim(ctx context.Context, clusterID, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().PersistentVolumeClaims(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
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

func (s *Service) getDirectPersistentVolume(ctx context.Context, clusterID, name string) (*corev1.PersistentVolume, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().PersistentVolumes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
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

func (s *Service) getDirectStorageClass(ctx context.Context, clusterID, name string) (*storagev1.StorageClass, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.StorageV1().StorageClasses().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) listDirectIngressClasses(ctx context.Context, clusterID string) ([]networkingv1.IngressClass, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().IngressClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectPriorityClasses(ctx context.Context, clusterID string) ([]schedulingv1.PriorityClass, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.SchedulingV1().PriorityClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectRuntimeClasses(ctx context.Context, clusterID string) ([]nodev1.RuntimeClass, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.NodeV1().RuntimeClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectClusterRoles(ctx context.Context, clusterID string) ([]rbacv1.ClusterRole, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.RbacV1().ClusterRoles().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) getDirectClusterRole(ctx context.Context, clusterID, name string) (*rbacv1.ClusterRole, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().ClusterRoles().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) listDirectClusterRoleBindings(ctx context.Context, clusterID string) ([]rbacv1.ClusterRoleBinding, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.RbacV1().ClusterRoleBindings().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) getDirectClusterRoleBinding(ctx context.Context, clusterID, name string) (*rbacv1.ClusterRoleBinding, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().ClusterRoleBindings().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) listDirectMutatingWebhookConfigurations(ctx context.Context, clusterID string) ([]admissionregistrationv1.MutatingWebhookConfiguration, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.AdmissionregistrationV1().MutatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectValidatingWebhookConfigurations(ctx context.Context, clusterID string) ([]admissionregistrationv1.ValidatingWebhookConfiguration, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectResourceQuotas(ctx context.Context, clusterID, namespace string) ([]corev1.ResourceQuota, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ResourceQuota, error) {
			items, err := bundle.Typed.CoreV1().ResourceQuotas(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().ResourceQuotas(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectLimitRanges(ctx context.Context, clusterID, namespace string) ([]corev1.LimitRange, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.LimitRange, error) {
			items, err := bundle.Typed.CoreV1().LimitRanges(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().LimitRanges(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectLeases(ctx context.Context, clusterID, namespace string) ([]coordinationv1.Lease, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]coordinationv1.Lease, error) {
			items, err := bundle.Typed.CoordinationV1().Leases(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoordinationV1().Leases(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (s *Service) listDirectReplicationControllers(ctx context.Context, clusterID, namespace string) ([]corev1.ReplicationController, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ReplicationController, error) {
			items, err := bundle.Typed.CoreV1().ReplicationControllers(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().ReplicationControllers(namespace).List(queryCtx, metav1.ListOptions{})
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
	request := s.resourceAccessRequest(ctx, principal, connection, namespace, kind, action)
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

func (s *Service) resourceAccessRequest(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace, kind string, action domainaccess.Action) domainaccess.Request {
	return domainaccess.Request{
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
}

func (s *Service) allowedActionsForResource(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace, kind string, action domainaccess.Action) []string {
	if s == nil || s.authorizer == nil {
		return nil
	}
	decision, err := s.authorizer.Authorize(ctx, s.resourceAccessRequest(ctx, principal, connection, namespace, kind, action))
	if err != nil || !decision.Allowed {
		return nil
	}
	return stringifyActions(decision.AllowedActions)
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

func (s *Service) recordOperation(ctx context.Context, principal domainidentity.Principal, operationType, clusterID, namespace, kind, name, summary string, metadata map[string]any) {
	if s.operations == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	_ = s.operations.Record(ctx, operationentry.New(
		ctx,
		principal,
		operationType,
		map[string]any{
			"module":       "platform",
			"clusterId":    clusterID,
			"namespace":    namespace,
			"resourceKind": kind,
			"resourceName": name,
			"targetId":     name,
			"targetLabel":  name,
		},
		"success",
		summary,
		metadata,
	))
}

func (s *Service) authorizeDeploymentPermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
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
	requests, limits := podResourceTotals(item)
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
		Requests:               formatResourceTotals(requests),
		Limits:                 formatResourceTotals(limits),
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
	requests, limits := podResourceTotals(item)
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
		Requests:           formatResourceTotals(requests),
		Limits:             formatResourceTotals(limits),
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Containers:         containers,
		Conditions:         conditions,
		AllowedActions:     stringifyActions(decision.AllowedActions),
	}
}

func (s *Service) buildPodDetailView(ctx context.Context, clusterID string, decision domainaccess.Decision, item corev1.Pod) domainresource.PodDetailView {
	view := mapPodDetail(item, decision)
	volumeSourceRefs := buildPodVolumeSourceRefs(item)
	view.Containers = buildDetailedPodContainers(item)
	view.Volumes = buildPodVolumes(item, volumeSourceRefs)
	view.RelatedResources = s.buildPodRelatedResources(ctx, clusterID, item, volumeSourceRefs)
	return view
}

type podVolumeSourceRefSet struct {
	configMaps      map[string]struct{}
	secrets         map[string]struct{}
	serviceAccounts map[string]struct{}
	pvcs            map[string]struct{}
}

type podRelatedResourceAccumulator struct {
	kind      string
	name      string
	namespace string
	relations map[string]struct{}
	details   map[string]struct{}
}

func buildDetailedPodContainers(item corev1.Pod) []domainresource.WorkloadContainerView {
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Containers))
	statusMap := make(map[string]corev1.ContainerStatus, len(item.Status.ContainerStatuses))
	for _, status := range item.Status.ContainerStatuses {
		statusMap[status.Name] = status
	}
	for _, container := range item.Spec.Containers {
		containerStatus := statusMap[container.Name]
		state := containerState(containerStatus.State)
		lastState := containerState(containerStatus.LastTerminationState)
		startedAt := ""
		reason := ""
		message := ""
		if containerStatus.State.Running != nil && containerStatus.State.Running.StartedAt.Time.UTC().Format(time.RFC3339) != "0001-01-01T00:00:00Z" {
			startedAt = containerStatus.State.Running.StartedAt.Time.UTC().Format(time.RFC3339)
		}
		if containerStatus.State.Waiting != nil {
			reason = containerStatus.State.Waiting.Reason
			message = containerStatus.State.Waiting.Message
		}
		if containerStatus.State.Terminated != nil {
			if reason == "" {
				reason = containerStatus.State.Terminated.Reason
			}
			if message == "" {
				message = containerStatus.State.Terminated.Message
			}
			if startedAt == "" && containerStatus.State.Terminated.StartedAt.Time.UTC().Format(time.RFC3339) != "0001-01-01T00:00:00Z" {
				startedAt = containerStatus.State.Terminated.StartedAt.Time.UTC().Format(time.RFC3339)
			}
		}
		containers = append(containers, domainresource.WorkloadContainerView{
			Name:         container.Name,
			Image:        container.Image,
			Ready:        containerStatus.Ready,
			RestartCount: containerStatus.RestartCount,
			State:        state,
			LastState:    lastState,
			ContainerID:  strings.TrimSpace(containerStatus.ContainerID),
			StartedAt:    startedAt,
			Reason:       strings.TrimSpace(reason),
			Message:      strings.TrimSpace(message),
		})
	}
	return containers
}

func buildPodVolumeSourceRefs(item corev1.Pod) podVolumeSourceRefSet {
	refs := podVolumeSourceRefSet{
		configMaps:      map[string]struct{}{},
		secrets:         map[string]struct{}{},
		serviceAccounts: map[string]struct{}{},
		pvcs:            map[string]struct{}{},
	}
	if sa := strings.TrimSpace(item.Spec.ServiceAccountName); sa != "" {
		refs.serviceAccounts[sa] = struct{}{}
	}
	for _, volume := range item.Spec.Volumes {
		if volume.ConfigMap != nil && strings.TrimSpace(volume.ConfigMap.Name) != "" {
			refs.configMaps[volume.ConfigMap.Name] = struct{}{}
		}
		if volume.Secret != nil && strings.TrimSpace(volume.Secret.SecretName) != "" {
			refs.secrets[volume.Secret.SecretName] = struct{}{}
		}
		if volume.PersistentVolumeClaim != nil && strings.TrimSpace(volume.PersistentVolumeClaim.ClaimName) != "" {
			refs.pvcs[volume.PersistentVolumeClaim.ClaimName] = struct{}{}
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if source.ConfigMap != nil && strings.TrimSpace(source.ConfigMap.Name) != "" {
					refs.configMaps[source.ConfigMap.Name] = struct{}{}
				}
				if source.Secret != nil && strings.TrimSpace(source.Secret.Name) != "" {
					refs.secrets[source.Secret.Name] = struct{}{}
				}
				if source.ServiceAccountToken != nil {
					refs.serviceAccounts[item.Spec.ServiceAccountName] = struct{}{}
				}
			}
		}
	}
	for _, container := range item.Spec.Containers {
		collectContainerEnvRefs(container, &refs)
	}
	for _, container := range item.Spec.InitContainers {
		collectContainerEnvRefs(container, &refs)
	}
	return refs
}

func collectContainerEnvRefs(container corev1.Container, refs *podVolumeSourceRefSet) {
	for _, env := range container.Env {
		if env.ValueFrom == nil {
			continue
		}
		if env.ValueFrom.ConfigMapKeyRef != nil && strings.TrimSpace(env.ValueFrom.ConfigMapKeyRef.Name) != "" {
			refs.configMaps[env.ValueFrom.ConfigMapKeyRef.Name] = struct{}{}
		}
		if env.ValueFrom.SecretKeyRef != nil && strings.TrimSpace(env.ValueFrom.SecretKeyRef.Name) != "" {
			refs.secrets[env.ValueFrom.SecretKeyRef.Name] = struct{}{}
		}
	}
	for _, envFrom := range container.EnvFrom {
		if envFrom.ConfigMapRef != nil && strings.TrimSpace(envFrom.ConfigMapRef.Name) != "" {
			refs.configMaps[envFrom.ConfigMapRef.Name] = struct{}{}
		}
		if envFrom.SecretRef != nil && strings.TrimSpace(envFrom.SecretRef.Name) != "" {
			refs.secrets[envFrom.SecretRef.Name] = struct{}{}
		}
	}
}

func buildPodVolumes(item corev1.Pod, refs podVolumeSourceRefSet) []domainresource.PodVolumeView {
	mountsByVolume := map[string][]domainresource.PodVolumeMountView{}
	appendMounts := func(containerName string, mounts []corev1.VolumeMount) {
		for _, mount := range mounts {
			if strings.TrimSpace(mount.Name) == "" {
				continue
			}
			mountsByVolume[mount.Name] = append(mountsByVolume[mount.Name], domainresource.PodVolumeMountView{
				Name:        containerName,
				MountPath:   mount.MountPath,
				SubPath:     mount.SubPath,
				ReadOnly:    mount.ReadOnly,
				Description: containerName,
			})
		}
	}
	for _, container := range item.Spec.InitContainers {
		appendMounts(container.Name, container.VolumeMounts)
	}
	for _, container := range item.Spec.Containers {
		appendMounts(container.Name, container.VolumeMounts)
	}

	volumes := make([]domainresource.PodVolumeView, 0, len(item.Spec.Volumes))
	for _, volume := range item.Spec.Volumes {
		volumeType, sourceName, readOnly, details := describePodVolume(volume)
		referencedConfigMaps := referencedConfigMapsForVolume(volume)
		volumeMounts := append([]domainresource.PodVolumeMountView(nil), mountsByVolume[volume.Name]...)
		for index := range volumeMounts {
			volumeMounts[index].VolumeType = volumeType
			volumeMounts[index].SourceName = sourceName
		}
		sort.SliceStable(volumeMounts, func(i, j int) bool {
			if volumeMounts[i].Name != volumeMounts[j].Name {
				return volumeMounts[i].Name < volumeMounts[j].Name
			}
			return volumeMounts[i].MountPath < volumeMounts[j].MountPath
		})
		sort.Strings(referencedConfigMaps)
		volumes = append(volumes, domainresource.PodVolumeView{
			Name:                 volume.Name,
			Type:                 volumeType,
			SourceName:           sourceName,
			ReadOnly:             readOnly,
			Details:              details,
			VolumeMounts:         volumeMounts,
			ReferencedConfigMaps: referencedConfigMaps,
		})
	}
	sort.SliceStable(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})
	return volumes
}

func describePodVolume(volume corev1.Volume) (string, string, bool, []string) {
	switch {
	case volume.ConfigMap != nil:
		details := []string{fmt.Sprintf("ConfigMap: %s", volume.ConfigMap.Name)}
		if volume.ConfigMap.Optional != nil {
			details = append(details, fmt.Sprintf("Optional: %t", *volume.ConfigMap.Optional))
		}
		if len(volume.ConfigMap.Items) > 0 {
			details = append(details, fmt.Sprintf("Items: %d", len(volume.ConfigMap.Items)))
		}
		return "ConfigMap", volume.ConfigMap.Name, false, details
	case volume.Secret != nil:
		details := []string{fmt.Sprintf("Secret: %s", volume.Secret.SecretName)}
		if volume.Secret.Optional != nil {
			details = append(details, fmt.Sprintf("Optional: %t", *volume.Secret.Optional))
		}
		if volume.Secret.DefaultMode != nil {
			details = append(details, fmt.Sprintf("DefaultMode: %04o", *volume.Secret.DefaultMode))
		}
		return "Secret", volume.Secret.SecretName, false, details
	case volume.PersistentVolumeClaim != nil:
		details := []string{fmt.Sprintf("PVC: %s", volume.PersistentVolumeClaim.ClaimName)}
		if volume.PersistentVolumeClaim.ReadOnly {
			details = append(details, "ReadOnly: true")
		}
		return "PersistentVolumeClaim", volume.PersistentVolumeClaim.ClaimName, volume.PersistentVolumeClaim.ReadOnly, details
	case volume.Projected != nil:
		details := []string{fmt.Sprintf("Sources: %d", len(volume.Projected.Sources))}
		if volume.Projected.DefaultMode != nil {
			details = append(details, fmt.Sprintf("DefaultMode: %04o", *volume.Projected.DefaultMode))
		}
		return "Projected", summarizeProjectedSourceNames(volume.Projected.Sources), false, details
	case volume.EmptyDir != nil:
		details := []string{}
		if volume.EmptyDir.Medium != "" {
			details = append(details, fmt.Sprintf("Medium: %s", volume.EmptyDir.Medium))
		}
		if volume.EmptyDir.SizeLimit != nil {
			details = append(details, fmt.Sprintf("SizeLimit: %s", volume.EmptyDir.SizeLimit.String()))
		}
		return "EmptyDir", "", false, details
	case volume.HostPath != nil:
		details := []string{fmt.Sprintf("Path: %s", volume.HostPath.Path)}
		if volume.HostPath.Type != nil {
			details = append(details, fmt.Sprintf("HostPathType: %s", string(*volume.HostPath.Type)))
		}
		return "HostPath", volume.HostPath.Path, false, details
	case volume.DownwardAPI != nil:
		details := []string{fmt.Sprintf("Items: %d", len(volume.DownwardAPI.Items))}
		if volume.DownwardAPI.DefaultMode != nil {
			details = append(details, fmt.Sprintf("DefaultMode: %04o", *volume.DownwardAPI.DefaultMode))
		}
		return "DownwardAPI", "", false, details
	default:
		return detectGenericPodVolumeType(volume), "", false, nil
	}
}

func detectGenericPodVolumeType(volume corev1.Volume) string {
	switch {
	case volume.CSI != nil:
		return "CSI"
	case volume.NFS != nil:
		return "NFS"
	case volume.AzureDisk != nil:
		return "AzureDisk"
	case volume.AzureFile != nil:
		return "AzureFile"
	case volume.CephFS != nil:
		return "CephFS"
	case volume.GCEPersistentDisk != nil:
		return "GCEPersistentDisk"
	case volume.ISCSI != nil:
		return "ISCSI"
	case volume.Ephemeral != nil:
		return "Ephemeral"
	default:
		return "Other"
	}
}

func summarizeProjectedSourceNames(sources []corev1.VolumeProjection) string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		switch {
		case source.ConfigMap != nil && strings.TrimSpace(source.ConfigMap.Name) != "":
			names = append(names, source.ConfigMap.Name)
		case source.Secret != nil && strings.TrimSpace(source.Secret.Name) != "":
			names = append(names, source.Secret.Name)
		case source.ServiceAccountToken != nil:
			names = append(names, "serviceAccountToken")
		case source.DownwardAPI != nil:
			names = append(names, "downwardAPI")
		case source.ClusterTrustBundle != nil && source.ClusterTrustBundle.Name != nil && strings.TrimSpace(*source.ClusterTrustBundle.Name) != "":
			names = append(names, *source.ClusterTrustBundle.Name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func referencedConfigMapsForVolume(volume corev1.Volume) []string {
	names := make([]string, 0, 2)
	if volume.ConfigMap != nil && strings.TrimSpace(volume.ConfigMap.Name) != "" {
		names = append(names, volume.ConfigMap.Name)
	}
	if volume.Projected != nil {
		for _, source := range volume.Projected.Sources {
			if source.ConfigMap != nil && strings.TrimSpace(source.ConfigMap.Name) != "" {
				names = append(names, source.ConfigMap.Name)
			}
		}
	}
	return uniqueSortedStrings(names)
}

func (s *Service) buildPodRelatedResources(ctx context.Context, clusterID string, item corev1.Pod, refs podVolumeSourceRefSet) []domainresource.PodRelatedResourceView {
	resources := map[string]*podRelatedResourceAccumulator{}
	add := func(kind, namespace, name, relation string, details ...string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := fmt.Sprintf("%s/%s/%s", kind, namespace, name)
		entry, ok := resources[key]
		if !ok {
			entry = &podRelatedResourceAccumulator{
				kind:      kind,
				name:      name,
				namespace: namespace,
				relations: map[string]struct{}{},
				details:   map[string]struct{}{},
			}
			resources[key] = entry
		}
		if strings.TrimSpace(relation) != "" {
			entry.relations[relation] = struct{}{}
		}
		for _, detail := range details {
			if strings.TrimSpace(detail) != "" {
				entry.details[detail] = struct{}{}
			}
		}
	}

	if sa := strings.TrimSpace(item.Spec.ServiceAccountName); sa != "" {
		add("ServiceAccount", item.Namespace, sa, "service-account")
	}
	for name := range refs.configMaps {
		add("ConfigMap", item.Namespace, name, "config")
	}
	for name := range refs.secrets {
		add("Secret", item.Namespace, name, "secret")
	}
	for name := range refs.pvcs {
		add("PersistentVolumeClaim", item.Namespace, name, "volume")
	}

	for _, owner := range item.OwnerReferences {
		switch owner.Kind {
		case "ReplicaSet":
			add("ReplicaSet", item.Namespace, owner.Name, "owner")
		case "StatefulSet", "DaemonSet", "Job", "CronJob":
			add(owner.Kind, item.Namespace, owner.Name, "owner")
		}
	}

	s.buildDirectPodRelatedResources(ctx, clusterID, item, add)

	result := make([]domainresource.PodRelatedResourceView, 0, len(resources))
	for _, entry := range resources {
		result = append(result, domainresource.PodRelatedResourceView{
			Kind:      entry.kind,
			Name:      entry.name,
			Namespace: entry.namespace,
			Relations: mapKeysSorted(entry.relations),
			Details:   mapKeysSorted(entry.details),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (s *Service) buildDirectPodRelatedResources(ctx context.Context, clusterID string, item corev1.Pod, add func(kind, namespace, name, relation string, details ...string)) {
	if services, _, err := s.listDirectServices(ctx, clusterID, item.Namespace); err == nil {
		serviceNames := map[string]struct{}{}
		for _, svc := range services {
			if selectorMatchesPodLabels(svc.Spec.Selector, item.Labels) {
				add("Service", svc.Namespace, svc.Name, "selected-by-service", fmt.Sprintf("Type: %s", svc.Spec.Type))
				serviceNames[svc.Name] = struct{}{}
			}
		}
		if ingresses, _, err := s.listDirectIngresses(ctx, clusterID, item.Namespace); err == nil {
			for _, ingress := range ingresses {
				for _, serviceName := range ingressBackendServiceNames(ingress) {
					if _, ok := serviceNames[serviceName]; ok {
						add("Ingress", ingress.Namespace, ingress.Name, "routes-service", fmt.Sprintf("Service: %s", serviceName))
					}
				}
			}
		}
	}
	if replicasets, err := s.listDirectReplicaSets(ctx, clusterID, item.Namespace); err == nil {
		for _, rs := range replicasets {
			if selectorMatchesPodLabels(rs.Spec.Selector.MatchLabels, item.Labels) {
				add("ReplicaSet", rs.Namespace, rs.Name, "selector-match")
				for _, owner := range rs.OwnerReferences {
					if owner.Kind == "Deployment" {
						add("Deployment", rs.Namespace, owner.Name, "managed-by-replicaset", fmt.Sprintf("ReplicaSet: %s", rs.Name))
					}
				}
			}
		}
	}
	if deployments, _, err := s.listDirectDeployments(ctx, clusterID, item.Namespace); err == nil {
		for _, deployment := range deployments {
			if selectorMatchesPodLabels(deployment.Spec.Selector.MatchLabels, item.Labels) {
				add("Deployment", deployment.Namespace, deployment.Name, "selector-match")
			}
		}
	}
}

func selectorMatchesPodLabels(selector, labels map[string]string) bool {
	entries := make([]string, 0, len(selector))
	for key, value := range selector {
		entries = append(entries, key+"="+value)
	}
	if len(entries) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func ingressBackendServiceNames(item networkingv1.Ingress) []string {
	names := make([]string, 0)
	if item.Spec.DefaultBackend != nil && item.Spec.DefaultBackend.Service != nil && strings.TrimSpace(item.Spec.DefaultBackend.Service.Name) != "" {
		names = append(names, item.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil && strings.TrimSpace(path.Backend.Service.Name) != "" {
				names = append(names, path.Backend.Service.Name)
			}
		}
	}
	return uniqueSortedStrings(names)
}

func uniqueSortedStrings(items []string) []string {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			set[item] = struct{}{}
		}
	}
	return mapKeysSorted(set)
}

func mapKeysSorted(items map[string]struct{}) []string {
	values := make([]string, 0, len(items))
	for item := range items {
		values = append(values, item)
	}
	sort.Strings(values)
	return values
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

func mapReplicaSet(item appsv1.ReplicaSet, decision domainaccess.Decision) domainresource.ReplicaSetView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicaSetView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		DesiredReplicas:   desired,
		ReadyReplicas:     item.Status.ReadyReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds:        secondsSince(item.CreationTimestamp.Time),
		AllowedActions:    stringifyActions(decision.AllowedActions),
	}
}

func mapConfigMap(item corev1.ConfigMap, decision domainaccess.Decision) domainresource.ConfigMapView {
	return domainresource.ConfigMapView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		DataEntries:    len(item.Data),
		BinaryEntries:  len(item.BinaryData),
		Immutable:      item.Immutable != nil && *item.Immutable,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapSecret(item corev1.Secret, decision domainaccess.Decision) domainresource.SecretView {
	return domainresource.SecretView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Type:           string(item.Type),
		DataEntries:    len(item.Data),
		Immutable:      item.Immutable != nil && *item.Immutable,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapServiceAccount(item corev1.ServiceAccount, decision domainaccess.Decision) domainresource.ServiceAccountView {
	return domainresource.ServiceAccountView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Secrets:          len(item.Secrets),
		ImagePullSecrets: len(item.ImagePullSecrets),
		AutomountSAToken: item.AutomountServiceAccountToken != nil && *item.AutomountServiceAccountToken,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}

func mapServiceAccountDetail(item corev1.ServiceAccount, decision domainaccess.Decision) domainresource.ServiceAccountDetailView {
	secrets := make([]string, 0, len(item.Secrets))
	for _, secret := range item.Secrets {
		if strings.TrimSpace(secret.Name) != "" {
			secrets = append(secrets, secret.Name)
		}
	}
	imagePullSecrets := make([]string, 0, len(item.ImagePullSecrets))
	for _, secret := range item.ImagePullSecrets {
		if strings.TrimSpace(secret.Name) != "" {
			imagePullSecrets = append(imagePullSecrets, secret.Name)
		}
	}
	sort.Strings(secrets)
	sort.Strings(imagePullSecrets)
	return domainresource.ServiceAccountDetailView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Labels:           item.Labels,
		Annotations:      item.Annotations,
		Secrets:          secrets,
		ImagePullSecrets: imagePullSecrets,
		AutomountSAToken: item.AutomountServiceAccountToken != nil && *item.AutomountServiceAccountToken,
		CreatedAt:        item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}

func summarizeRBACPolicyRules(rules []rbacv1.PolicyRule) []string {
	summaries := make([]string, 0, len(rules))
	for _, rule := range rules {
		verbs := append([]string(nil), rule.Verbs...)
		sort.Strings(verbs)
		left := strings.Join(verbs, ", ")
		switch {
		case len(rule.NonResourceURLs) > 0:
			urls := append([]string(nil), rule.NonResourceURLs...)
			sort.Strings(urls)
			summaries = append(summaries, fmt.Sprintf("%s -> %s", left, strings.Join(urls, ", ")))
		default:
			resources := append([]string(nil), rule.Resources...)
			sort.Strings(resources)
			right := strings.Join(resources, ", ")
			if len(rule.APIGroups) > 0 {
				groups := append([]string(nil), rule.APIGroups...)
				sort.Strings(groups)
				groupSummary := strings.Join(groups, ", ")
				if strings.TrimSpace(groupSummary) != "" {
					right = fmt.Sprintf("%s (%s)", right, groupSummary)
				}
			}
			if len(rule.ResourceNames) > 0 {
				names := append([]string(nil), rule.ResourceNames...)
				sort.Strings(names)
				right = fmt.Sprintf("%s [%s]", right, strings.Join(names, ", "))
			}
			summaries = append(summaries, fmt.Sprintf("%s -> %s", left, right))
		}
	}
	return summaries
}

func mapRole(item rbacv1.Role, decision domainaccess.Decision) domainresource.RoleView {
	return domainresource.RoleView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Rules:          len(item.Rules),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapRoleDetail(item rbacv1.Role, decision domainaccess.Decision) domainresource.RoleDetailView {
	return domainresource.RoleDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		Rules:          len(item.Rules),
		RuleSummaries:  summarizeRBACPolicyRules(item.Rules),
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapRoleBinding(item rbacv1.RoleBinding, decision domainaccess.Decision) domainresource.RoleBindingView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	return domainresource.RoleBindingView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		RoleRef:        fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:       subjects,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapRoleBindingDetail(item rbacv1.RoleBinding, decision domainaccess.Decision) domainresource.RoleBindingDetailView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	sort.Strings(subjects)
	return domainresource.RoleBindingDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		RoleRef:        fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:       subjects,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapHorizontalPodAutoscaler(item autoscalingv2.HorizontalPodAutoscaler, decision domainaccess.Decision) domainresource.HorizontalPodAutoscalerView {
	minReplicas := int32(1)
	if item.Spec.MinReplicas != nil {
		minReplicas = *item.Spec.MinReplicas
	}
	return domainresource.HorizontalPodAutoscalerView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		TargetRef:       fmt.Sprintf("%s/%s", item.Spec.ScaleTargetRef.Kind, item.Spec.ScaleTargetRef.Name),
		MinReplicas:     minReplicas,
		MaxReplicas:     item.Spec.MaxReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		DesiredReplicas: item.Status.DesiredReplicas,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}

func mapPodDisruptionBudget(item policyv1.PodDisruptionBudget, decision domainaccess.Decision) domainresource.PodDisruptionBudgetView {
	minAvailable := ""
	if item.Spec.MinAvailable != nil {
		minAvailable = item.Spec.MinAvailable.String()
	}
	maxUnavailable := ""
	if item.Spec.MaxUnavailable != nil {
		maxUnavailable = item.Spec.MaxUnavailable.String()
	}
	return domainresource.PodDisruptionBudgetView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		MinAvailable:       minAvailable,
		MaxUnavailable:     maxUnavailable,
		CurrentHealthy:     item.Status.CurrentHealthy,
		DesiredHealthy:     item.Status.DesiredHealthy,
		DisruptionsAllowed: item.Status.DisruptionsAllowed,
		AgeSeconds:         secondsSince(item.CreationTimestamp.Time),
		AllowedActions:     stringifyActions(decision.AllowedActions),
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

func mapEndpointSlice(item discoveryv1.EndpointSlice, decision domainaccess.Decision) domainresource.EndpointSliceView {
	ports := make([]string, 0, len(item.Ports))
	for _, port := range item.Ports {
		if port.Port == nil {
			continue
		}
		name := ""
		if port.Name != nil && strings.TrimSpace(*port.Name) != "" {
			name = *port.Name + ":"
		}
		protocol := ""
		if port.Protocol != nil {
			protocol = strings.ToLower(string(*port.Protocol))
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, *port.Port, protocol))
	}
	return domainresource.EndpointSliceView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		AddressType:    string(item.AddressType),
		Endpoints:      len(item.Endpoints),
		Ports:          ports,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
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

func mapPersistentVolumeClaimDetail(item corev1.PersistentVolumeClaim, decision domainaccess.Decision) domainresource.PersistentVolumeClaimDetailView {
	requested := ""
	if quantity, ok := item.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		requested = quantity.String()
	}
	capacity := ""
	if quantity, ok := item.Status.Capacity[corev1.ResourceStorage]; ok {
		capacity = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeClaimDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Status:         string(item.Status.Phase),
		VolumeName:     item.Spec.VolumeName,
		StorageClass:   storageClass,
		AccessModes:    accessModes,
		Requested:      requested,
		VolumeMode:     volumeMode,
		Capacity:       capacity,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
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

func mapPersistentVolumeDetail(item corev1.PersistentVolume, decision domainaccess.Decision) domainresource.PersistentVolumeDetailView {
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
	return domainresource.PersistentVolumeDetailView{
		Name:           item.Name,
		Status:         string(item.Status.Phase),
		StorageClass:   item.Spec.StorageClassName,
		ClaimRef:       claimRef,
		AccessModes:    accessModes,
		Capacity:       capacity,
		ReclaimPolicy:  string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode:     volumeMode,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
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

func mapStorageClassDetail(item storagev1.StorageClass, decision domainaccess.Decision) domainresource.StorageClassDetailView {
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
	return domainresource.StorageClassDetailView{
		Name:                 item.Name,
		Provisioner:          item.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    volumeBindingMode,
		AllowVolumeExpansion: allowVolumeExpansion,
		Parameters:           item.Parameters,
		Labels:               item.Labels,
		Annotations:          item.Annotations,
		CreatedAt:            item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
		AllowedActions:       stringifyActions(decision.AllowedActions),
	}
}

func mapIngressClass(item networkingv1.IngressClass, decision domainaccess.Decision) domainresource.IngressClassView {
	isDefault := false
	if v, ok := item.Annotations["ingressclass.kubernetes.io/is-default-class"]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
		isDefault = true
	}
	parameters := ""
	if item.Spec.Parameters != nil {
		parameters = fmt.Sprintf("%s/%s", item.Spec.Parameters.Kind, item.Spec.Parameters.Name)
	}
	return domainresource.IngressClassView{
		Name:           item.Name,
		Controller:     item.Spec.Controller,
		IsDefault:      isDefault,
		Parameters:     parameters,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapPriorityClass(item schedulingv1.PriorityClass, decision domainaccess.Decision) domainresource.PriorityClassView {
	preemptionPolicy := ""
	if item.PreemptionPolicy != nil {
		preemptionPolicy = string(*item.PreemptionPolicy)
	}
	return domainresource.PriorityClassView{
		Name:             item.Name,
		Value:            item.Value,
		GlobalDefault:    item.GlobalDefault,
		PreemptionPolicy: preemptionPolicy,
		Description:      item.Description,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}

func mapRuntimeClass(item nodev1.RuntimeClass, decision domainaccess.Decision) domainresource.RuntimeClassView {
	return domainresource.RuntimeClassView{
		Name:           item.Name,
		Handler:        item.Handler,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapClusterRole(item rbacv1.ClusterRole, decision domainaccess.Decision) domainresource.ClusterRoleView {
	aggregation := 0
	if item.AggregationRule != nil {
		aggregation = len(item.AggregationRule.ClusterRoleSelectors)
	}
	return domainresource.ClusterRoleView{
		Name:             item.Name,
		Rules:            len(item.Rules),
		AggregationRules: aggregation,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}

func mapClusterRoleDetail(item rbacv1.ClusterRole, decision domainaccess.Decision) domainresource.ClusterRoleDetailView {
	aggregation := 0
	if item.AggregationRule != nil {
		aggregation = len(item.AggregationRule.ClusterRoleSelectors)
	}
	return domainresource.ClusterRoleDetailView{
		Name:             item.Name,
		Labels:           item.Labels,
		Annotations:      item.Annotations,
		Rules:            len(item.Rules),
		AggregationRules: aggregation,
		RuleSummaries:    summarizeRBACPolicyRules(item.Rules),
		CreatedAt:        item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}

func mapClusterRoleBinding(item rbacv1.ClusterRoleBinding, decision domainaccess.Decision) domainresource.ClusterRoleBindingView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	return domainresource.ClusterRoleBindingView{
		Name:           item.Name,
		RoleRef:        fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:       subjects,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapClusterRoleBindingDetail(item rbacv1.ClusterRoleBinding, decision domainaccess.Decision) domainresource.ClusterRoleBindingDetailView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	sort.Strings(subjects)
	return domainresource.ClusterRoleBindingDetailView{
		Name:           item.Name,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		RoleRef:        fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:       subjects,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapMutatingWebhookConfiguration(item admissionregistrationv1.MutatingWebhookConfiguration, decision domainaccess.Decision) domainresource.MutatingWebhookConfigurationView {
	return domainresource.MutatingWebhookConfigurationView{
		Name:           item.Name,
		Webhooks:       len(item.Webhooks),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapValidatingWebhookConfiguration(item admissionregistrationv1.ValidatingWebhookConfiguration, decision domainaccess.Decision) domainresource.ValidatingWebhookConfigurationView {
	return domainresource.ValidatingWebhookConfigurationView{
		Name:           item.Name,
		Webhooks:       len(item.Webhooks),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapResourceQuota(item corev1.ResourceQuota, decision domainaccess.Decision) domainresource.ResourceQuotaView {
	scopes := make([]string, 0, len(item.Spec.Scopes))
	for _, scope := range item.Spec.Scopes {
		scopes = append(scopes, string(scope))
	}
	hard := make(map[string]string, len(item.Status.Hard))
	for k, v := range item.Status.Hard {
		hard[string(k)] = v.String()
	}
	used := make(map[string]string, len(item.Status.Used))
	for k, v := range item.Status.Used {
		used[string(k)] = v.String()
	}
	return domainresource.ResourceQuotaView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Scopes:         scopes,
		Hard:           hard,
		Used:           used,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapLimitRange(item corev1.LimitRange, decision domainaccess.Decision) domainresource.LimitRangeView {
	return domainresource.LimitRangeView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Limits:         len(item.Spec.Limits),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func mapLease(item coordinationv1.Lease, decision domainaccess.Decision) domainresource.LeaseView {
	holder := ""
	if item.Spec.HolderIdentity != nil {
		holder = *item.Spec.HolderIdentity
	}
	duration := int32(0)
	if item.Spec.LeaseDurationSeconds != nil {
		duration = *item.Spec.LeaseDurationSeconds
	}
	acquire := ""
	if item.Spec.AcquireTime != nil {
		acquire = item.Spec.AcquireTime.UTC().Format(time.RFC3339)
	}
	renew := ""
	if item.Spec.RenewTime != nil {
		renew = item.Spec.RenewTime.UTC().Format(time.RFC3339)
	}
	return domainresource.LeaseView{
		Name:                 item.Name,
		Namespace:            item.Namespace,
		HolderIdentity:       holder,
		LeaseDurationSeconds: duration,
		AcquireTime:          acquire,
		RenewTime:            renew,
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
		AllowedActions:       stringifyActions(decision.AllowedActions),
	}
}

func mapReplicationController(item corev1.ReplicationController, decision domainaccess.Decision) domainresource.ReplicationControllerView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicationControllerView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		DesiredReplicas:   desired,
		CurrentReplicas:   item.Status.Replicas,
		ReadyReplicas:     item.Status.ReadyReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds:        secondsSince(item.CreationTimestamp.Time),
		AllowedActions:    stringifyActions(decision.AllowedActions),
	}
}

func mapNetworkPolicy(item networkingv1.NetworkPolicy, decision domainaccess.Decision) domainresource.NetworkPolicyView {
	policyTypes := make([]string, 0, len(item.Spec.PolicyTypes))
	for _, policyType := range item.Spec.PolicyTypes {
		policyTypes = append(policyTypes, string(policyType))
	}
	return domainresource.NetworkPolicyView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		PolicyTypes:    policyTypes,
		IngressRules:   len(item.Spec.Ingress),
		EgressRules:    len(item.Spec.Egress),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
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
		Version:    firstCRDVersion(versions),
		Versions:   versions,
		CreatedAt:  item.GetCreationTimestamp().Time.UTC().Format(time.RFC3339),
		AgeSeconds: secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapCustomResource(item unstructured.Unstructured, definition crdResourceDefinition, decision domainaccess.Decision) domainresource.CustomResourceView {
	apiVersion := strings.TrimSpace(item.GetAPIVersion())
	if apiVersion == "" && definition.Group != "" && definition.Version != "" {
		apiVersion = definition.Group + "/" + definition.Version
	}
	return domainresource.CustomResourceView{
		APIVersion:     apiVersion,
		Kind:           definition.Kind,
		Name:           item.GetName(),
		Namespace:      item.GetNamespace(),
		Labels:         item.GetLabels(),
		CreatedAt:      item.GetCreationTimestamp().Time.UTC().Format(time.RFC3339),
		AgeSeconds:     secondsSince(item.GetCreationTimestamp().Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
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

func populateAllowedActionsReplicaSets(items []domainresource.ReplicaSetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsConfigMaps(items []domainresource.ConfigMapView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsSecrets(items []domainresource.SecretView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsServiceAccounts(items []domainresource.ServiceAccountView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsRoles(items []domainresource.RoleView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsRoleBindings(items []domainresource.RoleBindingView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsHorizontalPodAutoscalers(items []domainresource.HorizontalPodAutoscalerView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsPodDisruptionBudgets(items []domainresource.PodDisruptionBudgetView, decision domainaccess.Decision) {
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

func populateAllowedActionsEndpointSlices(items []domainresource.EndpointSliceView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsNetworkPolicies(items []domainresource.NetworkPolicyView, decision domainaccess.Decision) {
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

func populateAllowedActionsIngressClasses(items []domainresource.IngressClassView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsPriorityClasses(items []domainresource.PriorityClassView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsRuntimeClasses(items []domainresource.RuntimeClassView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsClusterRoles(items []domainresource.ClusterRoleView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsClusterRoleBindings(items []domainresource.ClusterRoleBindingView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsMutatingWebhookConfigurations(items []domainresource.MutatingWebhookConfigurationView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsValidatingWebhookConfigurations(items []domainresource.ValidatingWebhookConfigurationView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsResourceQuotas(items []domainresource.ResourceQuotaView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsLimitRanges(items []domainresource.LimitRangeView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsLeases(items []domainresource.LeaseView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsReplicationControllers(items []domainresource.ReplicationControllerView, decision domainaccess.Decision) {
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

func (d crdResourceDefinition) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: d.Group, Version: d.Version, Resource: d.Resource}
}

func parseCRDResourceDefinition(item unstructured.Unstructured) (crdResourceDefinition, error) {
	group, _, _ := unstructured.NestedString(item.Object, "spec", "group")
	kind, _, _ := unstructured.NestedString(item.Object, "spec", "names", "kind")
	resource, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
	scope, _, _ := unstructured.NestedString(item.Object, "spec", "scope")
	version, err := servedCRDVersion(item)
	if err != nil {
		return crdResourceDefinition{}, err
	}
	if strings.TrimSpace(group) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(resource) == "" {
		return crdResourceDefinition{}, fmt.Errorf("%w: crd %s is missing required group, kind, or plural metadata", apperrors.ErrInvalidArgument, item.GetName())
	}
	namespaced, err := namespacedFromCRDScope(scope, item.GetName())
	if err != nil {
		return crdResourceDefinition{}, err
	}
	return crdResourceDefinition{
		CRDName:    item.GetName(),
		Kind:       kind,
		Group:      group,
		Version:    version,
		Resource:   resource,
		Namespaced: namespaced,
	}, nil
}

func servedCRDVersion(item unstructured.Unstructured) (string, error) {
	versions, _, _ := unstructured.NestedSlice(item.Object, "spec", "versions")
	var fallback string
	for _, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := version["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		if fallback == "" {
			fallback = name
		}
		if served, _ := version["served"].(bool); served {
			if storage, _ := version["storage"].(bool); storage {
				return name, nil
			}
		}
	}
	for _, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := version["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		if served, _ := version["served"].(bool); served {
			return name, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("%w: crd %s does not expose any version metadata", apperrors.ErrInvalidArgument, item.GetName())
}

func firstCRDVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	return versions[0]
}

func namespacedFromCRDScope(scope, crdName string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "namespaced":
		return true, nil
	case "cluster":
		return false, nil
	default:
		return false, fmt.Errorf("%w: crd %s has unsupported scope %q", apperrors.ErrInvalidArgument, crdName, scope)
	}
}

func requiredCustomResourceNamespace(definition crdResourceDefinition, namespace string) (string, error) {
	namespace = strings.TrimSpace(namespace)
	if definition.Namespaced {
		if namespace == "" {
			return "", fmt.Errorf("%w: namespace is required for namespaced custom resource kind %s", apperrors.ErrInvalidArgument, definition.Kind)
		}
		return namespace, nil
	}
	if namespace != "" {
		return "", fmt.Errorf("%w: namespace must be empty for cluster-scoped custom resource kind %s", apperrors.ErrInvalidArgument, definition.Kind)
	}
	return "", nil
}

func buildCustomResourceFromYAML(definition crdResourceDefinition, content, namespace, expectedName string) (*unstructured.Unstructured, string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, "", fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return nil, "", fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	if item.GetKind() == "" {
		item.SetKind(definition.Kind)
	}
	if !strings.EqualFold(item.GetKind(), definition.Kind) {
		return nil, "", fmt.Errorf("%w: yaml kind %s does not match target %s", apperrors.ErrInvalidArgument, item.GetKind(), definition.Kind)
	}
	if item.GetAPIVersion() == "" {
		item.SetAPIVersion(definition.Group + "/" + definition.Version)
	}
	if strings.TrimSpace(item.GetName()) == "" {
		if strings.TrimSpace(expectedName) == "" {
			return nil, "", fmt.Errorf("%w: yaml metadata.name is required", apperrors.ErrInvalidArgument)
		}
		item.SetName(expectedName)
	}
	if expectedName = strings.TrimSpace(expectedName); expectedName != "" && item.GetName() != expectedName {
		return nil, "", fmt.Errorf("%w: yaml metadata.name does not match target resource", apperrors.ErrInvalidArgument)
	}
	effectiveNamespace, err := requiredCustomResourceNamespace(definition, firstNonEmpty(item.GetNamespace(), namespace))
	if err != nil {
		return nil, "", err
	}
	if definition.Namespaced {
		item.SetNamespace(effectiveNamespace)
	} else {
		item.SetNamespace("")
	}
	return item, effectiveNamespace, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeCustomResourceNamespaceForAuth(namespace string, namespaced bool) string {
	if !namespaced {
		return ""
	}
	return strings.TrimSpace(namespace)
}

func normalizeCustomResourceNamespaceForAudit(namespace string, namespaced bool) string {
	if !namespaced {
		return ""
	}
	return strings.TrimSpace(namespace)
}

func (s *Service) authorizeCRDDefinitionAccess(ctx context.Context, principal domainidentity.Principal, clusterID string, action domainaccess.Action) (domaincluster.Connection, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "CRD", action)
	if err != nil {
		return domaincluster.Connection{}, err
	}
	return connection, nil
}

func dedupeHelmReleases(items []domainresource.HelmReleaseView) []domainresource.HelmReleaseView {
	seen := make(map[string]struct{}, len(items))
	result := make([]domainresource.HelmReleaseView, 0, len(items))
	for _, item := range items {
		key := item.Namespace + "/" + item.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}
