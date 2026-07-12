package workflow

import (
	"fmt"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateDAGExecutionDefinition(definition dagWorkflowDefinition, binding domaincatalog.ApplicationEnvironment, app domainapp.App, input domainworkflow.Input) error {
	nodeIDs, producersByRef, err := indexDAGNodes(definition.Nodes)
	if err != nil {
		return err
	}
	if err := validateDAGEdges(definition.Edges, nodeIDs); err != nil {
		return err
	}
	return validateDAGNodeContracts(
		definition,
		binding,
		app,
		input,
		initialDAGInputReferences(app, input, binding),
		producersByRef,
	)
}

func indexDAGNodes(nodes []dagWorkflowNode) (map[string]struct{}, map[string][]string, error) {
	nodeIDs := make(map[string]struct{}, len(nodes))
	producersByRef := map[string][]string{}
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			return nil, nil, fmt.Errorf("%w: workflow node id is required", apperrors.ErrInvalidArgument)
		}
		if _, exists := nodeIDs[node.ID]; exists {
			return nil, nil, fmt.Errorf("%w: duplicate workflow node id %s", apperrors.ErrInvalidArgument, node.ID)
		}
		nodeIDs[node.ID] = struct{}{}
		indexDAGNodeProducers(producersByRef, node)
	}
	return nodeIDs, producersByRef, nil
}

func indexDAGNodeProducers(producersByRef map[string][]string, node dagWorkflowNode) {
	for _, output := range node.Outputs {
		output = strings.TrimSpace(output)
		if output == "" {
			continue
		}
		dagRegisterProducedRef(producersByRef, output, node.ID)
		dagRegisterProducedRef(producersByRef, node.ID+"."+output, node.ID)
	}
	for _, artifact := range node.ArtifactOutputs {
		name := strings.TrimSpace(fmt.Sprint(artifact["name"]))
		kind := strings.TrimSpace(fmt.Sprint(artifact["kind"]))
		if name != "" {
			dagRegisterProducedRef(producersByRef, name, node.ID)
			dagRegisterProducedRef(producersByRef, node.ID+"."+name, node.ID)
		}
		if kind != "" {
			dagRegisterProducedRef(producersByRef, kind, node.ID)
			dagRegisterProducedRef(producersByRef, node.ID+"."+kind, node.ID)
		}
	}
}

func validateDAGEdges(edges []dagWorkflowEdge, nodeIDs map[string]struct{}) error {
	for _, edge := range edges {
		if _, ok := nodeIDs[edge.Source]; !ok {
			return fmt.Errorf("%w: edge source %s not found", apperrors.ErrInvalidArgument, edge.Source)
		}
		if _, ok := nodeIDs[edge.Target]; !ok {
			return fmt.Errorf("%w: edge target %s not found", apperrors.ErrInvalidArgument, edge.Target)
		}
	}
	return nil
}

func validateDAGNodeContracts(definition dagWorkflowDefinition, binding domaincatalog.ApplicationEnvironment, app domainapp.App, input domainworkflow.Input, availableRefs map[string]struct{}, producersByRef map[string][]string) error {
	for _, node := range definition.Nodes {
		if err := validateDAGNodeInputs(definition, node, availableRefs, producersByRef); err != nil {
			return err
		}
		if err := validateDAGNodeArtifactOutputs(node); err != nil {
			return err
		}
		if strategy := normalizeDAGFanOutStrategy(node.FanOutStrategy); strings.TrimSpace(node.FanOutStrategy) != "" && strategy == "" {
			return fmt.Errorf("%w: delivery_dag node %s fanOut strategy %s is not supported", apperrors.ErrInvalidArgument, node.ID, node.FanOutStrategy)
		}
		if _, err := resolveDAGNodeSelectors(app, input, binding, node); err != nil {
			return err
		}
	}
	return nil
}

