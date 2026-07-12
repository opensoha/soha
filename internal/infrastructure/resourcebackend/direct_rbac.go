package resourcebackend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) ListServiceAccounts(ctx context.Context, clusterID, namespace string) ([]domainresource.ServiceAccountView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(ctx context.Context, namespace string) ([]corev1.ServiceAccount, error) {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ServiceAccountView, 0, len(items))
	for _, item := range items {
		views = append(views, mapServiceAccount(item))
	}
	return views, nil
}

func (d *Direct) GetServiceAccountDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.ServiceAccountDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ServiceAccountDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().ServiceAccounts(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ServiceAccountDetailView{}, err
	}
	return mapServiceAccountDetail(*item), nil
}

func (d *Direct) ListRoles(ctx context.Context, clusterID, namespace string) ([]domainresource.RoleView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(ctx context.Context, namespace string) ([]rbacv1.Role, error) {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.RbacV1().Roles(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RoleView, 0, len(items))
	for _, item := range items {
		views = append(views, mapRole(item))
	}
	return views, nil
}

func (d *Direct) GetRoleDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.RoleDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.RoleDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().Roles(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.RoleDetailView{}, err
	}
	return mapRoleDetail(*item), nil
}

func (d *Direct) ListRoleBindings(ctx context.Context, clusterID, namespace string) ([]domainresource.RoleBindingView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(ctx context.Context, namespace string) ([]rbacv1.RoleBinding, error) {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RoleBindingView, 0, len(items))
	for _, item := range items {
		views = append(views, mapRoleBinding(item))
	}
	return views, nil
}

func (d *Direct) GetRoleBindingDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.RoleBindingDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.RoleBindingDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().RoleBindings(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.RoleBindingDetailView{}, err
	}
	return mapRoleBindingDetail(*item), nil
}

func (d *Direct) ListClusterRoles(ctx context.Context, clusterID string) ([]domainresource.ClusterRoleView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.RbacV1().ClusterRoles().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ClusterRoleView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapClusterRole(item))
	}
	return views, nil
}

func (d *Direct) GetClusterRoleDetail(ctx context.Context, clusterID, name string) (domainresource.ClusterRoleDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ClusterRoleDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().ClusterRoles().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ClusterRoleDetailView{}, err
	}
	return mapClusterRoleDetail(*item), nil
}

func (d *Direct) ListClusterRoleBindings(ctx context.Context, clusterID string) ([]domainresource.ClusterRoleBindingView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.RbacV1().ClusterRoleBindings().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ClusterRoleBindingView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapClusterRoleBinding(item))
	}
	return views, nil
}

func (d *Direct) GetClusterRoleBindingDetail(ctx context.Context, clusterID, name string) (domainresource.ClusterRoleBindingDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ClusterRoleBindingDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.RbacV1().ClusterRoleBindings().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ClusterRoleBindingDetailView{}, err
	}
	return mapClusterRoleBindingDetail(*item), nil
}

func mapServiceAccount(item corev1.ServiceAccount) domainresource.ServiceAccountView {
	return domainresource.ServiceAccountView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Secrets:          len(item.Secrets),
		ImagePullSecrets: len(item.ImagePullSecrets),
		AutomountSAToken: item.AutomountServiceAccountToken != nil && *item.AutomountServiceAccountToken,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func mapServiceAccountDetail(item corev1.ServiceAccount) domainresource.ServiceAccountDetailView {
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
		CreatedAt:        item.CreationTimestamp.Format(time.RFC3339),
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func summarizeRBACPolicyRules(rules []rbacv1.PolicyRule) []string {
	summaries := make([]string, 0, len(rules))
	for _, rule := range rules {
		verbs := append([]string(nil), rule.Verbs...)
		sort.Strings(verbs)
		left := strings.Join(verbs, ", ")
		if len(rule.NonResourceURLs) > 0 {
			urls := append([]string(nil), rule.NonResourceURLs...)
			sort.Strings(urls)
			summaries = append(summaries, fmt.Sprintf("%s -> %s", left, strings.Join(urls, ", ")))
			continue
		}
		resources := append([]string(nil), rule.Resources...)
		sort.Strings(resources)
		right := strings.Join(resources, ", ")
		if len(rule.APIGroups) > 0 {
			groups := append([]string(nil), rule.APIGroups...)
			sort.Strings(groups)
			if groupSummary := strings.Join(groups, ", "); strings.TrimSpace(groupSummary) != "" {
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
	return summaries
}

func mapRole(item rbacv1.Role) domainresource.RoleView {
	return domainresource.RoleView{
		Name: item.Name, Namespace: item.Namespace, Rules: len(item.Rules),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleDetail(item rbacv1.Role) domainresource.RoleDetailView {
	return domainresource.RoleDetailView{
		Name: item.Name, Namespace: item.Namespace, Labels: item.Labels, Annotations: item.Annotations,
		Rules: len(item.Rules), RuleSummaries: summarizeRBACPolicyRules(item.Rules),
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleBinding(item rbacv1.RoleBinding) domainresource.RoleBindingView {
	return domainresource.RoleBindingView{
		Name: item.Name, Namespace: item.Namespace, RoleRef: formatRoleRef(item.RoleRef),
		Subjects: formatSubjects(item.Subjects), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleBindingDetail(item rbacv1.RoleBinding) domainresource.RoleBindingDetailView {
	subjects := formatSubjects(item.Subjects)
	sort.Strings(subjects)
	return domainresource.RoleBindingDetailView{
		Name: item.Name, Namespace: item.Namespace, Labels: item.Labels, Annotations: item.Annotations,
		RoleRef: formatRoleRef(item.RoleRef), Subjects: subjects,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRole(item rbacv1.ClusterRole) domainresource.ClusterRoleView {
	return domainresource.ClusterRoleView{
		Name: item.Name, Rules: len(item.Rules), AggregationRules: aggregationRuleCount(item.AggregationRule),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleDetail(item rbacv1.ClusterRole) domainresource.ClusterRoleDetailView {
	return domainresource.ClusterRoleDetailView{
		Name: item.Name, Labels: item.Labels, Annotations: item.Annotations, Rules: len(item.Rules),
		AggregationRules: aggregationRuleCount(item.AggregationRule), RuleSummaries: summarizeRBACPolicyRules(item.Rules),
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleBinding(item rbacv1.ClusterRoleBinding) domainresource.ClusterRoleBindingView {
	return domainresource.ClusterRoleBindingView{
		Name: item.Name, RoleRef: formatRoleRef(item.RoleRef), Subjects: formatSubjects(item.Subjects),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleBindingDetail(item rbacv1.ClusterRoleBinding) domainresource.ClusterRoleBindingDetailView {
	subjects := formatSubjects(item.Subjects)
	sort.Strings(subjects)
	return domainresource.ClusterRoleBindingDetailView{
		Name: item.Name, Labels: item.Labels, Annotations: item.Annotations,
		RoleRef: formatRoleRef(item.RoleRef), Subjects: subjects,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func formatSubjects(subjects []rbacv1.Subject) []string {
	formatted := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			formatted = append(formatted, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		formatted = append(formatted, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	return formatted
}

func formatRoleRef(ref rbacv1.RoleRef) string {
	return fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
}

func aggregationRuleCount(rule *rbacv1.AggregationRule) int {
	if rule == nil {
		return 0
	}
	return len(rule.ClusterRoleSelectors)
}

var _ appresource.DirectRBACReader = (*Direct)(nil)
