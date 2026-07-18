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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (d *Direct) ListServiceAccounts(ctx context.Context, clusterID, namespace string) ([]domainresource.ServiceAccountView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, true, namespace)
	if err != nil {
		return nil, err
	}
	return mapServiceAccountTable(table)
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
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true, namespace)
	if err != nil {
		return nil, err
	}
	return mapRoleTable(table)
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
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true, namespace)
	if err != nil {
		return nil, err
	}
	return mapRoleBindingTable(table)
}

func (d *Direct) ListRoleBindingsForServiceAccount(ctx context.Context, clusterID, namespace, name string) ([]domainresource.RoleBindingView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	views := make([]domainresource.RoleBindingView, 0)
	options := metav1.ListOptions{Limit: tableListPageSize}
	for {
		items, err := bundle.Typed.RbacV1().RoleBindings(namespace).List(queryCtx, options)
		if err != nil {
			return nil, err
		}
		for _, item := range items.Items {
			if referencesServiceAccount(item.Subjects, namespace, name, item.Namespace) {
				views = append(views, mapRoleBinding(item))
			}
		}
		if items.Continue == "" {
			return views, nil
		}
		if items.Continue == options.Continue {
			return nil, fmt.Errorf("rolebinding listing returned a repeated continue token")
		}
		options.Continue = items.Continue
	}
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
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false, "")
	if err != nil {
		return nil, err
	}
	return mapClusterRoleTable(table)
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
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false, "")
	if err != nil {
		return nil, err
	}
	return mapClusterRoleBindingTable(table)
}

func (d *Direct) ListClusterRoleBindingsForServiceAccount(ctx context.Context, clusterID, namespace, name string) ([]domainresource.ClusterRoleBindingView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	views := make([]domainresource.ClusterRoleBindingView, 0)
	options := metav1.ListOptions{Limit: tableListPageSize}
	for {
		items, err := bundle.Typed.RbacV1().ClusterRoleBindings().List(queryCtx, options)
		if err != nil {
			return nil, err
		}
		for _, item := range items.Items {
			if referencesServiceAccount(item.Subjects, namespace, name, "") {
				views = append(views, mapClusterRoleBinding(item))
			}
		}
		if items.Continue == "" {
			return views, nil
		}
		if items.Continue == options.Continue {
			return nil, fmt.Errorf("clusterrolebinding listing returned a repeated continue token")
		}
		options.Continue = items.Continue
	}
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

func mapServiceAccountTable(table metav1.Table) ([]domainresource.ServiceAccountView, error) {
	return tableViews(table, func(_ metav1.TableRow, metadata metav1.Object) (domainresource.ServiceAccountView, error) {
		return domainresource.ServiceAccountView{Name: metadata.GetName(), Namespace: metadata.GetNamespace(), AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
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

func mapRoleTable(table metav1.Table) ([]domainresource.RoleView, error) {
	return tableViews(table, func(_ metav1.TableRow, metadata metav1.Object) (domainresource.RoleView, error) {
		return domainresource.RoleView{Name: metadata.GetName(), Namespace: metadata.GetNamespace(), AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
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
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleBindingTable(table metav1.Table) ([]domainresource.RoleBindingView, error) {
	roleColumn := tableColumnIndex(table.ColumnDefinitions, "Role")
	if roleColumn < 0 {
		return nil, fmt.Errorf("role binding table is missing Role column")
	}
	return tableViews(table, func(row metav1.TableRow, metadata metav1.Object) (domainresource.RoleBindingView, error) {
		roleRef, err := tableStringCell(row.Cells, roleColumn)
		if err != nil {
			return domainresource.RoleBindingView{}, err
		}
		return domainresource.RoleBindingView{Name: metadata.GetName(), Namespace: metadata.GetNamespace(), RoleRef: roleRef, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
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

func mapClusterRoleTable(table metav1.Table) ([]domainresource.ClusterRoleView, error) {
	return tableViews(table, func(_ metav1.TableRow, metadata metav1.Object) (domainresource.ClusterRoleView, error) {
		return domainresource.ClusterRoleView{Name: metadata.GetName(), AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
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
		Name: item.Name, RoleRef: formatRoleRef(item.RoleRef),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleBindingTable(table metav1.Table) ([]domainresource.ClusterRoleBindingView, error) {
	roleColumn := tableColumnIndex(table.ColumnDefinitions, "Role")
	if roleColumn < 0 {
		return nil, fmt.Errorf("cluster role binding table is missing Role column")
	}
	return tableViews(table, func(row metav1.TableRow, metadata metav1.Object) (domainresource.ClusterRoleBindingView, error) {
		roleRef, err := tableStringCell(row.Cells, roleColumn)
		if err != nil {
			return domainresource.ClusterRoleBindingView{}, err
		}
		return domainresource.ClusterRoleBindingView{Name: metadata.GetName(), RoleRef: roleRef, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
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

func referencesServiceAccount(subjects []rbacv1.Subject, namespace, name, defaultNamespace string) bool {
	for _, subject := range subjects {
		subjectNamespace := subject.Namespace
		if subjectNamespace == "" {
			subjectNamespace = defaultNamespace
		}
		if subject.Kind == "ServiceAccount" && subjectNamespace == namespace && subject.Name == name {
			return true
		}
	}
	return false
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
