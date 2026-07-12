package workflow

import (
	"strings"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func matchBindingTarget(binding domaincatalog.ApplicationEnvironment, input domainworkflow.Input) *domaincatalog.ReleaseTarget {
	for _, target := range binding.Targets {
		if !target.Enabled {
			continue
		}
		if bindingID := strings.TrimSpace(input.ApplicationEnvironmentID); bindingID != "" && binding.ID != bindingID {
			continue
		}
		if target.ClusterID == strings.TrimSpace(input.ClusterID) &&
			target.Namespace == strings.TrimSpace(input.Namespace) &&
			target.WorkloadName == strings.TrimSpace(input.DeploymentName) {
			copyTarget := target
			return &copyTarget
		}
	}
	return nil
}

func selectedDAGTargetsFromSelectors(selectors map[string]any) []domaincatalog.ReleaseTarget {
	if len(selectors) == 0 {
		return nil
	}
	if rawTargets, ok := selectors["targets"].([]map[string]any); ok {
		targets := make([]domaincatalog.ReleaseTarget, 0, len(rawTargets))
		for _, item := range rawTargets {
			targets = append(targets, dagTargetFromMetadata(item))
		}
		return targets
	}
	if rawTargets, ok := selectors["targets"].([]any); ok {
		targets := make([]domaincatalog.ReleaseTarget, 0, len(rawTargets))
		for _, item := range rawTargets {
			if mapped, ok := item.(map[string]any); ok {
				targets = append(targets, dagTargetFromMetadata(mapped))
			}
		}
		if len(targets) > 0 {
			return targets
		}
	}
	targetMap, ok := selectors["target"].(map[string]any)
	if !ok || len(targetMap) == 0 {
		return nil
	}
	return []domaincatalog.ReleaseTarget{dagTargetFromMetadata(targetMap)}
}

func dagTargetFromMetadata(targetMap map[string]any) domaincatalog.ReleaseTarget {
	target := domaincatalog.ReleaseTarget{
		ID:            workflowMetadataString(targetMap, "id"),
		ClusterID:     workflowMetadataString(targetMap, "clusterId"),
		Namespace:     workflowMetadataString(targetMap, "namespace"),
		TargetKind:    workflowMetadataString(targetMap, "targetKind"),
		ExecutorKind:  workflowMetadataString(targetMap, "executorKind"),
		GroupKey:      workflowMetadataString(targetMap, "groupKey"),
		WaveKey:       workflowMetadataString(targetMap, "waveKey"),
		RegionKey:     workflowMetadataString(targetMap, "regionKey"),
		ConfigRef:     workflowMetadataString(targetMap, "configRef"),
		WorkloadKind:  workflowMetadataString(targetMap, "workloadKind"),
		WorkloadName:  workflowMetadataString(targetMap, "workloadName"),
		ContainerName: workflowMetadataString(targetMap, "containerName"),
		Enabled:       true,
	}
	if metadata, ok := targetMap["metadata"].(map[string]any); ok {
		target.Metadata = metadata
	}
	return target
}

func selectedTargetWorkloadName(target *domaincatalog.ReleaseTarget) string {
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.WorkloadName)
}

func targetClusterID(target *domaincatalog.ReleaseTarget, input domainworkflow.Input) string {
	if target == nil {
		return strings.TrimSpace(input.ClusterID)
	}
	return firstNonEmpty(target.ClusterID, input.ClusterID)
}

func targetNamespace(target *domaincatalog.ReleaseTarget, input domainworkflow.Input) string {
	if target == nil {
		return strings.TrimSpace(input.Namespace)
	}
	return firstNonEmpty(target.Namespace, input.Namespace)
}

func targetWorkloadName(target *domaincatalog.ReleaseTarget, input domainworkflow.Input) string {
	if target == nil {
		return strings.TrimSpace(input.DeploymentName)
	}
	return firstNonEmpty(target.WorkloadName, input.DeploymentName)
}

func normalizeDAGFanOutStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "":
		return ""
	case "parallel", "serial", "batch":
		return strings.ToLower(strings.TrimSpace(strategy))
	default:
		return ""
	}
}

func normalizeDAGFanOutFailurePolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "continue", "stop", "rollback":
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return ""
	}
}