func validateDAGNodeInputs(definition dagWorkflowDefinition, node dagWorkflowNode, availableRefs map[string]struct{}, producersByRef map[string][]string) error {
	for _, inputRef := range node.Inputs {
		if dagInputReferenceDeclared(inputRef, availableRefs) {
			continue
		}
		producers := dagProducedRefProducers(inputRef, producersByRef)
		if len(producers) == 0 {
			return fmt.Errorf("%w: delivery_dag node %s input reference %s not found", apperrors.ErrInvalidArgument, node.ID, inputRef)
		}
		if !dagProducedRefHasUpstreamProducer(definition, node.ID, producers) {
			return fmt.Errorf("%w: delivery_dag node %s input reference %s must come from an upstream node", apperrors.ErrInvalidArgument, node.ID, inputRef)
		}
	}
	return nil
}

func validateDAGNodeArtifactOutputs(node dagWorkflowNode) error {
	for _, artifact := range node.ArtifactOutputs {
		name := strings.TrimSpace(fmt.Sprint(artifact["name"]))
		if name == "" {
			return fmt.Errorf("%w: delivery_dag node %s artifact output requires name", apperrors.ErrInvalidArgument, node.ID)
		}
		kind := strings.TrimSpace(fmt.Sprint(artifact["kind"]))
		if !isAllowedDeliveryArtifactKind(kind) {
			return fmt.Errorf("%w: unsupported delivery_dag artifact output kind %s", apperrors.ErrInvalidArgument, kind)
		}
	}
	return nil
}

func dagRegisterProducedRef(producersByRef map[string][]string, ref, producerNodeID string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || producerNodeID == "" {
		return
	}
	producersByRef[ref] = append(producersByRef[ref], producerNodeID)
}

func dagProducedRefProducers(inputRef string, producersByRef map[string][]string) []string {
	ref := strings.TrimSpace(inputRef)
	if ref == "" {
		return nil
	}
	if producers := producersByRef[ref]; len(producers) > 0 {
		return producers
	}
	return nil
}

func dagProducedRefHasUpstreamProducer(definition dagWorkflowDefinition, nodeID string, producers []string) bool {
	for _, producerNodeID := range producers {
		if dagNodeHasUpstreamPath(definition, nodeID, producerNodeID, map[string]bool{}) {
			return true
		}
	}
	return false
}

func dagNodeHasUpstreamPath(definition dagWorkflowDefinition, nodeID, upstreamNodeID string, seen map[string]bool) bool {
	if nodeID == upstreamNodeID {
		return false
	}
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	for _, edge := range incomingEdgesForNode(definition, nodeID) {
		if edge.Source == upstreamNodeID {
			return true
		}
		if dagNodeHasUpstreamPath(definition, edge.Source, upstreamNodeID, seen) {
			return true
		}
	}
	return false
}

func initialDAGInputReferences(app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment) map[string]struct{} {
	refs := map[string]struct{}{
		"source":                   {},
		"application":              {},
		"app":                      {},
		"applicationId":            {},
		"application.id":           {},
		"branch":                   {},
		"ref":                      {},
		"refName":                  {},
		"commit":                   {},
		"image":                    {},
		"imageTag":                 {},
		"environment":              {},
		"environmentId":            {},
		"environmentKey":           {},
		"target":                   {},
		"cluster":                  {},
		"clusterId":                {},
		"namespace":                {},
		"deployment":               {},
		"deploymentName":           {},
		"applicationEnvironment":   {},
		"applicationEnvironmentId": {},
	}
	if app.ID != "" {
		refs[app.ID] = struct{}{}
	}
	if app.Key != "" {
		refs[app.Key] = struct{}{}
	}
	if input.BuildSourceID != "" {
		refs["buildSource"] = struct{}{}
		refs["buildSourceId"] = struct{}{}
	}
	if binding.ID != "" {
		refs[binding.ID] = struct{}{}
	}
	return refs
}

func dagInputReferenceDeclared(inputRef string, declared map[string]struct{}) bool {
	ref := strings.TrimSpace(inputRef)
	if ref == "" {
		return false
	}
	_, ok := declared[ref]
	return ok
}

func isAllowedDeliveryArtifactKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "image", "test_report", "scan_report", "sbom", "screenshot", "video", "junit", "log":
		return true
	default:
		return false
	}
}
