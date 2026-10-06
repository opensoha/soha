package resource

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Broad permissions cover every name. Only name-restricted grants require an
// individual review. This cache is confined to one list; mutations remain fresh.
func (c *CustomResources) populateCustomResourceListActions(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, items []domainresource.CustomResourceView, allowed []string, namespaceActions map[string][]string) error {
	for index := range items {
		namespace := normalizeCustomResourceNamespace(items[index].Namespace, definition.Namespaced)
		actions, checked := namespaceActions[namespace]
		if !checked {
			var err error
			actions, err = c.customResourceActions(ctx, connection, definition, namespace, "", allowed)
			if err != nil {
				return err
			}
			namespaceActions[namespace] = actions
		}
		if needsNamedCustomResourceReview(configuredCustomResourceActions(connection, definition, namespace, allowed), actions) {
			var err error
			actions, err = c.customResourceActions(ctx, connection, definition, namespace, items[index].Name, allowed)
			if err != nil {
				return err
			}
		}
		items[index].AllowedActions = slices.Clone(actions)
	}
	return nil
}

// A scoped Agent grant cannot list across all namespaces. Aggregate only its
// explicit namespaces, preserving both the caller's scope and Kubernetes RBAC.
func (c *CustomResources) listGrantedCustomResourceNamespaces(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, definition crdResourceDefinition, decision domainaccess.Decision) ([]domainresource.CustomResourceView, error) {
	var rules []domaincluster.AgentCustomResourceRule
	data, _ := json.Marshal(connection.Metadata["agent_custom_resource_rules"])
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, apperrors.ErrAccessDenied
	}
	namespaces := []string{}
	for _, rule := range rules {
		if rule.APIGroup == definition.Group && slices.Contains(rule.Resources, definition.Resource) && slices.Contains(rule.Verbs, "list") {
			namespaces = append(namespaces, rule.Namespaces...)
		}
	}
	slices.Sort(namespaces)
	namespaces = slices.Compact(namespaces)
	namespaces = filterScopedNamespaceItems(namespaces, decision, func(namespace string) string { return namespace })
	items := []domainresource.CustomResourceView{}
	permitted := false
	for _, namespace := range namespaces {
		_, scoped, err := c.authorizeCustomResourceAccess(ctx, principal, connection.Summary.ID, namespace, definition.Kind, domainaccess.ActionList)
		if errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		allowed := stringifyActions(scoped.AllowedActions)
		review := append(slices.Clone(allowed), string(domainaccess.ActionList))
		actions, err := c.customResourceActions(ctx, connection, definition, namespace, "", review)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(actions, string(domainaccess.ActionList)) {
			continue
		}
		permitted = true
		actions = slices.DeleteFunc(actions, func(action string) bool { return !slices.Contains(allowed, action) })
		listed, _, err := c.listCustomResources(ctx, connection, definition, namespace)
		if err != nil {
			return nil, err
		}
		if err := c.populateCustomResourceListActions(ctx, connection, definition, listed, allowed, map[string][]string{namespace: actions}); err != nil {
			return nil, err
		}
		items = append(items, listed...)
	}
	if !permitted {
		return nil, apperrors.ErrAccessDenied
	}
	return items, nil
}

func configuredCustomResourceActions(connection domaincluster.Connection, definition crdResourceDefinition, namespace string, allowed []string) []string {
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return allowed
	}
	var rules []domaincluster.AgentCustomResourceRule
	data, _ := json.Marshal(connection.Metadata["agent_custom_resource_rules"])
	if err := json.Unmarshal(data, &rules); err != nil {
		return []string{}
	}
	granted := []string{}
	for _, action := range allowed {
		verb := map[string]string{"list": "list", "view": "get", "create": "create", "update": "update", "delete": "delete"}[action]
		for _, rule := range rules {
			if rule.APIGroup == definition.Group && slices.Contains(rule.Resources, definition.Resource) && slices.Contains(rule.Verbs, verb) && (len(rule.Namespaces) == 0 || (namespace != "" && slices.Contains(rule.Namespaces, namespace))) {
				granted = append(granted, action)
				break
			}
		}
	}
	return granted
}

func needsNamedCustomResourceReview(allowed, broad []string) bool {
	for _, action := range allowed {
		if (action == "view" || action == "update" || action == "delete") && !slices.Contains(broad, action) {
			return true
		}
	}
	return false
}
