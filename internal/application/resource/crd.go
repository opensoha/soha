package resource

import (
	"context"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

func (c *CustomResources) ListCRDs(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.CRDView, error) {
	connection, decision, err := c.authorize(ctx, principal, clusterID, "", "CRD", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	items, source, err := c.listCRDs(ctx, connection)
	if err != nil {
		return nil, err
	}
	populateAllowedActionsCRDs(items, decision)
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, "", "CRD", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed crds via %s", source))
	return items, nil
}

func (c *CustomResources) ListCRDResources(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace string) ([]domainresource.CustomResourceView, error) {
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, crdName)
	if err != nil {
		return nil, err
	}
	authNamespace := normalizeCustomResourceNamespace(namespace, definition.Namespaced)
	_, decision, err := c.authorize(ctx, principal, clusterID, authNamespace, definition.Kind, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	items, source, err := c.listCustomResources(ctx, connection, definition, namespace)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].AllowedActions = stringifyActions(decision.AllowedActions)
	}
	if definition.Namespaced {
		items = filterScopedNamespaceItems(items, decision, func(item domainresource.CustomResourceView) string { return item.Namespace })
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, authNamespace, definition.Kind, "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed custom resources for crd %s via %s", crdName, source))
	return items, nil
}

func (c *CustomResources) CreateCRDResourceFromYAML(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, content string) (domainresource.ResourceYAMLView, error) {
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, crdName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	metadata, effectiveNamespace, err := inspectCustomResourceYAML(definition, content, namespace, "")
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, _, err := c.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionCreate); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	created, source, err := c.createCustomResource(ctx, connection, definition, effectiveNamespace, content)
	if err != nil {
		_ = c.recordAudit(ctx, principal, clusterID, effectiveNamespace, definition.Kind, metadata.Name, string(domainaccess.ActionCreate), "failure", err.Error())
		return domainresource.ResourceYAMLView{}, err
	}
	summary := "created custom resource from yaml"
	if source == "agent" {
		summary += " via agent"
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, created.Name, string(domainaccess.ActionCreate), "success", summary)
	c.recordOperation(ctx, principal, "platform.custom_resource.create", connection.Summary.ID, effectiveNamespace, definition.Kind, created.Name, summary, map[string]any{"crdName": crdName})
	return created, nil
}

func (c *CustomResources) GetCRDResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, crdName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	effectiveNamespace, err := requiredCustomResourceNamespace(definition, namespace)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, _, err := c.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionView); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	item, source, err := c.getCustomResourceYAML(ctx, connection, definition, effectiveNamespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	summary := "viewed custom resource yaml"
	if source == "agent" {
		summary += " via agent"
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionView), "success", summary)
	return item, nil
}

func (c *CustomResources) ApplyCRDResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, crdName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	_, effectiveNamespace, err := inspectCustomResourceYAML(definition, content, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, _, err := c.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionUpdate); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	updated, source, err := c.applyCustomResourceYAML(ctx, connection, definition, effectiveNamespace, name, content)
	if err != nil {
		_ = c.recordAudit(ctx, principal, clusterID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.ResourceYAMLView{}, err
	}
	summary := "applied custom resource yaml"
	if source == "agent" {
		summary += " via agent"
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionUpdate), "success", summary)
	c.recordOperation(ctx, principal, "platform.custom_resource.apply", connection.Summary.ID, effectiveNamespace, definition.Kind, name, summary, map[string]any{"crdName": crdName})
	return updated, nil
}

func (c *CustomResources) DeleteCRDResource(ctx context.Context, principal domainidentity.Principal, clusterID, crdName, namespace, name string) error {
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, crdName)
	if err != nil {
		return err
	}
	effectiveNamespace, err := requiredCustomResourceNamespace(definition, namespace)
	if err != nil {
		return err
	}
	if _, _, err := c.authorize(ctx, principal, clusterID, effectiveNamespace, definition.Kind, domainaccess.ActionDelete); err != nil {
		return err
	}
	source, err := c.deleteCustomResource(ctx, connection, definition, effectiveNamespace, name)
	if err != nil {
		_ = c.recordAudit(ctx, principal, clusterID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
		return err
	}
	summary := "deleted custom resource"
	if source == "agent" {
		summary += " via agent"
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, effectiveNamespace, definition.Kind, name, string(domainaccess.ActionDelete), "success", summary)
	c.recordOperation(ctx, principal, "platform.custom_resource.delete", connection.Summary.ID, effectiveNamespace, definition.Kind, name, summary, map[string]any{"crdName": crdName})
	return nil
}

