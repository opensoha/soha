package workflow

import (
	"fmt"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func restoredDAGArtifactState(metadata map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range metadataMap(metadata["artifacts"]) {
		out[key] = value
	}
	return out
}

func materializeDAGArtifactOutputs(node dagWorkflowNode, outputs map[string]any, artifactState map[string]any, status string) (map[string]any, error) {
	if len(node.ArtifactOutputs) == 0 {
		return nil, nil
	}
	artifacts := make(map[string]any, len(node.ArtifactOutputs))
	for _, artifact := range node.ArtifactOutputs {
		name := strings.TrimSpace(fmt.Sprint(artifact["name"]))
		kind := strings.TrimSpace(fmt.Sprint(artifact["kind"]))
		if name == "" || !isAllowedDeliveryArtifactKind(kind) {
			continue
		}
		value := firstArtifactValue(name, kind, outputs, artifactState)
		if value == nil && toBool(artifact["required"]) && status == "completed" {
			return nil, fmt.Errorf("required artifact output %s (%s) is missing", name, kind)
		}
		if value == nil {
			continue
		}
		artifactRecord := map[string]any{
			"name":   name,
			"kind":   kind,
			"value":  value,
			"nodeId": node.ID,
		}
		for _, key := range []string{"ref", "digest", "path", "retentionUntil", "retention"} {
			if raw, ok := artifact[key]; ok && raw != nil {
				artifactRecord[key] = raw
			}
		}
		if status != "" {
			artifactRecord["status"] = status
		}
		artifacts[name] = artifactRecord
		if kind != name {
			artifacts[kind] = artifactRecord
		}
	}
	return artifacts, nil
}

func metadataDAGExecutionResult(result dagExecutionResult) map[string]any {
	outputs := metadataDAGOutputState(result.outputs)
	artifacts := metadataDAGArtifactState(result.artifacts)
	if references := metadataDAGArtifactReferences(result.outputs); len(references) > 0 {
		for key, value := range references {
			if outputs == nil {
				outputs = map[string]any{}
			}
			outputs[key] = value
			if artifacts == nil {
				artifacts = map[string]any{}
			}
			artifacts[key] = value
		}
	}
	return map[string]any{
		"inputs":    metadataDAGInputState(result.inputs),
		"outputs":   outputs,
		"artifacts": artifacts,
		"selectors": result.selectors,
		"status":    result.status,
		"summary":   result.summary,
	}
}

func metadataDAGInputState(inputs map[string]any) map[string]any {
	if len(inputs) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(inputs))
	for key, value := range inputs {
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	return metadata
}

func metadataDAGOutputState(outputs map[string]any) map[string]any {
	if len(outputs) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(outputs))
	for key, value := range outputs {
		if key == "artifactReferences" {
			metadata[key] = value
			continue
		}
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	return metadata
}

func metadataDAGArtifactReferences(outputs map[string]any) map[string]any {
	if len(outputs) == 0 {
		return nil
	}
	references, ok := outputs["artifactReferences"].(map[string]any)
	if !ok || len(references) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(references))
	for key, value := range references {
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	return metadata
}

func metadataDAGArtifactState(artifactState map[string]any) map[string]any {
	if len(artifactState) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(artifactState))
	for key, value := range artifactState {
		if key == "artifactReferences" {
			continue
		}
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	if references, ok := artifactState["artifactReferences"].(map[string]any); ok {
		for key, value := range references {
			metadata[key] = metadataDAGArtifactValue(key, value)
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func metadataDAGArtifactValue(key string, value any) any {
	recordMap, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if references, ok := recordMap["artifactReferences"].(map[string]any); ok {
		for refKey, refValue := range references {
			if strings.TrimSpace(refKey) == strings.TrimSpace(key) {
				return metadataDAGArtifactValue(refKey, refValue)
			}
		}
	}
	lightweight := map[string]any{}
	for _, field := range []string{"id", "kind", "name", "ref", "digest", "path", "status", "workflowRunId", "workflowNodeId", "nodeId", "retentionUntil", "retention"} {
		if raw, ok := recordMap[field]; ok && raw != nil {
			lightweight[field] = raw
		}
	}
	if raw, ok := recordMap["value"]; ok {
		if lightweight["ref"] == nil {
			if ref := artifactValueString(raw, "ref"); ref != "" {
				lightweight["ref"] = ref
			} else if text := strings.TrimSpace(fmt.Sprint(raw)); text != "" && text != "<nil>" {
				lightweight["ref"] = text
			}
		}
		if lightweight["digest"] == nil {
			if digest := artifactValueString(raw, "digest"); digest != "" {
				lightweight["digest"] = digest
			}
		}
		if lightweight["path"] == nil {
			if path := artifactValueString(raw, "path"); path != "" {
				lightweight["path"] = path
			}
		}
	}
	if len(lightweight) == 0 {
		return value
	}
	if lightweight["name"] == nil && strings.TrimSpace(key) != "" {
		lightweight["name"] = strings.TrimSpace(key)
	}
	return lightweight
}

func dagArtifactStoreItem(app domainapp.App, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode, run domainworkflow.Run, artifact map[string]any, status string) domaindelivery.ExecutionArtifact {
	name := workflowMetadataString(artifact, "name")
	kind := workflowMetadataString(artifact, "kind")
	value := artifact["value"]
	ref := firstNonEmpty(workflowMetadataString(artifact, "ref"), artifactValueString(value, "ref"))
	digest := firstNonEmpty(workflowMetadataString(artifact, "digest"), artifactValueString(value, "digest"))
	path := firstNonEmpty(workflowMetadataString(artifact, "path"), artifactValueString(value, "path"))
	if ref == "" && path == "" {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			ref = text
		}
	}
	metadata := map[string]any{
		"value":              value,
		"workflowName":       run.WorkflowName,
		"nodeName":           node.Name,
		"nodeType":           node.Type,
		"environmentKey":     binding.EnvironmentKey,
		"workflowTemplateId": binding.WorkflowTemplateID,
	}
	retentionUntil := artifactTimeValue(artifact["retentionUntil"])
	if retentionUntil == nil {
		retentionUntil = artifactRetentionDeadline(artifact["retention"])
	}
	return domaindelivery.ExecutionArtifact{
		ID:                       "artifact:" + run.ID + ":" + node.ID + ":" + name,
		WorkflowRunID:            run.ID,
		WorkflowNodeID:           node.ID,
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		Kind:                     kind,
		Name:                     name,
		Ref:                      ref,
		Digest:                   digest,
		Path:                     path,
		Status:                   firstNonEmpty(workflowMetadataString(artifact, "status"), status),
		SizeBytes:                int64(toInt(firstNonNil(artifact["sizeBytes"], artifactValue(value, "sizeBytes")), 0)),
		Metadata:                 metadata,
		RetentionUntil:           retentionUntil,
	}
}

func artifactValue(value any, key string) any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped[key]
	}
	return nil
}

func artifactValueString(value any, key string) string {
	raw := artifactValue(value, key)
	if raw == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "<nil>" {
		return ""
	}
	return text
}

func dagArtifactRuntimeString(value any) string {
	if mapped, ok := value.(map[string]any); ok {
		for _, key := range []string{"ref", "digest", "path"} {
			if text := workflowMetadataString(mapped, key); text != "" {
				return text
			}
		}
		if raw, ok := mapped["value"]; ok {
			if text := dagArtifactRuntimeString(raw); text != "" {
				return text
			}
		}
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func artifactTimeValue(value any) *time.Time {
	switch typed := value.(type) {
	case time.Time:
		copyValue := typed.UTC()
		return &copyValue
	case string:
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

func artifactRetentionDeadline(value any) *time.Time {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return nil
	}
	if duration, err := time.ParseDuration(text); err == nil {
		deadline := time.Now().UTC().Add(duration)
		return &deadline
	}
	return artifactTimeValue(text)
}

func firstArtifactValue(name, kind string, outputs map[string]any, artifactState map[string]any) any {
	for _, key := range []string{name, kind} {
		if key == "" {
			continue
		}
		if value, ok := outputs[key]; ok {
			return value
		}
		if value, ok := artifactState[key]; ok {
			return value
		}
	}
	return nil
}
