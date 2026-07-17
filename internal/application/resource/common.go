package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type resourceAccess struct {
	directClusters  ClusterMetadataProvider
	resolver        ConnectionResolver
	authorizer      domainaccess.Authorizer
	permissions     RuntimePermissionAuthorizer
	audit           AuditRecorder
	operations      OperationRecorder
	namespaceLabels namespaceLabelResolver
}

type namespaceLabelResolver interface {
	Resolve(context.Context, domaincluster.Connection, string) (map[string]string, error)
}

func resolveAgentClient[T any](factory AgentClientFactory[T], connection domaincluster.Connection) (T, error) {
	var zero T
	if factory == nil {
		return zero, fmt.Errorf("%w: agent client is not configured", apperrors.ErrClusterUnready)
	}
	client, err := factory(connection)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	return client, nil
}

func (s *Workloads) workloadAgentClient(connection domaincluster.Connection) (WorkloadAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *Configuration) configurationAgentClient(connection domaincluster.Connection) (ConfigurationAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *Workloads) configurationAgentClient(connection domaincluster.Connection) (ConfigurationAgent, error) {
	return resolveAgentClient(s.configuration, connection)
}

func (s *Network) configurationAgentClient(connection domaincluster.Connection) (ConfigurationAgent, error) {
	return resolveAgentClient(s.configuration, connection)
}

func (s *Network) networkAgentClient(connection domaincluster.Connection) (NetworkAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *Storage) storageAgentClient(connection domaincluster.Connection) (StorageAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *RBAC) rbacAgentClient(connection domaincluster.Connection) (RBACAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *Helm) helmAgentClient(connection domaincluster.Connection) (HelmAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *Inventory) inventoryAgentClient(connection domaincluster.Connection) (InventoryAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *CustomResources) customResourceAgentClient(connection domaincluster.Connection) (CustomResourceAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *GenericResources) genericResourceAgentClient(connection domaincluster.Connection) (GenericResourceAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *Events) eventAgentClient(connection domaincluster.Connection) (EventAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func (s *PortForwards) portForwardAgentClient(connection domaincluster.Connection) (PortForwardAgent, error) {
	return resolveAgentClient(s.agent, connection)
}

func unsupportedAgentOperation(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "operation is not supported for agent-connected clusters yet"
	}
	return fmt.Errorf("%w: %s", apperrors.ErrUnsupportedOperation, message)
}