func (c *CustomResources) listCRDs(ctx context.Context, connection domaincluster.Connection) ([]domainresource.CRDView, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return nil, "agent", err
		}
		items, err := client.ListCRDs(ctx)
		return items, "agent", wrapAgentResourceError(err)
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return nil, "live", err
	}
	items, err := direct.ListCRDs(ctx, connection.Summary.ID)
	return items, "live", err
}

func (c *CustomResources) listCustomResources(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace string) ([]domainresource.CustomResourceView, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return nil, "agent", err
		}
		items, err := client.ListCustomResources(ctx, definition.AgentDefinition(), namespace)
		return items, "agent", wrapAgentResourceError(err)
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return nil, "live", err
	}
	items, err := direct.ListCustomResources(ctx, connection.Summary.ID, definition.AgentDefinition(), namespace)
	return items, "live", err
}

func (c *CustomResources) createCustomResource(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace, content string) (domainresource.ResourceYAMLView, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, "agent", err
		}
		item, err := client.CreateCustomResourceYAML(ctx, definition.AgentDefinition(), namespace, content)
		return item, "agent", wrapAgentResourceError(err)
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return domainresource.ResourceYAMLView{}, "live", err
	}
	item, err := direct.CreateCustomResourceYAML(ctx, connection.Summary.ID, definition.AgentDefinition(), namespace, content)
	return item, "live", err
}

func (c *CustomResources) getCustomResourceYAML(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace, name string) (domainresource.ResourceYAMLView, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, "agent", err
		}
		item, err := client.GetCustomResourceYAML(ctx, definition.AgentDefinition(), namespace, name)
		return item, "agent", wrapAgentResourceError(err)
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return domainresource.ResourceYAMLView{}, "live", err
	}
	item, err := direct.GetCustomResourceYAML(ctx, connection.Summary.ID, definition.AgentDefinition(), namespace, name)
	return item, "live", err
}

func (c *CustomResources) applyCustomResourceYAML(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace, name, content string) (domainresource.ResourceYAMLView, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, "agent", err
		}
		item, err := client.ApplyCustomResourceYAML(ctx, definition.AgentDefinition(), namespace, name, content)
		return item, "agent", wrapAgentResourceError(err)
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return domainresource.ResourceYAMLView{}, "live", err
	}
	item, err := direct.ApplyCustomResourceYAML(ctx, connection.Summary.ID, definition.AgentDefinition(), namespace, name, content)
	return item, "live", err
}

func (c *CustomResources) deleteCustomResource(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace, name string) (string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return "agent", err
		}
		return "agent", wrapAgentResourceError(client.DeleteCustomResource(ctx, definition.AgentDefinition(), namespace, name))
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return "live", err
	}
	return "live", direct.DeleteCustomResource(ctx, connection.Summary.ID, definition.AgentDefinition(), namespace, name)
}

func (c *CustomResources) resolveCRDResourceDefinition(ctx context.Context, connection domaincluster.Connection, crdName string) (crdResourceDefinition, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return crdResourceDefinition{}, err
		}
		items, err := client.ListCRDs(ctx)
		if err != nil {
			return crdResourceDefinition{}, wrapAgentResourceError(err)
		}
		for _, item := range items {
			if item.Name == crdName {
				return crdResourceDefinitionFromView(item)
			}
		}
		return crdResourceDefinition{}, fmt.Errorf("%w: crd %s", apperrors.ErrNotFound, crdName)
	}
	direct, err := c.directCustomResources()
	if err != nil {
		return crdResourceDefinition{}, err
	}
	definition, err := direct.ResolveCRD(ctx, connection.Summary.ID, crdName)
	if err != nil {
		return crdResourceDefinition{}, err
	}
	return crdResourceDefinitionFromDomain(definition)
}

