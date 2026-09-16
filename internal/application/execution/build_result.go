package execution

import (
	"context"
	"fmt"
	"maps"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func (s *Service) updateExecutionTask(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	if task.TaskKind == "build" {
		task.Result = buildResultWithProvenance(task)
		if task.Status == "completed" {
			if _, err := BuildArtifactImage(task); err != nil {
				task.Status = "failed"
				now := time.Now().UTC()
				task.FinishedAt = &now
				task.Result["executionTaskStatus"] = task.Status
				task.Result["failureReason"] = "invalid_artifact"
				task.Result["error"] = err.Error()
			}
		}
	}
	return s.repo.UpdateExecutionTask(ctx, task)
}

// BuildArtifactImage validates both the runner result and the server-selected
// output repository. Callers still enforce task and bundle ownership.
func BuildArtifactImage(task domaindelivery.ExecutionTask) (string, error) {
	ref, digest := resolveArtifactRef(task.Result, task.Payload), resolveArtifactDigest(task.Result, nil)
	image, err := domaindelivery.ImmutableImageReference(ref, digest)
	if err != nil {
		return "", err
	}
	if expected := valueAsString(task.Payload["image"]); expected != "" {
		selected, err := domaindelivery.ImmutableImageReference(expected, digest)
		if err != nil || selected != image {
			return "", fmt.Errorf("build artifact does not match the selected output repository")
		}
	}
	return image, nil
}

func buildResultWithProvenance(task domaindelivery.ExecutionTask) map[string]any {
	result := maps.Clone(task.Result)
	if result == nil {
		result = map[string]any{}
	}
	for _, key := range []string{"serviceId", "containerName", "buildSourceId", "repositoryId", "repositoryRefs", "repositoryBindings", "resolvedCommit", "refType", "refName", "buildTemplateId", "buildTemplateVersion", "workflowRunId", "workflowNodeId", "buildRecordId"} {
		result[key] = task.Payload[key]
	}
	result["applicationId"] = task.ApplicationID
	result["applicationEnvironmentId"] = task.ApplicationEnvironmentID
	result["executionTaskId"] = task.ID
	result["releaseBundleId"] = task.ReleaseBundleID
	return result
}
