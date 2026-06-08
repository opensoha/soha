package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
func (s *Service) getDirectCustomResourceYAML(ctx context.Context, clusterID string, definition crdResourceDefinition, namespace, name string) (domainresource.ResourceYAMLView, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
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
func (s *Service) deleteDirectCustomResource(ctx context.Context, clusterID string, definition crdResourceDefinition, namespace, name string) error {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	var resource dynamic.ResourceInterface
	if definition.Namespaced {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource()).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(definition.GroupVersionResource())
	}
	return resource.Delete(queryCtx, name, metav1.DeleteOptions{})
}
func (s *Service) applyDirectCustomResourceYAML(ctx context.Context, clusterID string, definition crdResourceDefinition, item *unstructured.Unstructured, namespace, name string) (domainresource.ResourceYAMLView, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
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
func (s *Service) listDirectCRDs(ctx context.Context, clusterID string) ([]domainresource.CRDView, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return nil, err
	}
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
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return crdResourceDefinition{}, err
	}
	defer cancel()
	crdGVR := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	item, err := bundle.Dynamic.Resource(crdGVR).Get(queryCtx, crdName, metav1.GetOptions{})
	if err != nil {
		return crdResourceDefinition{}, err
	}
	return parseCRDResourceDefinition(*item)
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
func populateAllowedActionsCRDs(items []domainresource.CRDView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
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
