package resource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
func (s *Service) listDirectNamespaces(ctx context.Context, clusterID string) ([]corev1.Namespace, string, error) {
	if items, err := s.cache.ListNamespaces(clusterID); err == nil {
		return items, "cache", nil
	} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
		return nil, "cache", err
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().Nodes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}
func (s *Service) getDirectNode(ctx context.Context, clusterID, name string) (*corev1.Node, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().Nodes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
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
func (s *Service) createDirectNamespace(ctx context.Context, clusterID string, input domainresource.NamespaceUpsertInput) (*corev1.Namespace, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: namespace name is required", apperrors.ErrInvalidArgument)
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	return bundle.Typed.CoreV1().Namespaces().Delete(queryCtx, namespace, metav1.DeleteOptions{})
}
func (s *Service) updateDirectNode(ctx context.Context, clusterID, nodeName string, input domainresource.NodeUpdateInput) (*corev1.Node, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return err
	}
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
func populateAllowedActionsNamespaces(items []domainresource.NamespaceView, decision domainaccess.Decision) {
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
