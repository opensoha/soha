package workflow

import (
	"fmt"
	"strings"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func parseDAGWorkflowDefinition(definition map[string]any) (dagWorkflowDefinition, bool) {
	mode, _ := definition["mode"].(string)
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "release_dag"
	}
	if mode != "release_dag" && mode != "delivery_dag" {
		return dagWorkflowDefinition{}, false
	}
	nodeItems, ok := toMapSlice(definition["nodes"])
	if !ok || len(nodeItems) == 0 {
		return dagWorkflowDefinition{}, false
	}
	edgeItems, _ := toMapSlice(definition["edges"])
	nodes := make([]dagWorkflowNode, 0, len(nodeItems))
	for _, item := range nodeItems {
		fanOut := toConfigMap(item["fanOut"])
		nodes = append(nodes, dagWorkflowNode{
			ID:                  strings.TrimSpace(fmt.Sprint(item["id"])),
			Name:                strings.TrimSpace(fmt.Sprint(item["name"])),
			Type:                strings.TrimSpace(fmt.Sprint(item["type"])),
			ExecutorKind:        mapString(item, "executorKind"),
			TargetKind:          mapString(item, "targetKind"),
			CapabilityRef:       mapString(item, "capabilityRef"),
			ProviderRef:         mapString(item, "providerRef"),
			TimeoutSeconds:      toInt(item["timeoutSeconds"], 300),
			ContinueOnFailure:   toBool(item["continueOnFailure"]),
			Config:              toConfigMap(item["config"]),
			Inputs:              toStringSlice(item["inputs"]),
			Outputs:             toStringSlice(item["outputs"]),
			ServiceSelector:     toConfigMap(item["serviceSelector"]),
			EnvironmentSelector: toConfigMap(item["environmentSelector"]),
			TargetSelector:      toConfigMap(item["targetSelector"]),
			InputMapping:        toConfigMap(item["inputMapping"]),
			ArtifactOutputs:     toMapSliceOrEmpty(item["artifactOutputs"]),
			ArtifactKinds:       toStringSlice(item["artifactKinds"]),
			RunCondition:        mapString(item, "runCondition"),
			FailurePolicy:       mapString(item, "failurePolicy"),
			FanOutStrategy:      firstNonEmpty(mapString(item, "fanOutStrategy"), mapString(fanOut, "strategy")),
			FanOutBatchSize:     firstPositiveInt(toInt(item["fanOutBatchSize"], 0), toInt(fanOut["batchSize"], 0)),
			FanOutFailurePolicy: firstNonEmpty(mapString(item, "fanOutFailurePolicy"), mapString(fanOut, "failurePolicy")),
			Observability:       toConfigMap(item["observability"]),
		})
	}
	edges := make([]dagWorkflowEdge, 0, len(edgeItems))
	for _, item := range edgeItems {
		edges = append(edges, dagWorkflowEdge{
			ID:        strings.TrimSpace(fmt.Sprint(item["id"])),
			Source:    strings.TrimSpace(fmt.Sprint(item["source"])),
			Target:    strings.TrimSpace(fmt.Sprint(item["target"])),
			Condition: strings.TrimSpace(fmt.Sprint(item["condition"])),
		})
	}
	return dagWorkflowDefinition{
		SchemaVersion: toInt(definition["schemaVersion"], 2),
		Mode:          mode,
		Nodes:         nodes,
		Edges:         edges,
	}, true
}

func validationOnlyDAGDefinition(definition dagWorkflowDefinition) dagWorkflowDefinition {
	return filterDAGDefinition(definition, isValidationDAGNode)
}

func rollbackOnlyDAGDefinition(definition dagWorkflowDefinition) dagWorkflowDefinition {
	return filterDAGDefinition(definition, isRollbackDAGNode)
}

func filterDAGDefinition(definition dagWorkflowDefinition, keep func(string) bool) dagWorkflowDefinition {
	allowed := make(map[string]struct{})
	nodes := make([]dagWorkflowNode, 0)
	for _, node := range definition.Nodes {
		if !keep(node.Type) {
			continue
		}
		nodes = append(nodes, node)
		allowed[node.ID] = struct{}{}
	}
	edges := make([]dagWorkflowEdge, 0)
	for _, edge := range definition.Edges {
		if _, ok := allowed[edge.Source]; !ok {
			continue
		}
		if _, ok := allowed[edge.Target]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	return dagWorkflowDefinition{
		SchemaVersion: definition.SchemaVersion,
		Mode:          definition.Mode,
		Nodes:         nodes,
		Edges:         edges,
	}
}

func definitionFromRunMetadata(run domainworkflow.Run) (dagWorkflowDefinition, bool) {
	if len(run.Metadata) == 0 {
		return dagWorkflowDefinition{}, false
	}
	if nodes, ok := run.Metadata["nodes"].([]dagWorkflowNode); ok && len(nodes) > 0 {
		definition := dagWorkflowDefinition{
			SchemaVersion: toInt(run.Metadata["schemaVersion"], 2),
			Mode:          workflowMetadataMode(run.Metadata),
			Nodes:         nodes,
		}
		if edges, edgeOK := run.Metadata["edges"].([]dagWorkflowEdge); edgeOK {
			definition.Edges = edges
		}
		return definition, true
	}
	return parseDAGWorkflowDefinition(map[string]any{
		"mode":          workflowMetadataMode(run.Metadata),
		"schemaVersion": run.Metadata["schemaVersion"],
		"nodes":         run.Metadata["nodes"],
		"edges":         run.Metadata["edges"],
	})
}

func toMapSlice(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok {
		items2, ok2 := value.([]map[string]any)
		if !ok2 {
			return nil, false
		}
		return append([]map[string]any(nil), items2...), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		valueMap, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, valueMap)
	}
	return out, true
}

func toMapSliceOrEmpty(value any) []map[string]any {
	items, ok := toMapSlice(value)
	if !ok {
		return nil
	}
	return items
}

func toConfigMap(value any) map[string]any {
	if valueMap, ok := value.(map[string]any); ok {
		return valueMap
	}
	return map[string]any{}
}

func toStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if text := strings.TrimSpace(item); text != "" {
					out = append(out, text)
				}
			}
			return out
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func configString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func mergeDAGMaps(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func toInt(value any, fallback int) int {
	switch current := value.(type) {
	case int:
		return current
	case int32:
		return int(current)
	case int64:
		return int(current)
	case float64:
		return int(current)
	case float32:
		return int(current)
	default:
		return fallback
	}
}

func toBool(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}
