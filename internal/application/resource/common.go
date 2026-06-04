package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	agentinfra "github.com/soha/soha/internal/infrastructure/agent"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
)

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