func (c *CustomResources) directCustomResources() (DirectCustomResource, error) {
	if c.direct == nil {
		return nil, fmt.Errorf("%w: direct custom resource adapter is not configured", apperrors.ErrClusterUnready)
	}
	return c.direct, nil
}

func wrapAgentResourceError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
}

func populateAllowedActionsCRDs(items []domainresource.CRDView, decision domainaccess.Decision) {
	for index := range items {
		if len(items[index].AllowedActions) == 0 {
			items[index].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func (d crdResourceDefinition) AgentDefinition() domainresource.CRDResourceDefinition {
	return domainresource.CRDResourceDefinition{CRDName: d.CRDName, Kind: d.Kind, Group: d.Group, Version: d.Version, Resource: d.Resource, Namespaced: d.Namespaced}
}

func crdResourceDefinitionFromDomain(item domainresource.CRDResourceDefinition) (crdResourceDefinition, error) {
	if strings.TrimSpace(item.Group) == "" || strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.Resource) == "" || strings.TrimSpace(item.Version) == "" {
		return crdResourceDefinition{}, fmt.Errorf("%w: crd %s is missing required group, kind, plural, or version metadata", apperrors.ErrInvalidArgument, item.CRDName)
	}
	return crdResourceDefinition{CRDName: item.CRDName, Kind: item.Kind, Group: item.Group, Version: item.Version, Resource: item.Resource, Namespaced: item.Namespaced}, nil
}

func crdResourceDefinitionFromView(item domainresource.CRDView) (crdResourceDefinition, error) {
	version := strings.TrimSpace(item.Version)
	if version == "" && len(item.Versions) > 0 {
		version = item.Versions[0]
	}
	namespaced, err := namespacedFromCRDScope(item.Scope, item.Name)
	if err != nil {
		return crdResourceDefinition{}, err
	}
	return crdResourceDefinitionFromDomain(domainresource.CRDResourceDefinition{
		CRDName: item.Name, Kind: item.Kind, Group: item.Group, Version: version, Resource: item.Plural, Namespaced: namespaced,
	})
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

type customResourceMetadata struct {
	Name       string
	Namespace  string
	Kind       string
	APIVersion string
}

func inspectCustomResourceYAML(definition crdResourceDefinition, content, namespace, expectedName string) (customResourceMetadata, string, error) {
	if strings.TrimSpace(content) == "" {
		return customResourceMetadata{}, "", fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	var document struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return customResourceMetadata{}, "", fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	kind := firstNonEmpty(document.Kind, definition.Kind)
	if !strings.EqualFold(kind, definition.Kind) {
		return customResourceMetadata{}, "", fmt.Errorf("%w: yaml kind %s does not match target %s", apperrors.ErrInvalidArgument, kind, definition.Kind)
	}
	name := firstNonEmpty(document.Metadata.Name, expectedName)
	if name == "" {
		return customResourceMetadata{}, "", fmt.Errorf("%w: yaml metadata.name is required", apperrors.ErrInvalidArgument)
	}
	if expectedName = strings.TrimSpace(expectedName); expectedName != "" && name != expectedName {
		return customResourceMetadata{}, "", fmt.Errorf("%w: yaml metadata.name does not match target resource", apperrors.ErrInvalidArgument)
	}
	effectiveNamespace, err := requiredCustomResourceNamespace(definition, firstNonEmpty(document.Metadata.Namespace, namespace))
	if err != nil {
		return customResourceMetadata{}, "", err
	}
	return customResourceMetadata{Name: name, Namespace: effectiveNamespace, Kind: kind, APIVersion: firstNonEmpty(document.APIVersion, definition.Group+"/"+definition.Version)}, effectiveNamespace, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func normalizeCustomResourceNamespace(namespace string, namespaced bool) string {
	if !namespaced {
		return ""
	}
	return strings.TrimSpace(namespace)
}

func (c *CustomResources) authorizeCRDDefinitionAccess(ctx context.Context, principal domainidentity.Principal, clusterID string, action domainaccess.Action) (domaincluster.Connection, error) {
	connection, _, err := c.authorize(ctx, principal, clusterID, "", "CRD", action)
	return connection, err
}
