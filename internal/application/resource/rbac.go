package resource

import (
	"context"
	"fmt"
	"strings"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *RBAC) ListServiceAccounts(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceAccountView, error) {
	return listRoutedModeResources(ctx, r.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "ServiceAccount", "serviceaccounts"), r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.ServiceAccountView, error) {
			return client.ListServiceAccounts(ctx, namespace)
		},
		func(direct DirectRBACReader) ([]domainresource.ServiceAccountView, error) {
			return direct.ListServiceAccounts(ctx, clusterID, namespace)
		},
		func(item domainresource.ServiceAccountView) string { return item.Namespace },
		func(item domainresource.ServiceAccountView) []string { return item.AllowedActions },
		func(item *domainresource.ServiceAccountView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) GetServiceAccountDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ServiceAccountDetailView, error) {
	return getNamespacedRBACDetail(ctx, r, principal, clusterID, namespace, "ServiceAccount", name,
		func(client RBACAgent) (domainresource.ServiceAccountDetailView, error) {
			return client.GetServiceAccountDetail(ctx, namespace, name)
		},
		func(direct DirectRBACReader) (domainresource.ServiceAccountDetailView, error) {
			return direct.GetServiceAccountDetail(ctx, clusterID, namespace, name)
		},
		func(item *domainresource.ServiceAccountDetailView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) ListRoles(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.RoleView, error) {
	return listRoutedModeResources(ctx, r.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "Role", "roles"), r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.RoleView, error) { return client.ListRoles(ctx, namespace) },
		func(direct DirectRBACReader) ([]domainresource.RoleView, error) {
			return direct.ListRoles(ctx, clusterID, namespace)
		},
		func(item domainresource.RoleView) string { return item.Namespace },
		func(item domainresource.RoleView) []string { return item.AllowedActions },
		func(item *domainresource.RoleView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) GetRoleDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.RoleDetailView, error) {
	return getNamespacedRBACDetail(ctx, r, principal, clusterID, namespace, "Role", name,
		func(client RBACAgent) (domainresource.RoleDetailView, error) {
			return client.GetRoleDetail(ctx, namespace, name)
		},
		func(direct DirectRBACReader) (domainresource.RoleDetailView, error) {
			return direct.GetRoleDetail(ctx, clusterID, namespace, name)
		},
		func(item *domainresource.RoleDetailView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) ListRoleBindings(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.RoleBindingView, error) {
	return listRoutedModeResources(ctx, r.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "RoleBinding", "rolebindings"), r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.RoleBindingView, error) {
			return client.ListRoleBindings(ctx, namespace)
		},
		func(direct DirectRBACReader) ([]domainresource.RoleBindingView, error) {
			return direct.ListRoleBindings(ctx, clusterID, namespace)
		},
		func(item domainresource.RoleBindingView) string { return item.Namespace },
		func(item domainresource.RoleBindingView) []string { return item.AllowedActions },
		func(item *domainresource.RoleBindingView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) ListRoleBindingsForServiceAccount(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.RoleBindingView, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: service account namespace and name are required", apperrors.ErrInvalidArgument)
	}
	return listRoutedModeResources(ctx, r.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "RoleBinding", "rolebindings for service account"), r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.RoleBindingView, error) {
			return client.ListRoleBindingsForServiceAccount(ctx, namespace, name)
		},
		func(direct DirectRBACReader) ([]domainresource.RoleBindingView, error) {
			return direct.ListRoleBindingsForServiceAccount(ctx, clusterID, namespace, name)
		},
		func(item domainresource.RoleBindingView) string { return item.Namespace },
		func(item domainresource.RoleBindingView) []string { return item.AllowedActions },
		func(item *domainresource.RoleBindingView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) GetRoleBindingDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.RoleBindingDetailView, error) {
	return getNamespacedRBACDetail(ctx, r, principal, clusterID, namespace, "RoleBinding", name,
		func(client RBACAgent) (domainresource.RoleBindingDetailView, error) {
			return client.GetRoleBindingDetail(ctx, namespace, name)
		},
		func(direct DirectRBACReader) (domainresource.RoleBindingDetailView, error) {
			return direct.GetRoleBindingDetail(ctx, clusterID, namespace, name)
		},
		func(item *domainresource.RoleBindingDetailView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) ListClusterRoles(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.ClusterRoleView, error) {
	request := clusterRBACListRequest(clusterID, "ClusterRole", "clusterroles")
	return listRoutedModeResources(ctx, r.resourceAccess, principal, request, r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.ClusterRoleView, error) { return client.ListClusterRoles(ctx) },
		func(direct DirectRBACReader) ([]domainresource.ClusterRoleView, error) {
			return direct.ListClusterRoles(ctx, clusterID)
		}, nil,
		func(item domainresource.ClusterRoleView) []string { return item.AllowedActions },
		func(item *domainresource.ClusterRoleView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) GetClusterRoleDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.ClusterRoleDetailView, error) {
	return getClusterRBACDetail(
		ctx, r, principal, clusterID, "ClusterRole", name,
		bindClusterAgentValue(ctx, name, RBACAgent.GetClusterRoleDetail),
		bindClusterDirectValue(ctx, clusterID, name, DirectRBACReader.GetClusterRoleDetail),
		setClusterRoleDetailActions,
	)
}

func (r *RBAC) ListClusterRoleBindings(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.ClusterRoleBindingView, error) {
	request := clusterRBACListRequest(clusterID, "ClusterRoleBinding", "clusterrolebindings")
	return listRoutedModeResources(ctx, r.resourceAccess, principal, request, r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.ClusterRoleBindingView, error) {
			return client.ListClusterRoleBindings(ctx)
		},
		func(direct DirectRBACReader) ([]domainresource.ClusterRoleBindingView, error) {
			return direct.ListClusterRoleBindings(ctx, clusterID)
		}, nil,
		func(item domainresource.ClusterRoleBindingView) []string { return item.AllowedActions },
		func(item *domainresource.ClusterRoleBindingView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) ListClusterRoleBindingsForServiceAccount(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.ClusterRoleBindingView, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: service account namespace and name are required", apperrors.ErrInvalidArgument)
	}
	request := clusterRBACListRequest(clusterID, "ClusterRoleBinding", "clusterrolebindings for service account")
	return listRoutedModeResources(ctx, r.resourceAccess, principal, request, r.rbacAgentClient, r.directRBAC,
		func(client RBACAgent) ([]domainresource.ClusterRoleBindingView, error) {
			return client.ListClusterRoleBindingsForServiceAccount(ctx, namespace, name)
		},
		func(direct DirectRBACReader) ([]domainresource.ClusterRoleBindingView, error) {
			return direct.ListClusterRoleBindingsForServiceAccount(ctx, clusterID, namespace, name)
		}, nil,
		func(item domainresource.ClusterRoleBindingView) []string { return item.AllowedActions },
		func(item *domainresource.ClusterRoleBindingView, actions []string) { item.AllowedActions = actions },
	)
}

func (r *RBAC) GetClusterRoleBindingDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.ClusterRoleBindingDetailView, error) {
	return getClusterRBACDetail(
		ctx, r, principal, clusterID, "ClusterRoleBinding", name,
		bindClusterAgentValue(ctx, name, RBACAgent.GetClusterRoleBindingDetail),
		bindClusterDirectValue(ctx, clusterID, name, DirectRBACReader.GetClusterRoleBindingDetail),
		setClusterRoleBindingDetailActions,
	)
}

func getClusterRBACDetail[T any](ctx context.Context, r *RBAC, principal domainidentity.Principal, clusterID, kind, name string, agentCall func(RBACAgent) (T, error), directCall func(DirectRBACReader) (T, error), setActions func(*T, []string)) (T, error) {
	request := resourceDetailRequest{
		clusterID: clusterID,
		kind:      kind,
		name:      name,
		summary: func(source string) string {
			return fmt.Sprintf("viewed %s detail via %s", strings.ToLower(kind), source)
		},
	}
	return getModeResource(
		ctx,
		r.resourceAccess,
		principal,
		request,
		func(connection domaincluster.Connection) (T, string, error) {
			return routeModeValue(connection, r.rbacAgentClient, r.directRBAC, agentCall, directCall)
		},
		setActions,
	)
}

func setClusterRoleDetailActions(item *domainresource.ClusterRoleDetailView, actions []string) {
	item.AllowedActions = actions
}

func setClusterRoleBindingDetailActions(item *domainresource.ClusterRoleBindingDetailView, actions []string) {
	item.AllowedActions = actions
}

func getNamespacedRBACDetail[T any](ctx context.Context, r *RBAC, principal domainidentity.Principal, clusterID, namespace, kind, name string, agentCall func(RBACAgent) (T, error), directCall func(DirectRBACReader) (T, error), setActions func(*T, []string)) (T, error) {
	var zero T
	if strings.TrimSpace(namespace) == "" {
		return zero, fmt.Errorf("%w: namespace is required for %s detail", apperrors.ErrInvalidArgument, strings.ToLower(kind))
	}
	request := resourceDetailRequest{clusterID: clusterID, namespace: namespace, kind: kind, name: name, summary: func(source string) string {
		return fmt.Sprintf("viewed %s detail via %s in namespace %s", strings.ToLower(kind), source, displayNamespace(namespace))
	}}
	return getModeResource(ctx, r.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (T, string, error) {
			return routeModeValue(connection, r.rbacAgentClient, r.directRBAC, agentCall, directCall)
		},
		setActions,
	)
}

func (r *RBAC) directRBAC() (DirectRBACReader, error) {
	return requireDirect(r.direct, r.direct != nil, "RBAC reader")
}

func clusterRBACListRequest(clusterID, kind, noun string) resourceListRequest {
	return resourceListRequest{clusterID: clusterID, kind: kind, summary: func(source string) string { return fmt.Sprintf("listed %s via %s", noun, source) }}
}
