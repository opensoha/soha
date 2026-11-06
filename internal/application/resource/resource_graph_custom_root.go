package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	cr "github.com/opensoha/soha-contracts/resource"
	shared "github.com/opensoha/soha-contracts/resource/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const graphCRDAPI = "apiextensions.k8s.io/v1"
const graphCRDKind = "CustomResourceDefinition"
const graphCustomSpecUnsupported = "This custom kind has no registered spec relationship rules. Reverse owner lookup only covers the observed page of this kind."
const graphCustomNetworkPartial = "Network relationships were unavailable for this served version or access scope. Reverse owner lookup only covers the observed page of this kind."

type directCustomGraphPage interface {
	ReadCustomResourceGraphPage(context.Context, string, cr.CRDResourceDefinition, string) (cr.CustomResourceGraphPage, error)
}

type agentCustomGraphPage interface {
	ReadCustomResourceGraphPage(context.Context, cr.CRDResourceDefinition, string) (cr.CustomResourceGraphPage, error)
}

type directCustomGraphObject interface {
	ReadCustomResourceGraphObject(context.Context, string, cr.CustomResourceGraphObjectRequest) (json.RawMessage, error)
}

type agentCustomGraphObject interface {
	ReadCustomResourceGraphObject(context.Context, cr.CustomResourceGraphObjectRequest) (json.RawMessage, error)
}

func (g *GenericResources) GetCustomResourceGraph(ctx context.Context, principal domainidentity.Principal, root cr.ResourceRef, plural string) (cr.ResourceGraph, error) {
	ctx = context.WithValue(ctx, graphNamespacePolicyKey{}, true)
	ctx, cancel := context.WithTimeout(ctx, shared.ResourceGraphTimeout)
	defer cancel()
	if g.customResources == nil {
		return cr.ResourceGraph{}, apperrors.ErrUnsupportedOperation
	}
	if !validExplicitGraphRoot(root, plural) {
		return cr.ResourceGraph{}, apperrors.ErrInvalidArgument
	}
	state, err := g.customGraphDiscovery(ctx, principal, root.ClusterID)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	if root.APIVersion == graphCRDAPI {
		return g.customDefinitionGraph(ctx, state, root)
	}
	return g.customInstanceGraph(ctx, state, root, plural)
}

func validExplicitGraphRoot(root cr.ResourceRef, plural string) bool {
	group, version, ok := strings.Cut(root.APIVersion, "/")
	if !ok || group == "" || version == "" || strings.Contains(version, "/") || root.ClusterID == "" || root.Kind == "" || root.Name == "" ||
		strings.TrimSpace(root.APIVersion) != root.APIVersion || strings.TrimSpace(plural) != plural || plural == "" ||
		strings.TrimSpace(root.Kind) != root.Kind || strings.TrimSpace(root.Name) != root.Name ||
		strings.TrimSpace(root.Namespace) != root.Namespace {
		return false
	}
	return root.APIVersion != graphCRDAPI || (root.Kind == graphCRDKind && plural == "customresourcedefinitions" && root.Namespace == "")
}

