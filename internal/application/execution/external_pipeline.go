package execution

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExternalPipelineResolver interface {
	ExternalPipelineProvider(context.Context, sohaapi.ExternalPipelineExecutionSpec) (domainbuild.PipelineProvider, func(), error)
}

type BuildImageVerifier interface {
	VerifyBuildImage(context.Context, string, string, string) error
}

type externalPipelineDispatchRepository interface {
	BeginExternalPipelineDispatch(context.Context, string) (domaindelivery.ExecutionTask, bool, error)
}

func (s *Service) SetExternalPipeline(resolver ExternalPipelineResolver, images BuildImageVerifier) {
	s.pipelines, s.pipelineImages = resolver, images
}

func externalPipelineRequest(task domaindelivery.ExecutionTask) (domainbuild.PipelineRequest, error) {
	request := domainbuild.PipelineRequest{TaskID: task.ID, Image: valueAsString(task.Payload["image"]), CreatedAt: task.CreatedAt, Variables: map[string]string{}}
	data, err := json.Marshal(task.Payload["externalPipeline"])
	if err != nil || json.Unmarshal(data, &request.Spec) != nil || task.TaskKind != "build" || request.Image == "" || request.Spec.SourceConnectionID == "" || request.Spec.ProviderProjectID == "" || request.Spec.Configuration.RegistryID == "" {
		return request, fmt.Errorf("%w: frozen external pipeline identity is missing", apperrors.ErrInvalidArgument)
	}
	for _, commit := range []string{request.Spec.SourceCommit, request.Spec.PipelineCommit} {
		if len(commit) != 40 {
			return request, apperrors.ErrInvalidArgument
		}
		if _, err := hex.DecodeString(commit); err != nil {
			return request, apperrors.ErrInvalidArgument
		}
	}
	for key, source := range map[string]string{"SOHA_BUILD_ARGS_JSON": "buildArgs", "SOHA_VARIABLES_JSON": "variables"} {
		encoded, err := json.Marshal(task.Payload[source])
		if err != nil || len(encoded) > 64<<10 {
			return request, fmt.Errorf("%w: invalid CI build parameters", apperrors.ErrInvalidArgument)
		}
		request.Variables[key] = string(encoded)
	}
	return request, nil
}

func (s *Service) dispatchExternalPipeline(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	repo, ok := s.repo.(externalPipelineDispatchRepository)
	if !ok || s.pipelines == nil || s.pipelineImages == nil {
		return task, fmt.Errorf("%w: durable external pipeline execution is unavailable", apperrors.ErrInvalidArgument)
	}
	task, won, err := repo.BeginExternalPipelineDispatch(ctx, task.ID)
	if err != nil || !won {
		return task, err
	}
	request, err := externalPipelineRequest(task)
	if err != nil {
		return s.recordExternalPipeline(ctx, task, domainbuild.PipelineInspection{Run: sohaapi.ExternalPipelineRun{Status: "invalid_configuration", StopConfirmed: true}}, err)
	}
	provider, closeClient, err := s.pipelines.ExternalPipelineProvider(ctx, request.Spec)
	if err != nil {
		return s.recordExternalPipeline(ctx, task, domainbuild.PipelineInspection{Run: sohaapi.ExternalPipelineRun{Status: "connection_unavailable", StopConfirmed: true}}, err)
	}
	defer closeClient()
	run, dispatchErr := provider.StartPipeline(ctx, request)
	// Keep the returned identity even if the caller disconnected during dispatch.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return s.recordExternalPipeline(persistCtx, task, domainbuild.PipelineInspection{Run: run}, dispatchErr)
}

