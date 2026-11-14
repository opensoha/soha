package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	contractresource "github.com/opensoha/soha/contracts/resource"
	shared "github.com/opensoha/soha/contracts/resource/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const graphCustomOwnerLimit = shared.ResourceGraphMetadataEnrichmentLimit
const graphCRDDefinitionLimit = 500
const graphCustomPartial = "Custom relationships are partial because discovery, access, observation or budget was unavailable."

// Builtin authorization is a GVK decision. A custom example.io/v1 Pod must
// never inherit the built-in Pod permission merely because its Kind matches.
func graphBuiltinResource(ref domainresource.ResourceRef) bool {
	if graphRBACResource(ref) {
		return true
	}
	apis := map[string]string{
		"Pod": "v1", "ReplicationController": "v1", "Service": "v1", "Secret": "v1", "ConfigMap": "v1", "PersistentVolumeClaim": "v1", "ServiceAccount": "v1", "Node": "v1", "Namespace": "v1", "Event": "v1", "ResourceQuota": "v1", "LimitRange": "v1",
		"Deployment": "apps/v1", "StatefulSet": "apps/v1", "DaemonSet": "apps/v1", "ReplicaSet": "apps/v1", "Job": "batch/v1", "CronJob": "batch/v1",
		"Ingress": "networking.k8s.io/v1", "HorizontalPodAutoscaler": "autoscaling/v2", "PodDisruptionBudget": "policy/v1",
		"PriorityClass": "scheduling.k8s.io/v1", "RuntimeClass": "node.k8s.io/v1", "Lease": "coordination.k8s.io/v1",
		"MutatingWebhookConfiguration": "admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration": "admissionregistration.k8s.io/v1",
		"PersistentVolume": "v1", "StorageClass": "storage.k8s.io/v1",
	}
	if definition, ok := graphKnownTopologyResource(ref); ok {
		return graphResourceScopeValid(ref, !definition.Namespaced)
	}
	api, ok := apis[ref.Kind]
	if !ok || api != ref.APIVersion {
		return false
	}
	clusterScoped := ref.Kind == "Node" || ref.Kind == "Namespace" || ref.Kind == "PriorityClass" || ref.Kind == "RuntimeClass" || ref.Kind == "MutatingWebhookConfiguration" || ref.Kind == "ValidatingWebhookConfiguration" || ref.Kind == "PersistentVolume" || ref.Kind == "StorageClass"
	return graphResourceScopeValid(ref, clusterScoped)
}

func graphKnownTopologyResource(ref domainresource.ResourceRef) (contractresource.NetworkTopologySourceDefinition, bool) {
	for _, definition := range contractresource.NetworkTopologySources() {
		if definition.Kind != ref.Kind || definition.RequiresCustomResourceAccess() {
			continue
		}
		for _, version := range contractresource.NetworkTopologySourceVersions(definition) {
			if ref.APIVersion == strings.TrimPrefix(definition.Group+"/"+version, "/") {
				return definition, true
			}
		}
	}
	return contractresource.NetworkTopologySourceDefinition{}, false
}

func graphResourceScopeValid(ref domainresource.ResourceRef, clusterScoped bool) bool {
	if clusterScoped {
		return ref.Namespace == "" && (ref.ScopeMode == "" || ref.ScopeMode == domainresource.ResourceScopeModeCluster)
	}
	return ref.Namespace != "" && (ref.ScopeMode == "" || ref.ScopeMode == domainresource.ResourceScopeModeNamespace)
}

func graphAuthorizedIdentity(ref domainresource.ResourceRef) string {
	return strings.Join([]string{ref.ClusterID, ref.APIVersion, ref.Kind, ref.Namespace, ref.Name, ref.UID}, "\x00")
}

type graphCustomCandidate struct {
	domainresource.ResourceGraphNode
	scaleTarget bool
}