func (g *GenericResources) customGraphDiscovery(ctx context.Context, principal domainidentity.Principal, cluster string) (*graphCustomEnricher, error) {
	if _, err := g.customGraphAccess(ctx, principal, cluster, "", graphCRDKind, domainaccess.ActionView); err != nil {
		return nil, err
	}
	connection, err := g.customGraphAccess(ctx, principal, cluster, "", graphCRDKind, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	views, err := g.customResources.listResourceGraphCRDs(ctx, connection)
	if err != nil {
		return nil, err
	}
	if len(views) > graphCRDDefinitionLimit {
		return nil, fmt.Errorf("%w: CRD discovery exceeds the relationship budget", apperrors.ErrUnsupportedOperation)
	}
	return &graphCustomEnricher{service: g.customResources, principal: principal, connection: connection, definitions: views}, nil
}

func (g *GenericResources) customGraphAccess(ctx context.Context, principal domainidentity.Principal, cluster, namespace, kind string, action domainaccess.Action) (domaincluster.Connection, error) {
	connection, decision, err := g.customResources.decideWithPermission(ctx, principal, cluster, namespace, "extensions", kind, action, appaccess.PermPlatformExtensionsView)
	if err != nil {
		return domaincluster.Connection{}, err
	}
	if !graphNamedReferenceScopeAllowed(namespace, decision) {
		return domaincluster.Connection{}, apperrors.ErrAccessDenied
	}
	return connection, nil
}

func (g *GenericResources) customGraphReferenceAllowed(ctx context.Context, principal domainidentity.Principal, cluster string, ref cr.ResourceRef) bool {
	if g.customResources == nil {
		return false
	}
	_, err := g.customGraphAccess(graphResourceName(ctx, ref.Name), principal, cluster, ref.Namespace, ref.Kind, domainaccess.ActionView)
	return err == nil
}

func (g *GenericResources) customDefinitionGraph(ctx context.Context, state *graphCustomEnricher, root cr.ResourceRef) (cr.ResourceGraph, error) {
	if _, err := g.customGraphAccess(graphResourceName(ctx, root.Name), state.principal, root.ClusterID, "", graphCRDKind, domainaccess.ActionView); err != nil {
		return cr.ResourceGraph{}, err
	}
	for _, view := range state.definitions {
		if view.Name != root.Name || view.UID == "" {
			continue
		}
		root = graphDefinitionRef(root.ClusterID, view)
		definition, err := crdResourceDefinitionFromView(view)
		if err != nil {
			return cr.ResourceGraph{}, err
		}
		inputs, partial := g.customGraphPage(ctx, state, definition, root, "", "")
		graph, err := shared.BuildCustomResourceGraph(ctx, root, root, inputs)
		if err != nil {
			return cr.ResourceGraph{}, err
		}
		graph = g.filterExplicitMetadataGraph(ctx, state, graph, root, definition, "", nil)
		return g.completeExplicitGraph(ctx, state, root, graph, partial, nil)
	}
	return cr.ResourceGraph{}, apperrors.ErrNotFound
}

func graphDefinitionRef(cluster string, view cr.CRDView) cr.ResourceRef {
	return cr.ResourceRef{ClusterID: cluster, APIVersion: graphCRDAPI, Kind: graphCRDKind,
		Name: view.Name, UID: view.UID, ScopeMode: cr.ResourceScopeModeCluster}
}

func (g *GenericResources) customInstanceGraph(ctx context.Context, state *graphCustomEnricher, root cr.ResourceRef, plural string) (cr.ResourceGraph, error) {
	views := slices.DeleteFunc(slices.Clone(state.definitions), func(view cr.CRDView) bool { return view.Plural != plural })
	definition, view, ok := graphExactDefinition(root, views)
	if !ok {
		return cr.ResourceGraph{}, apperrors.ErrNotFound
	}
	if definition.Namespaced != (root.Namespace != "") {
		return cr.ResourceGraph{}, apperrors.ErrInvalidArgument
	}
	if _, err := g.customGraphAccess(graphResourceName(ctx, root.Name), state.principal, root.ClusterID, root.Namespace, root.Kind, domainaccess.ActionView); err != nil {
		return cr.ResourceGraph{}, err
	}
	if err := state.service.requireCustomResourceAction(ctx, state.connection, definition, root.Namespace, root.Name, domainaccess.ActionView); err != nil {
		return cr.ResourceGraph{}, err
	}
	item, err := state.service.readCustomGraphObject(ctx, state.connection, definition, root.Namespace, root.Name)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	content, observed, err := graphCustomMetadata(item, root, root.Namespace)
	if err != nil {
		return cr.ResourceGraph{}, fmt.Errorf("%w: custom resource observation unavailable", apperrors.ErrClusterUnready)
	}
	crd := graphDefinitionRef(root.ClusterID, view)
	input := shared.ResourceGraphMetadataEnrichment{Content: content, Definition: crd}
	if network, ok := g.customNetworkGraph(ctx, state, observed, definition, input); ok {
		return g.completeExplicitGraph(ctx, state, observed, network, false, nil)
	}
	return g.genericCustomInstanceGraph(ctx, state, observed, definition, input)
}

func (g *GenericResources) genericCustomInstanceGraph(ctx context.Context, state *graphCustomEnricher, root cr.ResourceRef, definition crdResourceDefinition, input shared.ResourceGraphMetadataEnrichment) (cr.ResourceGraph, error) {
	inputs, partial := g.customGraphPage(ctx, state, definition, input.Definition, root.Namespace, root.Name)
	graph, err := shared.BuildCustomResourceGraph(ctx, root, input.Definition, append(inputs, input))
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	networkDef, known := graphNetworkRootDefinition(cr.ResourceRef{Kind: root.Kind})
	if known && networkDef.Provider == "gateway" && networkDef.Group == definition.Group && networkDef.Resource == definition.Resource {
		graph.Warnings = append(graph.Warnings, graphCustomNetworkPartial)
	} else {
		graph.Warnings = append(graph.Warnings, graphCustomSpecUnsupported)
	}
	graph, allowed, err := g.enrichCustomResourceGraph(ctx, state.principal, state.connection, graph)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	graph = g.filterExplicitMetadataGraph(ctx, state, graph, input.Definition, definition, root.Namespace, allowed)
	return g.completeExplicitGraph(ctx, state, root, graph, partial, nil)
}

func (g *GenericResources) filterExplicitMetadataGraph(ctx context.Context, state *graphCustomEnricher, graph cr.ResourceGraph, crd cr.ResourceRef, definition crdResourceDefinition, namespace string, allowed map[string]bool) cr.ResourceGraph {
	if allowed == nil {
		allowed = map[string]bool{}
	}
	allowed[graphAuthorizedIdentity(crd)] = true
	apiVersion := definition.Group + "/" + definition.Version
	for _, node := range graph.Nodes {
		ref := node.Resource
		if ref.APIVersion == apiVersion && ref.Kind == definition.Kind && (namespace == "" || ref.Namespace == namespace) && ref.UID != "" && node.Status != "unresolved" {
			allowed[graphAuthorizedIdentity(ref)] = true
		}
	}
	visible := func(ref cr.ResourceRef) bool {
		if allowed[graphAuthorizedIdentity(ref)] {
			return g.customGraphReferenceAllowed(ctx, state.principal, crd.ClusterID, ref)
		}
		if !graphBuiltinResource(ref) {
			return false
		}
		_, err := g.authorizeGraphView(ctx, state.principal, crd.ClusterID, ref, true)
		return err == nil
	}
	return retainRootConnectedGraph(filterGraphReferences(graph, visible, false))
}

func (g *GenericResources) customNetworkGraph(ctx context.Context, state *graphCustomEnricher, root cr.ResourceRef, definition crdResourceDefinition, input shared.ResourceGraphMetadataEnrichment) (cr.ResourceGraph, bool) {
	source, ok := graphNetworkRootDefinition(root)
	if !ok || source.Resource != definition.Resource || source.Provider != "gateway" {
		return cr.ResourceGraph{}, false
	}
	if _, err := g.authorizeGraphView(ctx, state.principal, root.ClusterID, root, true); err != nil {
		return cr.ResourceGraph{}, false
	}
	graph, allowed, err := g.getNetworkResourceGraph(ctx, state.principal, state.connection, root)
	if err != nil {
		return cr.ResourceGraph{}, false
	}
	node, ok := resourceGraphRootNode(graph)
	if !ok || node.Resource.APIVersion != root.APIVersion || node.Resource.UID != root.UID {
		return cr.ResourceGraph{}, false
	}
	input.TargetID = graph.RootID
	graph, err = shared.EnrichResourceGraphFromMetadataJSON(ctx, graph, []shared.ResourceGraphMetadataEnrichment{input})
	if err != nil {
		return cr.ResourceGraph{}, false
	}
	allowed[graphAuthorizedIdentity(input.Definition)] = true
	graph = retainRootConnectedGraph(g.filterAuthorizedGraph(ctx, state.principal, root.ClusterID, graph, allowed))
	return graph, true
}

func (g *GenericResources) customGraphPage(ctx context.Context, state *graphCustomEnricher, definition crdResourceDefinition, crd cr.ResourceRef, namespace, skipName string) ([]shared.ResourceGraphMetadataEnrichment, bool) {
	for _, action := range []domainaccess.Action{domainaccess.ActionList, domainaccess.ActionView} {
		if _, err := g.customGraphAccess(ctx, state.principal, crd.ClusterID, namespace, definition.Kind, action); err != nil {
			return nil, true
		}
		if err := state.service.requireCustomResourceAction(ctx, state.connection, definition, namespace, "", action); err != nil {
			return nil, true
		}
	}
	page, err := state.service.readCustomGraphPage(ctx, state.connection, definition, namespace)
	if err != nil {
		return nil, true
	}
	return explicitGraphPageInputs(page, definition, crd, namespace, skipName)
}

func explicitGraphPageInputs(page cr.CustomResourceGraphPage, definition crdResourceDefinition, crd cr.ResourceRef, namespace, skipName string) ([]shared.ResourceGraphMetadataEnrichment, bool) {
	limit := shared.ResourceGraphMetadataEnrichmentLimit
	if skipName != "" {
		limit--
	}
	inputs := make([]shared.ResourceGraphMetadataEnrichment, 0, min(limit, len(page.Items)))
	for _, content := range page.Items[:min(len(page.Items), shared.ResourceGraphMetadataEnrichmentLimit)] {
		var object graphOwnerMetadata
		if len(content) > shared.ResourceGraphMetadataJSONLimit || json.Unmarshal(content, &object) != nil {
			return nil, true
		}
		ref := cr.ResourceRef{ClusterID: crd.ClusterID, APIVersion: definition.Group + "/" + definition.Version, Kind: definition.Kind, Name: object.Metadata.Name}
		if ref.Name == "" || definition.Namespaced != (object.Metadata.Namespace != "") || (namespace != "" && namespace != object.Metadata.Namespace) {
			return nil, true
		}
		if ref.Name == skipName && object.Metadata.Namespace == namespace {
			continue
		}
		validated, _, err := graphCustomMetadata(content, ref, object.Metadata.Namespace)
		if err != nil {
			return nil, true
		}
		if len(inputs) >= limit {
			return inputs, true
		}
		inputs = append(inputs, shared.ResourceGraphMetadataEnrichment{Content: validated, Definition: crd})
	}
	return inputs, page.Truncated || len(page.Items) > shared.ResourceGraphMetadataEnrichmentLimit
}

func (c *CustomResources) readCustomGraphObject(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace, name string) (json.RawMessage, error) {
	request := cr.CustomResourceGraphObjectRequest{Definition: definition.AgentDefinition(), Namespace: namespace, Name: name}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return nil, err
		}
		if reader, ok := client.(agentCustomGraphObject); ok {
			item, err := reader.ReadCustomResourceGraphObject(ctx, request)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return item, wrapAgentResourceError(err)
		}
	} else if reader, ok := c.direct.(directCustomGraphObject); ok {
		return reader.ReadCustomResourceGraphObject(ctx, connection.Summary.ID, request)
	}
	return nil, apperrors.ErrUnsupportedOperation
}

