package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
)

type externalDAGNodeHandler struct {
	store ExecutionTaskStore
}

func (h externalDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	task, logErr, err := createExternalDAGExecutionTask(ctx, h.store, input)
	if err != nil {
		return failedDAGNode(err)
	}
	result := completedDAGNode(fmt.Sprintf("execution task %s queued for %s", task.ID, task.ProviderKind))
	result.status = workflowStatusWaitingExecution
	result.outputs["executionTaskId"] = task.ID
	if logErr != nil {
		result.events = append(result.events, map[string]any{"type": "execution_log_write_failed", "summary": "execution task was queued without its initial log"})
	}
	return result
}

func createExternalDAGExecutionTask(ctx context.Context, store ExecutionTaskStore, input dagNodeExecutionInput) (domaindelivery.ExecutionTask, error, error) {
	if store == nil {
		return domaindelivery.ExecutionTask{}, nil, errors.New("execution task store is not configured")
	}
	node := input.node
	workflowInput := input.workflowInput
	providerKind := firstNonEmpty(node.ExecutorKind, configString(node.Config, "executorKind"), "webhook_callback")
	taskKind := firstNonEmpty(configString(node.Config, "taskKind"), node.Type)
	if taskKind == "external" {
		taskKind = "verify"
	}
	releaseBundleID := firstNonEmpty(workflowMetadataString(workflowInput.Variables, "releaseBundleId"), configString(node.Config, "releaseBundleId"))
	now := time.Now().UTC()
	payload := map[string]any{
		"workflowRunId":    input.run.ID,
		"workflowNodeId":   node.ID,
		"workflowNodeName": node.Name,
		"executorKind":     providerKind,
		"targetKind":       firstNonEmpty(node.TargetKind, configString(node.Config, "targetKind"), "external_service"),
		"capabilityRef":    node.CapabilityRef,
		"providerRef":      node.ProviderRef,
		"inputMapping":     node.InputMapping,
		"inputs":           input.resolvedInputs,
		"selectors":        input.selectors,
		"artifactKinds":    node.ArtifactKinds,
		"applicationId":    input.app.ID,
		"environmentKey":   input.binding.EnvironmentKey,
		"clusterId":        workflowInput.ClusterID,
		"namespace":        workflowInput.Namespace,
		"deploymentName":   workflowInput.DeploymentName,
	}
	task, err := store.CreateExecutionTask(ctx, domaindelivery.ExecutionTask{
		ID:                       "task:" + uuid.NewString(),
		ReleaseBundleID:          releaseBundleID,
		ApplicationID:            input.app.ID,
		ApplicationEnvironmentID: input.binding.ID,
		TaskKind:                 taskKind,
		ProviderKind:             providerKind,
		TargetKind:               firstNonEmpty(node.TargetKind, configString(node.Config, "targetKind"), "external_service"),
		Status:                   "queued",
		QueueKey:                 input.app.ID,
		LockKey:                  input.app.ID + ":" + taskKind,
		MaxRetries:               1,
		AttemptCount:             0,
		TimeoutSeconds:           node.TimeoutSeconds,
		CallbackToken:            uuid.NewString(),
		Payload:                  payload,
		Result:                   map[string]any{},
		CreatedAt:                now,
		UpdatedAt:                now,
	})
	if err != nil {
		return domaindelivery.ExecutionTask{}, nil, err
	}
	// The task is authoritative. A best-effort log failure must not enqueue it twice.
	logErr := store.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: task.ID,
		LogLevel:        "info",
		Message:         fmt.Sprintf("workflow node %s queued external execution task", node.ID),
		Metadata: map[string]any{
			"workflowRunId":  input.run.ID,
			"workflowNodeId": node.ID,
			"providerKind":   providerKind,
			"capabilityRef":  node.CapabilityRef,
		},
		CreatedAt: now,
	})
	return domaindelivery.WithOperationState(task, now), logErr, nil
}

type manualApprovalDAGNodeHandler struct{}

func (manualApprovalDAGNodeHandler) execute(context.Context, dagNodeExecutionInput) dagNodeHandlerResult {
	result := completedDAGNode("waiting for approval")
	result.status = workflowStatusWaitingApproval
	return result
}

type releaseDAGNodeHandler struct {
	releases ReleaseExecutor
}

