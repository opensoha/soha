package resource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type topologyCollectionPolicyKey struct{}

func (n *Network) GetNetworkTopology(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (domainresource.NetworkTopologyView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = context.WithValue(queryCtx, graphNamespacePolicyKey{}, true)
	connection, _, err := n.authorizeTopologyRead(ctx, principal, clusterID, namespace, "network", "NetworkTopology", domainaccess.ActionView)
	if err != nil {
		return domainresource.NetworkTopologyView{}, err
	}
	if (connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent && n.topologyAgent == nil) || (connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent && n.topologyDirect == nil) {
		return n.legacyTopologyWithCoverage(ctx, principal, connection, namespace)
	}
	// Keep a slow optional source's policy lookup from consuming the entire
	// request before any already-authorized Kubernetes resources can be read.
	deadline, _ := ctx.Deadline()
	authorizationCtx, finishAuthorization := context.WithTimeout(ctx, time.Until(deadline)/2)
	request, coverage := n.topologySourceQueries(authorizationCtx, principal, connection, namespace)
	finishAuthorization()
	snapshot := contractresource.NetworkTopologySnapshot{Version: contractresource.NetworkTopologySnapshotFeature}
	if len(request.Queries) > 0 {
		snapshot, err = n.readTopologySnapshot(ctx, connection, request)
		if errors.Is(err, contractresource.ErrNetworkTopologySnapshotUnsupported) {
			return n.legacyTopologyWithCoverage(ctx, principal, connection, namespace)
		}
		if err != nil {
			snapshot.Sources = nil
			for _, query := range request.Queries {
				definition, _ := contractresource.LookupNetworkTopologySource(query.SourceID)
				snapshot.Sources = append(snapshot.Sources, contractresource.NetworkTopologySourceResult{Coverage: topologyCoverage(definition, query.Namespace, "error", "source_unavailable")})
			}
		}
	}
	snapshot = validateTopologySnapshot(snapshot, request, clusterID)
	for _, item := range coverage {
		snapshot.Sources = append(snapshot.Sources, contractresource.NetworkTopologySourceResult{Coverage: item})
	}
	view := domainresource.BuildNetworkTopology(clusterID, namespace, snapshot)
	return view, nil
}

func (n *Network) readTopologySnapshot(ctx context.Context, connection domaincluster.Connection, request contractresource.NetworkTopologySnapshotRequest) (contractresource.NetworkTopologySnapshot, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := resolveAgentClient(n.topologyAgent, connection)
		if err != nil {
			return contractresource.NetworkTopologySnapshot{}, err
		}
		return client.NetworkTopologySnapshot(ctx, request)
	}
	return n.topologyDirect.NetworkTopologySnapshot(ctx, connection.Summary.ID, request)
}

func (n *Network) topologySourceQueries(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace string) (contractresource.NetworkTopologySnapshotRequest, []contractresource.NetworkTopologyCoverage) {
	request := contractresource.NetworkTopologySnapshotRequest{}
	coverage := []contractresource.NetworkTopologyCoverage{}
	for _, definition := range contractresource.NetworkTopologySources() {
		ns := namespace
		if !definition.Namespaced {
			ns = ""
		}
		group := resourceGroupForKind(definition.Kind)
		vendor := definition.RequiresCustomResourceAccess()
		if vendor {
			group = "extensions"
		}
		sourceCtx := topologySourcePolicyContext(ctx, ns, definition.Namespaced)
		_, decision, err := n.authorizeTopologyRead(sourceCtx, principal, connection.Summary.ID, ns, group, definition.Kind, domainaccess.ActionView)
		if err != nil {
			coverage = append(coverage, topologyAuthorizationCoverage(definition, ns, err, "soha_permission"))
			continue
		}
		namespaces := topologyQueryNamespaces(ns, definition.Namespaced, decision)
		if vendor {
			namespaces = topologyVendorNamespaces(connection, definition, namespaces)
		}
		if len(namespaces) == 0 {
			reason := "select_authorized_namespace"
			if !definition.Namespaced {
				reason = "soha_permission"
			}
			coverage = append(coverage, topologyCoverage(definition, ns, "denied", reason))
			continue
		}
		for _, targetNamespace := range namespaces {
			if err := n.authorizeTopologySourceScope(ctx, principal, connection, definition, group, ns, targetNamespace); err != nil {
				coverage = append(coverage, topologyAuthorizationCoverage(definition, targetNamespace, err, "soha_permission"))
				continue
			}
			if vendor {
				allowed, err := n.canReadTopologyVendor(ctx, principal, connection, definition, targetNamespace)
				if err != nil {
					coverage = append(coverage, topologyAuthorizationCoverage(definition, targetNamespace, err, "custom_resource_grant"))
					continue
				}
				if !allowed {
					coverage = append(coverage, topologyCoverage(definition, targetNamespace, "denied", "custom_resource_grant"))
					continue
				}
			}
			if len(request.Queries) >= contractresource.NetworkTopologyMaxQueries {
				item := topologyCoverage(definition, ns, "partial", "query_budget")
				item.Truncated = true
				coverage = append(coverage, item)
				break
			}
			request.Queries = append(request.Queries, contractresource.NetworkTopologySourceQuery{SourceID: definition.ID, Namespace: targetNamespace})
		}
	}
	return request, coverage
}