func (c *CustomResources) readCustomGraphPage(ctx context.Context, connection domaincluster.Connection, definition crdResourceDefinition, namespace string) (cr.CustomResourceGraphPage, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := c.customResourceAgentClient(connection)
		if err != nil {
			return cr.CustomResourceGraphPage{}, err
		}
		if reader, ok := client.(agentCustomGraphPage); ok {
			return reader.ReadCustomResourceGraphPage(ctx, definition.AgentDefinition(), namespace)
		}
	} else if reader, ok := c.direct.(directCustomGraphPage); ok {
		return reader.ReadCustomResourceGraphPage(ctx, connection.Summary.ID, definition.AgentDefinition(), namespace)
	}
	return cr.CustomResourceGraphPage{}, apperrors.ErrUnsupportedOperation
}

func (g *GenericResources) completeExplicitGraph(ctx context.Context, state *graphCustomEnricher, root cr.ResourceRef, graph cr.ResourceGraph, partial bool, err error) (cr.ResourceGraph, error) {
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	if _, ok := resourceGraphRootNode(graph); !ok {
		return cr.ResourceGraph{}, apperrors.ErrAccessDenied
	}
	if partial && !slices.Contains(graph.Warnings, graphCustomPartial) {
		graph.Warnings = append(graph.Warnings, graphCustomPartial)
	}
	_ = g.recordAudit(ctx, state.principal, root.ClusterID, root.Namespace, root.Kind, root.Name, string(domainaccess.ActionView), "success", "viewed explicit CRD relationship graph")
	return graph, nil
}