func (h releaseDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	workflowInput := input.workflowInput
	node := input.node
	if workflowInput.ValidationOnly {
		return skippedDAGNode("release node skipped in validation mode")
	}
	if h.releases == nil {
		return failedDAGNode(errors.New("release executor is not configured"))
	}
	targets := append([]domaincatalog.ReleaseTarget(nil), input.selectedTargets...)
	if len(targets) == 0 {
		if target := matchBindingTarget(input.binding, workflowInput); target != nil {
			targets = append(targets, *target)
		}
	}
	containerName := configString(node.Config, "containerName")
	if containerName == "" && len(targets) > 0 {
		containerName = targets[0].ContainerName
	}
	actionKind := strings.TrimSpace(input.binding.ReleasePolicy.ActionKind)
	if actionKind == "" {
		actionKind = configString(node.Config, "actionKind")
	}
	if actionKind == "" {
		actionKind = "deploy"
	}
	resolvedImage := dagArtifactRuntimeString(input.artifactState["image"])
	imageTag := configString(node.Config, "imageTag")
	if configString(node.Config, "imageTagSource") == "build_artifact" && resolvedImage == "" {
		return failedDAGNode(errors.New("build artifact image is not available"))
	}
	if len(targets) == 0 {
		targets = append(targets, domaincatalog.ReleaseTarget{
			ID:            "input",
			ClusterID:     workflowInput.ClusterID,
			Namespace:     workflowInput.Namespace,
			WorkloadName:  workflowInput.DeploymentName,
			ContainerName: workflowInput.ContainerName,
			Enabled:       true,
		})
	}

	fanOutItems := make([]map[string]any, 0, len(targets))
	failedTargets := 0
	for index, target := range targets {
		record, err := h.releases.Trigger(ctx, input.principal, domainrelease.TriggerInput{
			ApplicationID:            input.app.ID,
			ApplicationEnvironmentID: input.binding.ID,
			ClusterID:                targetClusterID(&target, workflowInput),
			Namespace:                targetNamespace(&target, workflowInput),
			DeploymentName:           targetWorkloadName(&target, workflowInput),
			ContainerName:            firstNonEmpty(workflowInput.ContainerName, containerName, target.ContainerName),
			Image:                    resolvedImage,
			ImageTag:                 firstNonEmpty(workflowInput.ImageTag, imageTag),
			ReleaseName:              workflowInput.ReleaseName,
			ActionKind:               actionKind,
			WorkflowRunID:            input.run.ID,
		})
		targetStatus, targetSummary, releaseID, failed := releaseTargetResult(record, err)
		if failed {
			failedTargets++
		}
		fanOutItems = append(fanOutItems, map[string]any{
			"index":     index,
			"target":    releaseTargetMetadata(target),
			"status":    targetStatus,
			"summary":   targetSummary,
			"releaseId": releaseID,
		})
		if err != nil && normalizeDAGFanOutFailurePolicy(node.FanOutFailurePolicy) != "continue" && effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node) != "continue" {
			break
		}
	}

	result := completedDAGNode("")
	if len(targets) > 1 {
		result.outputs["fanOut"] = map[string]any{
			"strategy":      firstNonEmpty(normalizeDAGFanOutStrategy(node.FanOutStrategy), "parallel"),
			"targetCount":   len(targets),
			"failurePolicy": firstNonEmpty(normalizeDAGFanOutFailurePolicy(node.FanOutFailurePolicy), effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node)),
			"targets":       fanOutItems,
		}
		result.events = append(result.events, map[string]any{"type": "node_fan_out_completed", "fanOut": result.outputs["fanOut"]})
	}
	if failedTargets > 0 {
		result.status = "failed"
		result.summary = fmt.Sprintf("release fan-out failed on %d/%d targets", failedTargets, len(targets))
		return result
	}
	if len(fanOutItems) == 1 {
		result.summary = strings.TrimSpace(fmt.Sprint(fanOutItems[0]["summary"]))
	} else {
		result.summary = fmt.Sprintf("release fan-out completed on %d targets", len(fanOutItems))
	}
	return result
}

func releaseTargetResult(record domainrelease.Record, err error) (status, summary, releaseID string, failed bool) {
	if err != nil {
		return "failed", err.Error(), "", true
	}
	return record.Status, fmt.Sprintf("release %s finished with status %s", record.ID, record.Status), record.ID, false
}

type buildDAGNodeHandler struct {
	builds dagBuildExecutor
}

type dagBuildExecutor interface {
	Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
}

func (h buildDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	workflowInput := input.workflowInput
	if workflowInput.ValidationOnly {
		return skippedDAGNode("build node skipped in validation mode")
	}
	if h.builds == nil {
		return failedDAGNode(errors.New("build executor is not configured"))
	}
	record, err := h.builds.Execute(ctx, input.principal, domainbuild.TriggerInput{
		ApplicationID:            input.app.ID,
		ApplicationEnvironmentID: input.binding.ID,
		BuildSourceID:            firstNonEmpty(workflowInput.BuildSourceID, input.binding.BuildPolicy.SourceID),
		RefType:                  firstNonEmpty(workflowInput.RefType, input.binding.BuildPolicy.RefType, "branch"),
		RefName:                  firstNonEmpty(workflowInput.RefName, input.binding.BuildPolicy.RefValue, input.app.DefaultBranch, "main"),
		ImageTag:                 firstNonEmpty(workflowInput.ImageTag, input.app.DefaultTag),
		BuildArgs:                mergeDAGMaps(input.binding.BuildPolicy.BuildArgs, workflowInput.BuildArgs),
		Variables:                mergeDAGMaps(input.binding.BuildPolicy.Variables, workflowInput.Variables),
		TriggeredByWorkflowRunID: input.run.ID,
	})
	if err != nil {
		return failedDAGNode(err)
	}
	result := completedDAGNode(fmt.Sprintf("build %s queued", record.ID))
	if record.Metadata != nil {
		if artifact, ok := record.Metadata["artifact"]; ok {
			result.outputs["artifact"] = artifact
		}
		if image, ok := record.Metadata["image"]; ok {
			result.outputs["image"] = image
		}
	}
	return result
}
