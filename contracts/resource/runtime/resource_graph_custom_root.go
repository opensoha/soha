package runtime

import (
	"context"
	"sort"
	"strings"

	cr "github.com/opensoha/soha/contracts/resource"
)

// BuildCustomResourceGraph builds the metadata relationships for an observed
// CRD or custom instance. Callers authorize every observation and establish the
// definition's served GVK, plural and scope. An input may omit TargetID, which
// is derived from its metadata. An observed built-in may omit Definition.
func BuildCustomResourceGraph(ctx context.Context, root cr.ResourceRef, definition cr.ResourceRef, inputs []ResourceGraphMetadataEnrichment) (cr.ResourceGraph, error) {
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	if len(inputs) > ResourceGraphMetadataEnrichmentLimit {
		return cr.ResourceGraph{}, ErrResourceGraphBudget
	}
	if root.ClusterID == "" || root.APIVersion == "" || root.Kind == "" || root.Name == "" || root.UID == "" || root.ScopeMode != topologyScope(root.Namespace != "") {
		return cr.ResourceGraph{}, invalidGraphRoot("custom graph requires an exact observed root")
	}
	plural, group, _ := strings.Cut(definition.Name, ".")
	if definition.ClusterID != root.ClusterID || !customGraphIsDefinition(definition) || definition.UID == "" || definition.Namespace != "" || definition.ScopeMode != cr.ResourceScopeModeCluster || plural == "" || group == "" {
		return cr.ResourceGraph{}, invalidGraphRoot("custom graph requires an exact observed definition")
	}
	rootID := graphNodeID(root)
	definitionRoot := customGraphIsDefinition(root)
	if definitionRoot && rootID != graphNodeID(definition) {
		return cr.ResourceGraph{}, invalidGraphRoot("definition root identity mismatch")
	}
	definitions := map[string]cr.ResourceRef{definition.Name: definition}
	observations := make(map[string]ResourceGraphEnrichment, len(inputs))
	nodes := make(map[string]cr.ResourceGraphNode, len(inputs)+1)
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return cr.ResourceGraph{}, err
		}
		if len(input.Content) > ResourceGraphMetadataJSONLimit {
			return cr.ResourceGraph{}, ErrResourceGraphBudget
		}
		o, err := decodeEnrichmentMetadata(input.Content)
		if err != nil {
			return cr.ResourceGraph{}, err
		}
		ref := enrichmentRef(root.ClusterID, o)
		if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" || ref.UID == "" || customGraphIsDefinition(ref) {
			return cr.ResourceGraph{}, invalidGraphEnrichment("observation requires exact instance identity")
		}
		id := graphNodeID(ref)
		if input.TargetID != "" && input.TargetID != id {
			return cr.ResourceGraph{}, invalidGraphEnrichment("observation target identity mismatch")
		}
		if graphBuiltinSpec(o) {
			if input.Definition != (cr.ResourceRef{}) {
				return cr.ResourceGraph{}, invalidGraphEnrichment("built-in observation has a custom definition")
			}
		} else {
			if err := validateEnrichmentDefinition(root.ClusterID, ref.APIVersion, input.Definition); err != nil {
				return cr.ResourceGraph{}, err
			}
			apiGroup, version, _ := strings.Cut(ref.APIVersion, "/")
			_, definitionGroup, _ := strings.Cut(input.Definition.Name, ".")
			if version == "" || strings.Contains(version, "/") || definitionGroup != apiGroup {
				return cr.ResourceGraph{}, invalidGraphEnrichment("definition requires exact group and instance version")
			}
			if prior, exists := definitions[input.Definition.Name]; exists {
				if prior.UID != input.Definition.UID {
					return cr.ResourceGraph{}, invalidGraphEnrichment("conflicting definition UID")
				}
				input.Definition = prior
			} else {
				definitions[input.Definition.Name] = input.Definition
			}
		}
		key := graphIdentity(ref.APIVersion, ref.Kind, ref.Namespace, ref.Name)
		observation := ResourceGraphEnrichment{TargetID: id, Object: o, Definition: input.Definition}
		if prior, exists := observations[key]; exists {
			if prior.TargetID != id || !sameEnrichment(prior, observation) {
				return cr.ResourceGraph{}, invalidGraphEnrichment("conflicting duplicate observation")
			}
			continue
		}
		observations[key] = observation
		nodes[id] = cr.ResourceGraphNode{ID: id, Resource: ref}
	}
	rootObservation := observations[graphIdentity(root.APIVersion, root.Kind, root.Namespace, root.Name)]
	if !definitionRoot && (rootObservation.TargetID != rootID || rootObservation.Definition != definition) {
		return cr.ResourceGraph{}, invalidGraphRoot("custom root metadata identity mismatch")
	}
	for _, ref := range definitions {
		id := graphNodeID(ref)
		nodes[id] = cr.ResourceGraphNode{ID: id, Resource: ref}
	}
	nodes[rootID] = cr.ResourceGraphNode{ID: rootID, Resource: root}
	ordered := make([]ResourceGraphEnrichment, 0, len(observations))
	for _, input := range observations {
		ordered = append(ordered, input)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].TargetID < ordered[j].TargetID })
	b := enrichmentBuilder(ctx, root.ClusterID, nodes, ordered)
	b.nodes = nodes
	for _, input := range ordered {
		if input.Definition.UID != "" {
			b.edge(graphNodeID(input.Definition), input.TargetID, "defines")
		}
	}
	if rootObservation.Object != nil {
		b.ownerRelations(rootObservation.Object, rootID)
	}
	for _, input := range ordered {
		if input.TargetID != rootID {
			b.ownerRelations(input.Object, input.TargetID)
		}
	}
	if b.err != nil {
		return cr.ResourceGraph{}, b.err
	}
	graph, err := b.bounded(rootID, root.Namespace)
	if b.partial {
		graph.Warnings = append(graph.Warnings, "resource graph working set truncated at 10000 nodes or 10000 edges")
	}
	return graph, err
}

func customGraphIsDefinition(ref cr.ResourceRef) bool {
	return ref.APIVersion == "apiextensions.k8s.io/v1" && ref.Kind == "CustomResourceDefinition"
}
