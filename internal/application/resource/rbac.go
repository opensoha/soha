package resource

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().ServiceAccounts(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectServiceAccount(ctx context.Context, clusterID, namespace, name string) (*corev1.ServiceAccount, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.RbacV1().Roles(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectRole(ctx context.Context, clusterID, namespace, name string) (*rbacv1.Role, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.RbacV1().RoleBindings(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectRoleBinding(ctx context.Context, clusterID, namespace, name string) (*rbacv1.RoleBinding, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.RbacV1().RoleBindings(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) listDirectClusterRoles(ctx context.Context, clusterID string) ([]rbacv1.ClusterRole, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.RbacV1().ClusterRoles().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectClusterRole(ctx context.Context, clusterID, name string) (*rbacv1.ClusterRole, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.RbacV1().ClusterRoles().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) listDirectClusterRoleBindings(ctx context.Context, clusterID string) ([]rbacv1.ClusterRoleBinding, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.RbacV1().ClusterRoleBindings().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectClusterRoleBinding(ctx context.Context, clusterID, name string) (*rbacv1.ClusterRoleBinding, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.RbacV1().ClusterRoleBindings().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
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
