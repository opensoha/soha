package aigateway

import (
	"context"
	"strings"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type releaseFailureDiagnosisInput struct {
	ApplicationID            string `json:"applicationId"`
	ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
	ReleaseBundleID          string `json:"releaseBundleId"`
	ExecutionTaskID          string `json:"executionTaskId"`
	ClusterID                string `json:"clusterId"`
	Namespace                string `json:"namespace"`
	WorkloadKind             string `json:"workloadKind"`
	WorkloadName             string `json:"workloadName"`
	PodName                  string `json:"podName"`
	Container                string `json:"container"`
	LogLimit                 int    `json:"logLimit"`
	EventLimit               int    `json:"eventLimit"`
	AgentProviderID          string `json:"agentProviderId"`
	ProviderID               string `json:"providerId"`
	DeepAnalysis             bool   `json:"deepAnalysis"`
	ExternalAnalysis         bool   `json:"externalAnalysis"`
	TimeoutSeconds           int    `json:"timeoutSeconds"`
}

func parseReleaseFailureDiagnosisRequest(input map[string]any) (releaseFailureDiagnosisRequest, error) {
	var raw releaseFailureDiagnosisInput
	if err := mapInput(input, &raw); err != nil {
		return releaseFailureDiagnosisRequest{}, err
	}
	agentProviderID := firstNonEmpty(strings.TrimSpace(raw.AgentProviderID), strings.TrimSpace(raw.ProviderID))
	return releaseFailureDiagnosisRequest{
		ApplicationID:            strings.TrimSpace(raw.ApplicationID),
		ApplicationEnvironmentID: strings.TrimSpace(raw.ApplicationEnvironmentID),
		ReleaseBundleID:          strings.TrimSpace(raw.ReleaseBundleID),
		ExecutionTaskID:          strings.TrimSpace(raw.ExecutionTaskID),
		ClusterID:                strings.TrimSpace(raw.ClusterID),
		Namespace:                strings.TrimSpace(raw.Namespace),
		WorkloadKind:             strings.TrimSpace(raw.WorkloadKind),
		WorkloadName:             strings.TrimSpace(raw.WorkloadName),
		PodName:                  strings.TrimSpace(raw.PodName),
		Container:                strings.TrimSpace(raw.Container),
		LogLimit:                 raw.LogLimit,
		EventLimit:               raw.EventLimit,
		AgentProviderID:          agentProviderID,
		DeepAnalysis:             raw.DeepAnalysis || raw.ExternalAnalysis || agentProviderID != "",
		TimeoutSeconds:           raw.TimeoutSeconds,
	}, nil
}

func newReleaseFailureDiagnosisContext(req releaseFailureDiagnosisRequest) (map[string]any, map[string]any) {
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleId":          req.ReleaseBundleID,
		"executionTaskId":          req.ExecutionTaskID,
		"clusterId":                req.ClusterID,
		"namespace":                req.Namespace,
	}
	contextView := map[string]any{
		"summary": "collected release failure diagnosis context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadKind":             req.WorkloadKind,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
		},
		"delivery": map[string]any{},
		"runtime":  map[string]any{},
		"findings": []string{
			"Evidence is collected through soha application services; this Gateway tool does not execute cluster mutations.",
		},
		"nextChecks": []string{},
	}
	return related, contextView
}

func (s *Service) collectReleaseDeliveryEvidence(ctx context.Context, principal domainidentity.Principal, req releaseFailureDiagnosisRequest, contextView, related map[string]any, nextChecks []string) []string {
	evidence := mapValue(contextView["delivery"])
	if s.delivery == nil {
		evidence["error"] = "delivery gateway services are not configured"
		return append(nextChecks, "Configure delivery services before collecting release execution evidence.")
	}
	if req.ExecutionTaskID != "" {
		limit := req.LogLimit
		if limit <= 0 {
			limit = 100
		}
		logs, err := s.delivery.ListExecutionLogs(ctx, principal, req.ExecutionTaskID, limit)
		if err != nil {
			evidence["executionLogsError"] = err.Error()
			nextChecks = append(nextChecks, "Re-check execution task logs after the delivery control plane is reachable.")
		} else {
			logs = redactExecutionLogs(logs)
			evidence["executionLogs"] = logs
			evidence["executionLogCount"] = len(logs)
			related["executionLogCount"] = len(logs)
		}
	}
	if req.ReleaseBundleID != "" {
		artifacts, err := s.delivery.ListReleaseBundleArtifacts(ctx, principal, req.ReleaseBundleID)
		if err != nil {
			evidence["releaseBundleArtifactsError"] = err.Error()
		} else {
			evidence["releaseBundleArtifacts"] = artifacts
			evidence["releaseBundleArtifactCount"] = len(artifacts)
			related["releaseBundleArtifactCount"] = len(artifacts)
		}
	}
	if req.ApplicationID != "" || req.ApplicationEnvironmentID != "" || req.ReleaseBundleID != "" {
		tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
			ApplicationID:            req.ApplicationID,
			ApplicationEnvironmentID: req.ApplicationEnvironmentID,
			ReleaseBundleID:          req.ReleaseBundleID,
			Limit:                    10,
		})
		if err != nil {
			evidence["executionTasksError"] = err.Error()
		} else {
			evidence["executionTasks"] = tasks
			evidence["executionTaskCount"] = len(tasks)
		}
	}
	return nextChecks
}

