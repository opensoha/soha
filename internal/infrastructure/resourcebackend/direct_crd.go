package resourcebackend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

var crdGVR = schema.GroupVersionResource{
	Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
}

func (d *Direct) ListCRDs(ctx context.Context, clusterID string) ([]domainresource.CRDView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	items, err := listPartialMetadata(queryCtx, bundle, crdGVR, false, "", metav1.ListOptions{})
	if err != nil {
		if !metadataListUnsupported(err) {
			return nil, err
		}
		return listFullCRDs(queryCtx, bundle)
	}
	resources, discoveryErr := listServerResources(bundle, 8*time.Second)
	if discoveryErr != nil && len(resources) == 0 {
		return listFullCRDs(queryCtx, bundle)
	}
	views, missing := mapCRDMetadata(items, resources)
	for _, item := range missing {
		full, err := bundle.Dynamic.Resource(crdGVR).Get(queryCtx, item.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		views = append(views, mapCRD(*full))
	}
	sort.SliceStable(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	return views, nil
}

func listServerResources(bundle *k8sinfra.Bundle, timeout time.Duration) ([]*metav1.APIResourceList, error) {
	config := rest.CopyConfig(bundle.RESTConfig)
	config.Timeout = timeout
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	_, resources, err := client.ServerGroupsAndResources()
	return resources, err
}

func listFullCRDs(ctx context.Context, bundle *k8sinfra.Bundle) ([]domainresource.CRDView, error) {
	items, err := bundle.Dynamic.Resource(crdGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.CRDView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapCRD(item))
	}
	return views, nil
}

func (d *Direct) ResolveCRD(ctx context.Context, clusterID, crdName string) (domainresource.CRDResourceDefinition, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.CRDResourceDefinition{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Dynamic.Resource(crdGVR).Get(queryCtx, crdName, metav1.GetOptions{})
	if err != nil {
		return domainresource.CRDResourceDefinition{}, err
	}
	return parseCRDDefinition(*item)
}

func (d *Direct) ListCustomResources(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition, namespace string) ([]domainresource.CustomResourceView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	effectiveNamespace := strings.TrimSpace(namespace)
	if !definition.Namespaced {
		var err error
		effectiveNamespace, err = customResourceNamespace(definition, namespace)
		if err != nil {
			return nil, err
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	gvr := schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}
	items, err := listPartialMetadata(queryCtx, bundle, gvr, definition.Namespaced, effectiveNamespace, metav1.ListOptions{})
	if err != nil {
		if !metadataListUnsupported(err) {
			return nil, err
		}
		return d.listFullCustomResources(queryCtx, clusterID, definition, namespace)
	}
	return mapPartialCustomResources(items, definition), nil
}

func (d *Direct) listFullCustomResources(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition, namespace string) ([]domainresource.CustomResourceView, error) {
	if definition.Namespaced && strings.TrimSpace(namespace) == "" {
		items, err := d.listCustomResourcesAcrossNamespaces(ctx, clusterID, definition)
		if err != nil {
			return nil, err
		}
		return mapCustomResources(items, definition), nil
	}
	effectiveNamespace, err := customResourceNamespace(definition, namespace)
	if err != nil {
		return nil, err
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	items, err := customResourceInterface(bundle.Dynamic, definition, effectiveNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return mapCustomResources(items.Items, definition), nil
}

func (d *Direct) CreateCustomResourceYAML(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition, namespace, content string) (domainresource.ResourceYAMLView, error) {
	item, effectiveNamespace, err := parseCustomResourceYAML(definition, content, namespace, "")
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	item.SetResourceVersion("")
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	created, err := customResourceInterface(bundle.Dynamic, definition, effectiveNamespace).Create(queryCtx, item, metav1.CreateOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return renderCustomResource(definition.Kind, created)
}

func (d *Direct) GetCustomResourceYAML(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition, namespace, name string) (domainresource.ResourceYAMLView, error) {
	effectiveNamespace, err := customResourceNamespace(definition, namespace)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := customResourceInterface(bundle.Dynamic, definition, effectiveNamespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
	return renderCustomResource(definition.Kind, item)
}

func (d *Direct) ApplyCustomResourceYAML(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	item, effectiveNamespace, err := parseCustomResourceYAML(definition, content, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource := customResourceInterface(bundle.Dynamic, definition, effectiveNamespace)
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
	return renderCustomResource(definition.Kind, updated)
}

func (d *Direct) DeleteCustomResource(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition, namespace, name string) error {
	effectiveNamespace, err := customResourceNamespace(definition, namespace)
	if err != nil {
		return err
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return customResourceInterface(bundle.Dynamic, definition, effectiveNamespace).Delete(queryCtx, name, metav1.DeleteOptions{})
}

func (d *Direct) listCustomResourcesAcrossNamespaces(ctx context.Context, clusterID string, definition domainresource.CRDResourceDefinition) ([]unstructured.Unstructured, error) {
	return listAcrossNamespaces(ctx, d, clusterID, 5*time.Second, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]unstructured.Unstructured, error) {
		items, err := customResourceInterface(bundle.Dynamic, definition, namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return items.Items, nil
	})
}

func customResourceInterface(client dynamic.Interface, definition domainresource.CRDResourceDefinition, namespace string) dynamic.ResourceInterface {
	resource := client.Resource(schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource})
	if definition.Namespaced {
		return resource.Namespace(namespace)
	}
	return resource
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
		if name, _ := value["name"].(string); strings.TrimSpace(name) != "" {
			versions = append(versions, name)
		}
	}
	view := domainresource.CRDView{
		Name: item.GetName(), Group: group, Scope: scope, Kind: kind, Plural: plural,
		Versions: versions, CreatedAt: item.GetCreationTimestamp().Time.UTC().Format(time.RFC3339),
		AgeSeconds: secondsSince(item.GetCreationTimestamp().Time),
	}
	if len(versions) > 0 {
		view.Version = versions[0]
	}
	return view
}

type discoveredCRD struct {
	group      string
	plural     string
	kind       string
	scope      string
	versions   []string
	versionSet map[string]struct{}
}

func mapCRDMetadata(items []metav1.PartialObjectMetadata, resourceLists []*metav1.APIResourceList) ([]domainresource.CRDView, []metav1.PartialObjectMetadata) {
	discovered := discoverCRDs(resourceLists)
	views := make([]domainresource.CRDView, 0, len(items))
	missing := make([]metav1.PartialObjectMetadata, 0)
	for _, item := range items {
		entry, ok := discovered[item.Name]
		if !ok {
			missing = append(missing, item)
			continue
		}
		views = append(views, domainresource.CRDView{
			Name:       item.Name,
			Group:      entry.group,
			Scope:      entry.scope,
			Kind:       entry.kind,
			Plural:     entry.plural,
			Version:    firstValue(entry.versions...),
			Versions:   entry.versions,
			CreatedAt:  item.CreationTimestamp.Time.UTC().Format(time.RFC3339),
			AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		})
	}
	return views, missing
}

func discoverCRDs(resourceLists []*metav1.APIResourceList) map[string]*discoveredCRD {
	result := make(map[string]*discoveredCRD)
	for _, resourceList := range resourceLists {
		if resourceList == nil {
			continue
		}
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil || groupVersion.Group == "" {
			continue
		}
		for _, resource := range resourceList.APIResources {
			if strings.Contains(resource.Name, "/") {
				continue
			}
			name := resource.Name + "." + groupVersion.Group
			entry := result[name]
			if entry == nil {
				scope := "Cluster"
				if resource.Namespaced {
					scope = "Namespaced"
				}
				entry = &discoveredCRD{
					group: groupVersion.Group, plural: resource.Name, kind: resource.Kind, scope: scope,
					versionSet: make(map[string]struct{}),
				}
				result[name] = entry
			}
			if _, exists := entry.versionSet[groupVersion.Version]; !exists {
				entry.versionSet[groupVersion.Version] = struct{}{}
				entry.versions = append(entry.versions, groupVersion.Version)
			}
		}
	}
	return result
}

func mapCustomResources(items []unstructured.Unstructured, definition domainresource.CRDResourceDefinition) []domainresource.CustomResourceView {
	views := make([]domainresource.CustomResourceView, 0, len(items))
	for _, item := range items {
		apiVersion := strings.TrimSpace(item.GetAPIVersion())
		if apiVersion == "" {
			apiVersion = definition.Group + "/" + definition.Version
		}
		views = append(views, domainresource.CustomResourceView{
			APIVersion: apiVersion, Kind: definition.Kind, Name: item.GetName(), Namespace: item.GetNamespace(),
			Labels: item.GetLabels(), CreatedAt: item.GetCreationTimestamp().Time.UTC().Format(time.RFC3339),
			AgeSeconds: secondsSince(item.GetCreationTimestamp().Time),
		})
	}
	return views
}

func mapPartialCustomResources(items []metav1.PartialObjectMetadata, definition domainresource.CRDResourceDefinition) []domainresource.CustomResourceView {
	views := make([]domainresource.CustomResourceView, 0, len(items))
	for _, item := range items {
		views = append(views, domainresource.CustomResourceView{
			APIVersion: definition.Group + "/" + definition.Version,
			Kind:       definition.Kind,
			Name:       item.Name,
			Namespace:  item.Namespace,
			Labels:     item.Labels,
			CreatedAt:  item.CreationTimestamp.Time.UTC().Format(time.RFC3339),
			AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		})
	}
	return views
}

func parseCRDDefinition(item unstructured.Unstructured) (domainresource.CRDResourceDefinition, error) {
	group, _, _ := unstructured.NestedString(item.Object, "spec", "group")
	kind, _, _ := unstructured.NestedString(item.Object, "spec", "names", "kind")
	resource, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
	scope, _, _ := unstructured.NestedString(item.Object, "spec", "scope")
	version, err := servedCRDVersion(item)
	if err != nil {
		return domainresource.CRDResourceDefinition{}, err
	}
	if strings.TrimSpace(group) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(resource) == "" {
		return domainresource.CRDResourceDefinition{}, fmt.Errorf("%w: crd %s is missing required group, kind, or plural metadata", apperrors.ErrInvalidArgument, item.GetName())
	}
	namespaced, err := namespacedCRDScope(scope, item.GetName())
	if err != nil {
		return domainresource.CRDResourceDefinition{}, err
	}
	return domainresource.CRDResourceDefinition{CRDName: item.GetName(), Kind: kind, Group: group, Version: version, Resource: resource, Namespaced: namespaced}, nil
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
		if served, _ := version["served"].(bool); served && strings.TrimSpace(name) != "" {
			return name, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("%w: crd %s does not expose any version metadata", apperrors.ErrInvalidArgument, item.GetName())
}

func namespacedCRDScope(scope, crdName string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "namespaced":
		return true, nil
	case "cluster":
		return false, nil
	default:
		return false, fmt.Errorf("%w: crd %s has unsupported scope %q", apperrors.ErrInvalidArgument, crdName, scope)
	}
}

func customResourceNamespace(definition domainresource.CRDResourceDefinition, namespace string) (string, error) {
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

func parseCustomResourceYAML(definition domainresource.CRDResourceDefinition, content, namespace, expectedName string) (*unstructured.Unstructured, string, error) {
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
	effectiveNamespace, err := customResourceNamespace(definition, firstValue(item.GetNamespace(), namespace))
	if err != nil {
		return nil, "", err
	}
	item.SetNamespace(effectiveNamespace)
	return item, effectiveNamespace, nil
}

func renderCustomResource(kind string, item *unstructured.Unstructured) (domainresource.ResourceYAMLView, error) {
	rendered, err := yaml.Marshal(item.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: kind, Name: item.GetName(), Namespace: item.GetNamespace(), Content: string(rendered)}, nil
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

var _ appresource.DirectCustomResource = (*Direct)(nil)