func (s *resourceAccess) loadConnection(ctx context.Context, clusterID string) (domaincluster.Connection, error) {
	if s.resolver != nil {
		connection, err := s.resolver.GetConnection(ctx, clusterID)
		if err == nil {
			if connection.Summary.ConnectionMode == "" {
				connection.Summary.ConnectionMode = domaincluster.ConnectionModeDirectKubeconfig
			}
			return connection, nil
		}
	}
	if s.directClusters == nil {
		return domaincluster.Connection{}, fmt.Errorf("%w: direct cluster provider is not configured", apperrors.ErrClusterUnready)
	}
	summary, err := s.directClusters.Metadata(clusterID)
	if err != nil {
		return domaincluster.Connection{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return domaincluster.Connection{Summary: summary}, nil
}
func (s *resourceAccess) authorize(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind string, action domainaccess.Action) (domaincluster.Connection, domainaccess.Decision, error) {
	connection, decision, err := s.decide(ctx, principal, clusterID, namespace, resourceGroupForKind(kind), kind, action)
	if err != nil {
		return domaincluster.Connection{}, domainaccess.Decision{}, err
	}
	if !decision.Allowed {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, "", string(action), "deny", decision.Reason)
		return domaincluster.Connection{}, domainaccess.Decision{}, fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return connection, decision, nil
}

func (s *resourceAccess) decide(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, resourceGroup, kind string, action domainaccess.Action) (domaincluster.Connection, domainaccess.Decision, error) {
	connection, err := s.loadConnection(ctx, clusterID)
	if err != nil {
		return domaincluster.Connection{}, domainaccess.Decision{}, err
	}
	request := s.resourceAccessRequest(ctx, principal, connection, namespace, kind, action)
	if strings.TrimSpace(resourceGroup) != "" {
		request.Resource.Group = strings.TrimSpace(resourceGroup)
	}
	decision, err := s.authorizer.Authorize(ctx, request)
	if err != nil {
		return domaincluster.Connection{}, domainaccess.Decision{}, err
	}
	if shouldResolveNamespaceLabels(decision, namespace, s.namespaceLabels) {
		if labels, resolveErr := s.namespaceLabels.Resolve(ctx, connection, namespace); resolveErr == nil {
			request.Namespace.Labels = labels
			decision, err = s.authorizer.Authorize(ctx, request)
			if err != nil {
				return domaincluster.Connection{}, domainaccess.Decision{}, err
			}
		}
	}
	return connection, decision, nil
}

func shouldResolveNamespaceLabels(decision domainaccess.Decision, namespace string, resolver namespaceLabelResolver) bool {
	if decision.Allowed || namespace == "" || resolver == nil {
		return false
	}
	reason := strings.ToLower(decision.Reason)
	return strings.Contains(reason, "namespace") || strings.Contains(reason, "scope grant")
}
func (s *resourceAccess) resourceAccessRequest(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace, kind string, action domainaccess.Action) domainaccess.Request {
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
		Resource:  domainaccess.ResourceAttributes{Group: resourceGroupForKind(kind), Kind: kind},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	}
}
func (s *resourceAccess) allowedActionsForResource(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace, kind string, action domainaccess.Action) []string {
	if s == nil || s.authorizer == nil {
		return nil
	}
	decision, err := s.authorizer.Authorize(ctx, s.resourceAccessRequest(ctx, principal, connection, namespace, kind, action))
	if err != nil || !decision.Allowed {
		return nil
	}
	return stringifyActions(decision.AllowedActions)
}
func (s *resourceAccess) recordAudit(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, action, result, summary string) error {
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
func (s *resourceAccess) recordOperation(ctx context.Context, principal domainidentity.Principal, operationType, clusterID, namespace, kind, name, summary string, metadata map[string]any) {
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
func (s *resourceAccess) authorizeDeploymentPermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	if s.permissions == nil {
		return fmt.Errorf("%w: runtime permission resolver unavailable for %s", apperrors.ErrAccessDenied, strings.TrimSpace(permissionKey))
	}
	return s.permissions.Authorize(ctx, principal, permissionKey)
}

func filterScopedNamespaceItems[T any](items []T, decision domainaccess.Decision, namespaceOf func(T) string) []T {
	if decision.ResourceScope == nil {
		return items
	}
	// Generic resource views do not carry namespace labels. Selector-only
	// scopes therefore fail closed; Namespace inventory resolves selectors to
	// concrete visible names before the user enters a namespaced resource page.
	if len(decision.ResourceScope.ExcludedNamespaceSelectors) > 0 || (len(decision.ResourceScope.Namespaces) == 0 && len(decision.ResourceScope.NamespaceSelectors) > 0) {
		return nil
	}
	allowed := make(map[string]struct{}, len(decision.ResourceScope.Namespaces))
	for _, namespace := range decision.ResourceScope.Namespaces {
		allowed[namespace] = struct{}{}
	}
	excluded := make(map[string]struct{}, len(decision.ResourceScope.ExcludedNamespaces))
	for _, namespace := range decision.ResourceScope.ExcludedNamespaces {
		excluded[namespace] = struct{}{}
	}
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		namespace := namespaceOf(item)
		if _, denied := excluded[namespace]; denied {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[namespace]; !ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func resourceGroupForKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod", "deployment", "statefulset", "daemonset", "job", "cronjob", "replicaset", "replicationcontroller":
		return "workloads"
	case "configmap", "secret", "horizontalpodautoscaler", "poddisruptionbudget", "priorityclass", "runtimeclass", "resourcequota", "limitrange", "lease", "mutatingwebhookconfiguration", "validatingwebhookconfiguration":
		return "configuration"
	case "service", "ingress", "endpointslice", "networkpolicy", "gatewayclass", "gateway", "httproute", "grpcroute", "backendtlspolicy", "referencegrant", "ingressclass":
		return "network"
	case "persistentvolumeclaim", "persistentvolume", "storageclass":
		return "storage"
	case "serviceaccount", "role", "rolebinding", "clusterrole", "clusterrolebinding":
		return "access-control"
	case "customresourcedefinition", "customresource", "helmrelease":
		return "extensions"
	case "namespace", "node", "cluster":
		return "inventory"
	default:
		return ""
	}
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