func topologySourcePolicyContext(ctx context.Context, namespace string, namespaced bool) context.Context {
	// The unscoped planning decision may project a bounded set of namespaces.
	// The planner checks the actual collection after it selects those targets.
	if namespace == "" && namespaced {
		return ctx
	}
	return context.WithValue(ctx, topologyCollectionPolicyKey{}, true)
}

func (n *Network) authorizeTopologySourceScope(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, definition contractresource.NetworkTopologySourceDefinition, group, requestedNamespace, targetNamespace string) error {
	if definition.Namespaced && targetNamespace == "" {
		return n.requireTopologyWideRead(ctx, principal, connection, group, definition.Kind, domainaccess.ActionView)
	}
	if targetNamespace == requestedNamespace {
		return nil
	}
	collectionCtx := context.WithValue(ctx, topologyCollectionPolicyKey{}, true)
	_, decision, err := n.authorizeTopologyRead(collectionCtx, principal, connection.Summary.ID, targetNamespace, group, definition.Kind, domainaccess.ActionView)
	if err != nil {
		return err
	}
	if !graphNamedReferenceScopeAllowed(targetNamespace, decision) {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func topologyQueryNamespaces(namespace string, namespaced bool, decision domainaccess.Decision) []string {
	if !namespaced {
		if !clusterWideResourceScope(decision) {
			return nil
		}
		return []string{""}
	}
	if namespace != "" {
		if !graphNamedReferenceScopeAllowed(namespace, decision) {
			return nil
		}
		return []string{namespace}
	}
	scope := decision.ResourceScope
	if scope != nil && scope.LabelSelector != "" {
		return nil
	}
	if scope == nil {
		return []string{namespace}
	}
	if len(scope.NamespaceSelectors) > 0 || len(scope.ExcludedNamespaceSelectors) > 0 {
		return nil
	}
	if len(scope.Namespaces) == 0 {
		if len(scope.ExcludedNamespaces) > 0 {
			return nil
		}
		return []string{""}
	}
	result := filterScopedNamespaceItems(slices.Clone(scope.Namespaces), decision, func(value string) string { return value })
	slices.Sort(result)
	return slices.Compact(result)
}

func (n *Network) canReadTopologyVendor(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, source contractresource.NetworkTopologySourceDefinition, namespace string) (bool, error) {
	if n.customResources == nil {
		return false, nil
	}
	if _, _, err := n.customResources.authorizeTopologyRead(ctx, principal, connection.Summary.ID, "", "extensions", "CustomResourceDefinition", domainaccess.ActionView); err != nil {
		return false, err
	}
	if namespace == "" && source.Namespaced {
		if err := n.customResources.requireTopologyWideRead(ctx, principal, connection, "extensions", source.Kind, domainaccess.ActionList); err != nil {
			return false, err
		}
	} else {
		collectionCtx := context.WithValue(ctx, topologyCollectionPolicyKey{}, true)
		_, decision, err := n.customResources.authorizeTopologyRead(collectionCtx, principal, connection.Summary.ID, namespace, "extensions", source.Kind, domainaccess.ActionList)
		if err != nil {
			return false, err
		}
		if !graphNamedReferenceScopeAllowed(namespace, decision) {
			return false, nil
		}
	}
	definition := crdResourceDefinition{Kind: source.Kind, Group: source.Group, Version: source.Version, Resource: source.Resource, Namespaced: source.Namespaced}
	// The snapshot's actual LIST enforces Kubernetes RBAC without reviewing unrelated mutation verbs.
	return slices.Contains(configuredCustomResourceActions(connection, definition, namespace, []string{string(domainaccess.ActionList)}), string(domainaccess.ActionList)), nil
}

func (s *resourceAccess) requireTopologyWideRead(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, group, kind string, action domainaccess.Action) error {
	request := s.resourceAccessRequest(ctx, principal, connection, "", group, kind, action)
	request.RequireClusterWideNamespace = true
	decision, err := s.authorizer.Authorize(ctx, request)
	if err != nil {
		return err
	}
	if clusterWideResourceScope(decision) {
		return nil
	}
	const reason = "topology source requires access across all namespaces"
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", kind, "", string(action), "deny", reason)
	return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, reason)
}

// Topology consumes read decisions and resource scope, not resource action menus.
// Keep the normal policy/namespace-label checks and denial audit, but do not fan
// out into update/delete/restart/scale checks for every graph source.
func (s *resourceAccess) authorizeTopologyRead(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, group, kind string, action domainaccess.Action) (domaincluster.Connection, domainaccess.Decision, error) {
	connection, decision, err := s.decide(ctx, principal, clusterID, namespace, group, kind, action)
	if err != nil {
		return domaincluster.Connection{}, domainaccess.Decision{}, err
	}
	if !decision.Allowed {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, "", string(action), "deny", decision.Reason)
		return domaincluster.Connection{}, domainaccess.Decision{}, fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return connection, decision, nil
}

func topologyAuthorizationCoverage(source contractresource.NetworkTopologySourceDefinition, namespace string, err error, deniedReason string) contractresource.NetworkTopologyCoverage {
	if errors.Is(err, apperrors.ErrAccessDenied) {
		return topologyCoverage(source, namespace, "denied", deniedReason)
	}
	reason := "source_unavailable"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		reason = "timeout"
	}
	return topologyCoverage(source, namespace, "error", reason)
}

