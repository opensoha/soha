package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
		return domainresource.ConfigMapDetailView{}, unsupportedAgentOperation("configmap detail is not supported for agent-connected clusters yet")
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
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
		return domainresource.SecretDetailView{}, unsupportedAgentOperation("secret detail is not supported for agent-connected clusters yet")
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
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
		return domainresource.ResourceYAMLView{}, unsupportedAgentOperation("yaml create is not supported for agent-connected clusters yet")
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectIngressClasses(ctx context.Context, clusterID string) ([]networkingv1.IngressClass, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().IngressClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectPriorityClasses(ctx context.Context, clusterID string) ([]schedulingv1.PriorityClass, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.SchedulingV1().PriorityClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectRuntimeClasses(ctx context.Context, clusterID string) ([]nodev1.RuntimeClass, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.NodeV1().RuntimeClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectMutatingWebhookConfigurations(ctx context.Context, clusterID string) ([]admissionregistrationv1.MutatingWebhookConfiguration, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.AdmissionregistrationV1().MutatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectValidatingWebhookConfigurations(ctx context.Context, clusterID string) ([]admissionregistrationv1.ValidatingWebhookConfiguration, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().ReplicationControllers(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
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