func graphCustomCandidates(graph domainresource.ResourceGraph, seen map[string]bool) []graphCustomCandidate {
	nodes := make(map[string]domainresource.ResourceGraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	declared := map[string]bool{}
	scaleTargets := map[string]bool{}
	for _, edge := range graph.Edges {
		if edge.Relation == "controls" || edge.Relation == "owns" {
			declared[edge.SourceID] = true
		}
		if edge.Relation == "scales" {
			source, target := nodes[edge.SourceID].Resource, nodes[edge.TargetID].Resource
			if source.Kind == "HorizontalPodAutoscaler" && graphBuiltinResource(source) && target.Namespace == source.Namespace && (target.ScopeMode == "" || target.ScopeMode == domainresource.ResourceScopeModeNamespace) {
				declared[edge.TargetID], scaleTargets[edge.TargetID] = true, true
			}
		}
	}
	result := []graphCustomCandidate{}
	for _, node := range graph.Nodes {
		if declared[node.ID] && !seen[node.ID] && node.Status == "unresolved" && !graphBuiltinResource(node.Resource) {
			result = append(result, graphCustomCandidate{ResourceGraphNode: node, scaleTarget: scaleTargets[node.ID]})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

type graphCustomEnricher struct {
	service     *CustomResources
	principal   domainidentity.Principal
	connection  domaincluster.Connection
	definitions []domainresource.CRDView
	allowed     map[string]bool
	seen        map[string]bool
	observedUID map[string]string
	attempts    int
	partial     bool
}

func (g *GenericResources) enrichCustomResourceGraph(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, graph domainresource.ResourceGraph) (domainresource.ResourceGraph, map[string]bool, error) {
	state := &graphCustomEnricher{service: g.customResources, principal: principal, connection: connection, allowed: map[string]bool{}, seen: map[string]bool{}, observedUID: map[string]string{}}
	if err := ctx.Err(); err != nil {
		return graph, state.allowed, err
	}
	if len(graphCustomCandidates(graph, state.seen)) == 0 {
		return graph, state.allowed, nil
	}
	if !state.discover(ctx) {
		return graphWithCustomPartial(graph), state.allowed, ctx.Err()
	}
	for depth := 0; depth < 3; depth++ {
		candidates := graphCustomCandidates(graph, state.seen)
		if len(candidates) == 0 {
			break
		}
		inputs, err := state.observe(ctx, candidates)
		if err != nil {
			return graph, state.allowed, err
		}
		if len(inputs) == 0 {
			break
		}
		var droppedOwnership bool
		graph, droppedOwnership = graphBindObservedScaleUIDs(graph, state.observedUID)
		state.partial = state.partial || droppedOwnership
		enriched, err := shared.EnrichResourceGraphFromMetadataJSON(ctx, graph, inputs)
		if err != nil {
			if ctx.Err() != nil {
				return graph, state.allowed, ctx.Err()
			}
			state.partial = true
			break
		}
		graph = enriched
	}
	if len(graphCustomCandidates(graph, state.seen)) > 0 {
		state.partial = true
	}
	if state.partial {
		graph = graphWithCustomPartial(graph)
	}
	return graph, state.allowed, ctx.Err()
}

func graphBindObservedScaleUIDs(graph domainresource.ResourceGraph, observed map[string]string) (domainresource.ResourceGraph, bool) {
	bound := map[string]bool{}
	graph.Nodes = slices.Clone(graph.Nodes)
	for i := range graph.Nodes {
		if uid := observed[graph.Nodes[i].ID]; uid != "" && graph.Nodes[i].Resource.UID == "" {
			graph.Nodes[i].Resource.UID = uid
			bound[graph.Nodes[i].ID] = true
		}
	}
	edges := make([]domainresource.ResourceGraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		// A named scale target observation cannot supply the missing UID of a
		// separate owner declaration that happened to share this node identity.
		if bound[edge.SourceID] && (edge.Relation == "controls" || edge.Relation == "owns") {
			continue
		}
		edges = append(edges, edge)
	}
	dropped := len(edges) != len(graph.Edges)
	graph.Edges = edges
	return graph, dropped
}

func graphWithCustomPartial(graph domainresource.ResourceGraph) domainresource.ResourceGraph {
	if !slices.Contains(graph.Warnings, graphCustomPartial) {
		graph.Warnings = append(graph.Warnings, graphCustomPartial)
	}
	return graph
}

func (s *graphCustomEnricher) discover(ctx context.Context) bool {
	if s.service == nil {
		return false
	}
	cluster := s.connection.Summary.ID
	if _, err := s.service.authorizeCRDDefinitionAccess(ctx, s.principal, cluster, domainaccess.ActionView); err != nil {
		return false
	}
	if _, err := s.service.authorizeCRDDefinitionAccess(ctx, s.principal, cluster, domainaccess.ActionList); err != nil {
		return false
	}
	definitions, err := s.service.listResourceGraphCRDs(ctx, s.connection)
	// Existing transports retain their context and response-size bounds. Graph
	// rejects oversized definition catalogs instead of iterating them or instances.
	if err != nil || len(definitions) > graphCRDDefinitionLimit {
		return false
	}
	s.definitions = definitions
	return true
}

func (s *graphCustomEnricher) observe(ctx context.Context, candidates []graphCustomCandidate) ([]shared.ResourceGraphMetadataEnrichment, error) {
	result := []shared.ResourceGraphMetadataEnrichment{}
	for _, node := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.attempts >= graphCustomOwnerLimit {
			s.partial = true
			break
		}
		s.attempts++
		s.seen[node.ID] = true
		input, ref, ok := s.observeOwner(ctx, node)
		if !ok {
			s.partial = true
			continue
		}
		result = append(result, input)
		if node.scaleTarget {
			s.observedUID[node.ID] = ref.UID
		}
		s.allowed[graphAuthorizedIdentity(ref)] = true
		s.allowed[graphAuthorizedIdentity(input.Definition)] = true
	}
	return result, ctx.Err()
}

func (s *graphCustomEnricher) observeOwner(ctx context.Context, node graphCustomCandidate) (shared.ResourceGraphMetadataEnrichment, domainresource.ResourceRef, bool) {
	empty := shared.ResourceGraphMetadataEnrichment{}
	definition, view, ok := graphExactDefinition(node.Resource, s.definitions)
	if !ok || (!node.scaleTarget && node.Resource.UID == "") || (node.scaleTarget && !definition.Namespaced) {
		return empty, domainresource.ResourceRef{}, false
	}
	namespace := normalizeCustomResourceNamespace(node.Resource.Namespace, definition.Namespaced)
	if definition.Namespaced && namespace == "" {
		return empty, domainresource.ResourceRef{}, false
	}
	cluster := s.connection.Summary.ID
	if err := s.authorizeOwner(ctx, namespace, definition.Kind, node.Resource.Name); err != nil {
		return empty, domainresource.ResourceRef{}, false
	}
	if err := s.service.requireCustomResourceAction(ctx, s.connection, definition, namespace, node.Resource.Name, domainaccess.ActionView); err != nil {
		return empty, domainresource.ResourceRef{}, false
	}
	item, err := s.service.readCustomGraphObject(ctx, s.connection, definition, namespace, node.Resource.Name)
	if err != nil {
		return empty, domainresource.ResourceRef{}, false
	}
	content, ref, err := graphCustomMetadata(item, node.Resource, namespace)
	if err != nil {
		return empty, domainresource.ResourceRef{}, false
	}
	crd := domainresource.ResourceRef{ClusterID: cluster, APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition", Name: view.Name, UID: view.UID, ScopeMode: domainresource.ResourceScopeModeCluster}
	_ = s.service.recordAudit(ctx, s.principal, cluster, namespace, definition.Kind, node.Resource.Name, string(domainaccess.ActionView), "success", "viewed custom resource metadata for resource graph")
	return shared.ResourceGraphMetadataEnrichment{TargetID: node.ID, Content: content, Definition: crd}, ref, true
}

// Discovery supplies the plural and scope. Version is the exact owner version,
// and must occur in the observed served versions; no preferred-version fallback.
func graphExactDefinition(ref domainresource.ResourceRef, views []domainresource.CRDView) (crdResourceDefinition, domainresource.CRDView, bool) {
	group, version, ok := strings.Cut(ref.APIVersion, "/")
	if !ok || group == "" || version == "" {
		return crdResourceDefinition{}, domainresource.CRDView{}, false
	}
	var matched crdResourceDefinition
	var matchedView domainresource.CRDView
	found := false
	for _, view := range views {
		if view.Group != group || view.Kind != ref.Kind || view.UID == "" || view.Plural == "" || view.Name != view.Plural+"."+group || !slices.Contains(view.Versions, version) {
			continue
		}
		namespaced, err := namespacedFromCRDScope(view.Scope, view.Name)
		if err != nil {
			continue
		}
		if found {
			return crdResourceDefinition{}, domainresource.CRDView{}, false
		}
		matched = crdResourceDefinition{CRDName: view.Name, Group: group, Version: version, Kind: ref.Kind, Resource: view.Plural, Namespaced: namespaced}
		matchedView, found = view, true
	}
	return matched, matchedView, found
}

// Decode only GVK and metadata before passing bytes to the shared rules. Unknown
// custom spec/status, annotations, credentials and YAML never enter graph state.
type graphOwnerMetadata struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace,omitempty"`
		UID             string `json:"uid"`
		ResourceVersion string `json:"resourceVersion,omitempty"`
		OwnerReferences []struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			UID        string `json:"uid"`
			Controller *bool  `json:"controller,omitempty"`
		} `json:"ownerReferences,omitempty"`
	} `json:"metadata"`
}

func graphCustomMetadata(content []byte, expected domainresource.ResourceRef, namespace string) ([]byte, domainresource.ResourceRef, error) {
	if len(content) > shared.ResourceGraphMetadataJSONLimit {
		return nil, domainresource.ResourceRef{}, fmt.Errorf("custom metadata input exceeds budget")
	}
	var object graphOwnerMetadata
	if err := json.Unmarshal(content, &object); err != nil {
		return nil, domainresource.ResourceRef{}, fmt.Errorf("invalid custom metadata")
	}
	meta := object.Metadata
	if object.APIVersion != expected.APIVersion || object.Kind != expected.Kind || meta.Name != expected.Name || meta.UID == "" || (expected.UID != "" && meta.UID != expected.UID) || meta.Namespace != namespace {
		return nil, domainresource.ResourceRef{}, fmt.Errorf("custom metadata identity changed")
	}
	ref := expected
	ref.UID = meta.UID
	ref.Namespace = namespace
	ref.ResourceVersion = meta.ResourceVersion
	ref.ScopeMode = domainresource.ResourceScopeModeNamespace
	if namespace == "" {
		ref.ScopeMode = domainresource.ResourceScopeModeCluster
	}
	encoded, err := json.Marshal(object)
	return encoded, ref, err
}

// Reuse normal policy and namespace-label evaluation, while pinning the existing
// custom-resource permission independently of potentially colliding Kind names.
func (s *graphCustomEnricher) authorizeOwner(ctx context.Context, namespace, kind, name string) error {
	permission := appaccess.PermPlatformExtensionsView
	connection, decision, err := s.service.decideWithPermission(graphResourceName(ctx, name), s.principal, s.connection.Summary.ID, namespace, "extensions", kind, domainaccess.ActionView, permission)
	if err != nil {
		return err
	}
	if !graphNamedReferenceScopeAllowed(namespace, decision) {
		_ = s.service.recordAudit(ctx, s.principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "deny", "custom owner metadata access denied")
		return apperrors.ErrAccessDenied
	}
	return nil
}

// ResourceGraphCRDDiscovery is an optional metadata-only, single-page catalog
// capability. It deliberately does not widen the ordinary custom-resource port.
type ResourceGraphCRDDiscovery interface {
	ListResourceGraphCRDs(context.Context, string) ([]domainresource.CRDView, error)
}

func (c *CustomResources) listResourceGraphCRDs(ctx context.Context, connection domaincluster.Connection) ([]domainresource.CRDView, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		items, _, err := c.listCRDs(ctx, connection)
		return items, err
	}
	discovery, ok := c.direct.(ResourceGraphCRDDiscovery)
	if !ok {
		return nil, apperrors.ErrUnsupportedOperation
	}
	return discovery.ListResourceGraphCRDs(ctx, connection.Summary.ID)
}