func topologyCoverage(source contractresource.NetworkTopologySourceDefinition, namespace, state, reason string) contractresource.NetworkTopologyCoverage {
	return contractresource.NetworkTopologyCoverage{Provider: source.Provider, SourceID: source.ID, Namespace: namespace, State: state, ReasonCode: reason, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
}

func validateTopologySnapshot(snapshot contractresource.NetworkTopologySnapshot, request contractresource.NetworkTopologySnapshotRequest, clusterID string) contractresource.NetworkTopologySnapshot {
	result := contractresource.NetworkTopologySnapshot{Version: contractresource.NetworkTopologySnapshotFeature}
	total := 0
	for _, query := range request.Queries {
		definition, _ := contractresource.LookupNetworkTopologySource(query.SourceID)
		source := contractresource.NetworkTopologySourceResult{Coverage: topologyCoverage(definition, query.Namespace, "error", "invalid_snapshot"), Objects: []contractresource.NetworkTopologySourceObject{}}
		for _, candidate := range snapshot.Sources {
			if candidate.Coverage.SourceID == query.SourceID && candidate.Coverage.Namespace == query.Namespace && candidate.Coverage.Name == query.Name {
				source = candidate
				break
			}
		}
		source.Coverage.Provider = definition.Provider
		source.Coverage.SupportedKinds = []string{definition.Kind}
		if !slices.Contains([]string{"available", "partial", "denied", "error", "not_installed", "unsupported"}, source.Coverage.State) {
			source.Coverage.State = "error"
		}
		source.Coverage.ReasonCode = cleanTopologyReason(source.Coverage.ReasonCode)
		if source.Coverage.State != "available" && source.Coverage.State != "partial" {
			source.Objects = nil
		}
		objects := []contractresource.NetworkTopologySourceObject{}
		for index, item := range source.Objects {
			if index >= contractresource.NetworkTopologyMaxSourceItems || total+len(item.Content) > contractresource.NetworkTopologyMaxTotalBytes {
				source.Coverage.Truncated = true
				source.Coverage.State = "partial"
				break
			}
			sanitized, ok := sanitizeTopologyObject(item, definition, query, clusterID)
			if !ok {
				source.Coverage.State = "partial"
				continue
			}
			item = sanitized
			data := []byte(item.Content)
			total += len(data)
			objects = append(objects, item)
		}
		if source.Coverage.State != "available" && source.Coverage.State != "partial" {
			objects = nil
		}
		source.Objects = objects
		result.Sources = append(result.Sources, source)
	}
	return result
}

func cleanTopologyReason(reason string) string {
	if slices.Contains([]string{"", "kubernetes_rbac", "source_not_served", "invalid_response", "item_budget", "object_budget", "invalid_identity", "total_budget", "byte_budget", "source_unavailable", "action_not_allowed", "timeout", "invalid_snapshot", "named_get", "named_not_found"}, reason) {
		return reason
	}
	return "source_unavailable"
}

func (n *Network) legacyTopologyWithCoverage(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace string) (domainresource.NetworkTopologyView, error) {
	clusterID := connection.Summary.ID
	view := domainresource.NetworkTopologyView{ClusterID: clusterID, Namespace: namespace, Source: "backend-aggregate", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Traces: []domainresource.NetworkTopologyTraceView{}}
	allowed, withheld := n.legacyTopologyAuthorizedSources(ctx, principal, connection, namespace)
	var err error
	if len(allowed) > 0 {
		view, err = n.getLegacyNetworkTopology(ctx, principal, clusterID, namespace, allowed)
		sanitizeLegacyTopology(&view)
	}
	if withheld {
		view.Warnings = append(view.Warnings, "Some legacy inventory sources could not be read within the current access scope.")
	}
	view.Coverage = []contractresource.NetworkTopologyCoverage{{Provider: "kubernetes", SourceID: "legacy.inventory", State: "unsupported", ReasonCode: "upgrade_agent_for_rule_topology"}}
	view.Warnings = append(view.Warnings, "Legacy inventory: rule, port and provider coverage unverified; upgrade Agent for topology snapshot.")
	return view, err
}

func (n *Network) legacyTopologyAuthorizedSources(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace string) (map[string]bool, bool) {
	allowed := map[string]bool{}
	withheld := false
	for _, kind := range []string{"Service", "Ingress", "HTTPRoute", "Gateway", "Pod"} {
		if !n.canViewRelatedKind(ctx, principal, connection.Summary.ID, namespace, kind) {
			continue
		}
		// Legacy collection can return references outside the selected namespace
		// and cannot enforce name-based restrictions before aggregating them.
		group := resourceGroupForKind(kind)
		if n.requireTopologyWideRead(ctx, principal, connection, group, kind, domainaccess.ActionView) != nil ||
			n.requireTopologyWideRead(ctx, principal, connection, group, kind, domainaccess.ActionList) != nil {
			withheld = true
			continue
		}
		allowed[kind] = true
	}
	return allowed, withheld
}

func sanitizeTopologyObject(item contractresource.NetworkTopologySourceObject, definition contractresource.NetworkTopologySourceDefinition, query contractresource.NetworkTopologySourceQuery, clusterID string) (contractresource.NetworkTopologySourceObject, bool) {
	ref := item.Resource
	knownVersion := false
	for _, version := range contractresource.NetworkTopologySourceVersions(definition) {
		if ref.APIVersion == strings.TrimPrefix(definition.Group+"/"+version, "/") {
			knownVersion = true
			break
		}
	}
	if ref.ClusterID != clusterID || ref.Kind != definition.Kind || !knownVersion || (query.Namespace != "" && ref.Namespace != query.Namespace) || (!definition.Namespaced && ref.Namespace != "") || len(item.Content) > contractresource.NetworkTopologyMaxObjectBytes {
		return item, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(item.Content), &raw) != nil {
		return item, false
	}
	projected, err := contractresource.ProjectNetworkTopologyObject(query.SourceID, raw)
	if err != nil {
		return item, false
	}
	metadata, _ := projected["metadata"].(map[string]any)
	if ref.UID == "" || metadata["uid"] != ref.UID || metadata["name"] != ref.Name || (definition.Namespaced && metadata["namespace"] != ref.Namespace) {
		return item, false
	}
	if ref.ResourceVersion != "" && metadata["resourceVersion"] != ref.ResourceVersion {
		return item, false
	}
	data, _ := json.Marshal(projected)
	item.Content = string(data)
	return item, true
}

func topologyVendorNamespaces(connection domaincluster.Connection, source contractresource.NetworkTopologySourceDefinition, namespaces []string) []string {
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent || len(namespaces) != 1 || namespaces[0] != "" {
		return namespaces
	}
	var rules []domaincluster.AgentCustomResourceRule
	data, _ := json.Marshal(connection.Metadata["agent_custom_resource_rules"])
	if json.Unmarshal(data, &rules) != nil {
		return nil
	}
	result := []string{}
	for _, rule := range rules {
		if rule.APIGroup != source.Group || !slices.Contains(rule.Resources, source.Resource) || !slices.Contains(rule.Verbs, "list") {
			continue
		}
		if len(rule.Namespaces) == 0 {
			return namespaces
		}
		result = append(result, rule.Namespaces...)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func sanitizeLegacyTopology(view *domainresource.NetworkTopologyView) {
	opaque := func(value string) string { return fmt.Sprintf("legacy:%x", sha256.Sum256([]byte(value))) }
	sanitize := func(node *domainresource.NetworkTopologyNodeView) {
		node.ID = opaque(node.ID)
		if node.State != "live" {
			node.Name = "Unresolved reference"
			node.Namespace = ""
			node.ResourceName = ""
			node.Subtitle = "Legacy coverage unverified"
		}
		node.State = "pending"
	}
	for index := range view.Traces {
		trace := &view.Traces[index]
		trace.ID = opaque(trace.ID)
		sanitize(&trace.Entry)
		sanitize(&trace.Route)
		if trace.Service != nil {
			sanitize(trace.Service)
		}
		for i := range trace.BackendPods {
			sanitize(&trace.BackendPods[i])
			trace.BackendPods[i].Badge = "Selector match only"
		}
		trace.State = "pending"
		trace.Note = "Legacy inventory; endpoint and rule resolution unverified"
	}
}