func (s *Service) collectReleaseRuntimeEvidence(ctx context.Context, principal domainidentity.Principal, req releaseFailureDiagnosisRequest, contextView map[string]any, nextChecks []string) []string {
	evidence := mapValue(contextView["runtime"])
	if req.ClusterID == "" {
		return append(nextChecks, "Provide clusterId and namespace to collect runtime Kubernetes evidence.")
	}
	if s.resources == nil {
		evidence["error"] = "Kubernetes resource gateway service is not configured"
		return nextChecks
	}
	eventLimit := req.EventLimit
	if eventLimit <= 0 {
		eventLimit = 100
	}
	if pods, err := s.resources.ListPods(ctx, principal, req.ClusterID, req.Namespace); err != nil {
		evidence["podsError"] = err.Error()
	} else {
		evidence["pods"] = filterPodsForDiagnosis(pods, req.PodName, req.WorkloadName)
	}
	if deployments, err := s.resources.ListDeployments(ctx, principal, req.ClusterID, req.Namespace); err != nil {
		evidence["deploymentsError"] = err.Error()
	} else {
		evidence["deployments"] = filterDeploymentsForDiagnosis(deployments, req.WorkloadName)
	}
	if services, err := s.resources.ListServices(ctx, principal, req.ClusterID, req.Namespace); err != nil {
		evidence["servicesError"] = err.Error()
	} else {
		evidence["services"] = services
	}
	if events, err := s.resources.ListClusterEvents(ctx, principal, req.ClusterID, req.Namespace, eventLimit); err != nil {
		evidence["eventsError"] = err.Error()
	} else {
		evidence["events"] = filterEventsForDiagnosis(events, req.PodName, req.WorkloadName)
	}
	if req.PodName != "" {
		logs, err := s.resources.GetPodLogs(ctx, principal, req.ClusterID, req.Namespace, req.PodName, req.Container, 200, 0, false)
		if err != nil {
			evidence["podLogsError"] = err.Error()
		} else {
			evidence["podLogs"] = redactPodLogs(logs)
		}
	}
	return nextChecks
}

func (s *Service) recordReleaseFailureAnalysis(ctx context.Context, principal domainidentity.Principal, req releaseFailureDiagnosisRequest, input, contextView, related map[string]any, nextChecks []string) {
	if s.copilot == nil {
		contextView["analysisArtifact"] = map[string]any{
			"artifactStored": false,
			"reason":         "AI Workbench artifact recorder is not configured",
		}
		return
	}
	artifactInput := buildReleaseFailureArtifactInput(req, input, contextView)
	if req.DeepAnalysis {
		s.queueReleaseFailureAnalysis(ctx, principal, req, artifactInput, contextView, related, nextChecks)
		return
	}
	s.persistReleaseFailureAnalysis(ctx, principal, artifactInput, contextView, related, nextChecks)
}

func (s *Service) queueReleaseFailureAnalysis(ctx context.Context, principal domainidentity.Principal, req releaseFailureDiagnosisRequest, artifactInput domaincopilot.GatewayAnalysisArtifactInput, contextView, related map[string]any, nextChecks []string) {
	run, err := s.copilot.QueueGatewayAnalysisAgentRun(ctx, principal, domaincopilot.GatewayAnalysisAgentRunInput{
		GatewayAnalysisArtifactInput: artifactInput,
		AgentProviderID:              req.AgentProviderID,
		TimeoutSeconds:               req.TimeoutSeconds,
	})
	if err != nil {
		contextView["analysisArtifactError"] = err.Error()
		contextView["nextChecks"] = append(nextChecks, "Retry external Agent Runtime queueing after the provider is available.")
		related["analysisArtifactError"] = err.Error()
		return
	}
	contextView["analysisArtifact"] = map[string]any{
		"agentRunId":     run.ID,
		"capabilityId":   run.CapabilityID,
		"providerId":     run.ProviderID,
		"providerKind":   run.ProviderKind,
		"status":         run.Status,
		"queued":         true,
		"artifactStored": false,
		"runtime":        "agent_runtime_claim_callback",
	}
	related["agentRunId"] = run.ID
	related["agentProviderId"] = run.ProviderID
	related["agentRunStatus"] = run.Status
}

func (s *Service) persistReleaseFailureAnalysis(ctx context.Context, principal domainidentity.Principal, artifactInput domaincopilot.GatewayAnalysisArtifactInput, contextView, related map[string]any, nextChecks []string) {
	run, err := s.copilot.RecordGatewayAnalysisArtifact(ctx, principal, artifactInput)
	if err != nil {
		contextView["analysisArtifactError"] = err.Error()
		contextView["nextChecks"] = append(nextChecks, "Retry analysis artifact persistence after the AI Workbench runtime is available.")
		related["analysisArtifactError"] = err.Error()
		return
	}
	contextView["analysisArtifact"] = map[string]any{
		"agentRunId":     run.ID,
		"capabilityId":   run.CapabilityID,
		"status":         run.Status,
		"artifactCount":  len(run.AnalysisArtifacts),
		"artifactKind":   firstAnalysisArtifactKind(run.AnalysisArtifacts),
		"artifactRunId":  firstAnalysisArtifactRunID(run.AnalysisArtifacts),
		"artifactTitle":  firstAnalysisArtifactTitle(run.AnalysisArtifacts),
		"artifactStored": len(run.AnalysisArtifacts) > 0,
	}
	related["agentRunId"] = run.ID
	related["analysisArtifactCount"] = len(run.AnalysisArtifacts)
}