func (s *Service) reconcileExternalPipeline(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time) (domaindelivery.ExecutionTask, error) {
	if isStrictTerminalTaskStatus(task.Status) {
		return task, nil
	}
	if task.Status == "queued" {
		return s.dispatchExternalPipeline(ctx, task)
	}
	if deliveryTaskExecutionExpired(task, now) {
		repo, ok := s.repo.(deliveryTaskStopRepository)
		if !ok {
			return task, apperrors.ErrConflict
		}
		var err error
		task, err = repo.RequestDeliveryTaskStop(ctx, task.ID, executionTimeoutReason)
		if err != nil {
			return task, err
		}
	}
	request, err := externalPipelineRequest(task)
	if err != nil {
		return task, err
	}
	if s.pipelines == nil || s.pipelineImages == nil {
		return task, apperrors.ErrClusterUnready
	}
	run, err := externalPipelineRun(task)
	if err != nil {
		return task, err
	}
	provider, closeClient, err := s.pipelines.ExternalPipelineProvider(ctx, request.Spec)
	if err != nil {
		run.StopConfirmed = false
		return s.recordExternalPipeline(ctx, task, domainbuild.PipelineInspection{Run: run}, fmt.Errorf("%w: external CI connection is unavailable or its policy changed", apperrors.ErrClusterUnready))
	}
	defer closeClient()
	if run.RunID == "" {
		run, err = provider.FindPipeline(ctx, request)
		if err != nil || run.RunID == "" {
			return s.recordExternalPipeline(ctx, task, domainbuild.PipelineInspection{Run: run}, err)
		}
	}
	inspection, err := provider.InspectPipeline(ctx, request, run.RunID, task.Status == "canceling")
	if err == nil && inspection.Run.StopConfirmed && inspection.Run.Status == "success" && task.Status != "canceling" {
		if inspection.ImageDigest == "" {
			err = fmt.Errorf("%w: CI artifact digest is missing", apperrors.ErrConflict)
		} else {
			err = s.pipelineImages.VerifyBuildImage(ctx, request.Spec.Configuration.RegistryID, request.Image, inspection.ImageDigest)
		}
	}
	return s.recordExternalPipeline(ctx, task, inspection, err)
}

func externalPipelineRun(task domaindelivery.ExecutionTask) (sohaapi.ExternalPipelineRun, error) {
	var run sohaapi.ExternalPipelineRun
	data, err := json.Marshal(task.Result["externalPipeline"])
	if err == nil {
		err = json.Unmarshal(data, &run)
	}
	if err != nil {
		return run, fmt.Errorf("%w: invalid persisted CI identity", apperrors.ErrConflict)
	}
	return run, nil
}

func (s *Service) recordExternalPipeline(ctx context.Context, task domaindelivery.ExecutionTask, inspection domainbuild.PipelineInspection, inspectionErr error) (domaindelivery.ExecutionTask, error) {
	now := time.Now().UTC()
	run := inspection.Run
	if run.Status == "" {
		run.Status = "unknown"
	}
	task.Result = mergeMaps(task.Result, map[string]any{"externalPipeline": run})
	if inspectionErr != nil {
		task.Result["externalPipelineError"] = "External CI reconciliation failed: " + inspectionErr.Error()
	} else {
		delete(task.Result, "externalPipelineError")
	}
	if run.Status == "identity_mismatch" || run.Status == "duplicate_runs" {
		task.Status, task.Result["pipelineFailure"] = "canceling", run.Status
	}
	if run.StopConfirmed {
		task.Status = externalPipelineTerminalStatus(task, run, inspectionErr)
		task.FinishedAt = &now
		if task.Status == "completed" {
			task.Result["image"], task.Result["imageDigest"] = task.Payload["image"], inspection.ImageDigest
			var report sohaapi.ExternalPipelineArtifactReport
			if json.Unmarshal(inspection.Artifact, &report) == nil {
				task.Result["externalPipelineArtifact"] = report
			}
		} else if task.Result["cancelReason"] == executionTimeoutReason {
			task.Result["error"] = executionTimeoutMessage(task)
		} else if task.Status == "failed" {
			task.Result["error"] = "External pipeline did not produce a verified build artifact (" + run.Status + ")."
		}
	} else if task.Status != "canceling" && run.RunID != "" {
		task.Status = "running"
	}
	task.UpdatedAt, task.LastHeartbeatAt = now, &now
	if inspectionErr == nil {
		task.LastRuntimeSeenAt = &now
	}
	updated, err := s.updateExecutionTask(ctx, task)
	if err != nil {
		return task, err
	}
	if err := s.syncBuildRecord(ctx, updated); err != nil {
		return updated, err
	}
	if isStrictTerminalTaskStatus(updated.Status) {
		if err := s.finishDeliveryTaskStop(ctx, updated); err != nil {
			return updated, err
		}
		if err := s.notifyWorkflowExecutionTaskResult(ctx, updated); err != nil {
			return updated, err
		}
	} else if err := s.notifyExecutionTaskSinks(ctx, updated); err != nil {
		return updated, err
	}
	return updated, nil
}

func externalPipelineTerminalStatus(task domaindelivery.ExecutionTask, run sohaapi.ExternalPipelineRun, err error) string {
	if task.Result["pipelineFailure"] != nil || task.Result["cancelReason"] == executionTimeoutReason {
		return "failed"
	}
	if task.Status == "canceling" {
		return "canceled"
	}
	if err == nil && run.Status == "success" && run.ArtifactJobID != "" && strings.HasPrefix(run.ArtifactDigest, "sha256:") {
		return "completed"
	}
	return "failed"
}
