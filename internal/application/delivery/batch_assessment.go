package delivery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) AssessDeliveryTarget(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, target domainworkflow.DeliveryTargetSnapshot, input sohaapi.DeliveryBatchAssessmentInput) (sohaapi.DeliveryBatchAssessment, error) {
	result := sohaapi.DeliveryBatchAssessment{BatchID: input.BatchID, TargetID: target.Target.ID, ApplicationID: target.Target.ApplicationID, ServiceID: target.Target.ServiceID, ApplicationEnvironmentID: target.Target.ApplicationEnvironmentID,
		Verdict: "inconclusive", Summary: "a completed deployment with fresh evidence is required", Evidence: []sohaapi.CapabilityEvidence{}, Access: []sohaapi.DeliveryAccessResult{}}
	if input.MaxAgeSeconds == 0 {
		input.MaxAgeSeconds = 120
	}
	if input.BatchID == "" || input.TargetID != target.Target.ID || input.MaxAgeSeconds < 1 || input.MaxAgeSeconds > 600 {
		return result, apperrors.ErrInvalidArgument
	}
	if err := s.authorizeRuntimeScope(ctx, principal, result.ApplicationID, result.ApplicationEnvironmentID); err != nil {
		return result, err
	}
	node := batchStageNode(run, target.Target.ID, "deploy")
	result.DeliveryPlanID = node.DeliveryPlanID
	if node.DeliveryPlanID == "" || node.Status != "completed" {
		return result, nil
	}
	plan, err := s.repository.GetDeliveryPlan(ctx, node.DeliveryPlanID)
	if err != nil {
		return result, err
	}
	if !assessmentPlanMatches(plan, run, target) {
		return result, apperrors.ErrConflict
	}
	result.ReleaseBundleID = plan.ReleaseBundleID
	if finished, err := time.Parse(time.RFC3339, node.FinishedAt); err == nil {
		result.DeployedAt = &finished
	}
	var candidates []domaindelivery.AccessCandidate
	switch {
	case len(plan.DockerSnapshots) == 1:
		candidates, err = s.assessDockerTarget(ctx, principal, plan, target, input, &result)
	case len(plan.ManifestSnapshots) == 1:
		candidates, err = s.assessManifestTarget(ctx, principal, plan, target, input, &result)
	case len(plan.HelmSnapshots) == 1:
		candidates, err = s.assessHelmTarget(ctx, principal, plan, target, &result)
	}
	if err != nil {
		return result, err
	}
	if err := assessTargetAccess(ctx, input, candidates, &result); err != nil {
		return result, err
	}
	result.Summary = fmt.Sprintf("deployed target verification is %s; evidence and access results describe the checked conditions", result.Verdict)
	return result, nil
}

func assessmentPlanMatches(plan domaindelivery.DeliveryPlan, run domainworkflow.Run, target domainworkflow.DeliveryTargetSnapshot) bool {
	return plan.Source == domainworkflow.ScopeDeliveryBatch && plan.ApplicationID == target.Target.ApplicationID && plan.ApplicationEnvironmentID == target.Target.ApplicationEnvironmentID && plan.Impact["workflowRunId"] == run.ID && plan.Impact["workflowTargetId"] == target.Target.ID && len(plan.DockerSnapshots)+len(plan.ManifestSnapshots)+len(plan.HelmSnapshots) == 1
}

func (s *Service) assessDockerTarget(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan, target domainworkflow.DeliveryTargetSnapshot, input sohaapi.DeliveryBatchAssessmentInput, result *sohaapi.DeliveryBatchAssessment) ([]domaindelivery.AccessCandidate, error) {
	snapshot := plan.DockerSnapshots[0]
	if s.docker == nil || snapshot.DeliveryPlanID != plan.ID || snapshot.TargetID != target.Target.ReleaseTargetID || snapshot.ServiceID != result.ServiceID {
		return nil, apperrors.ErrConflict
	}
	op, err := s.docker.GetOperation(ctx, principal, snapshot.DeployOperationID)
	if err != nil {
		return nil, err
	}
	if !validDockerOperation(op, snapshot, "delivery_deploy") {
		return nil, apperrors.ErrConflict
	}
	result.Images, result.DeployedAt = snapshot.Images, op.FinishedAt
	assessment, err := s.docker.AssessProject(ctx, principal, appdocker.ProjectAssessmentInput{ProjectID: snapshot.ProjectID, AfterOperationID: snapshot.DeployOperationID, ExpectedServices: snapshot.ExpectedServices, ExpectedImages: snapshot.Images, MaxAgeSeconds: input.MaxAgeSeconds})
	if err != nil {
		return nil, err
	}
	result.Verdict, result.Evidence = sohaapi.DeliveryBatchAssessmentVerdict(assessment.Verdict), assessment.Evidence
	return s.docker.DeliveryProjectAccess(ctx, principal, snapshot, plan.DockerPrepared[snapshot.TargetID])
}

func (s *Service) assessManifestTarget(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan, target domainworkflow.DeliveryTargetSnapshot, input sohaapi.DeliveryBatchAssessmentInput, result *sohaapi.DeliveryBatchAssessment) ([]domaindelivery.AccessCandidate, error) {
	snapshot := plan.ManifestSnapshots[0]
	if s.manifestDelivery == nil || target.FrozenReleaseTarget == nil || snapshot.DeliveryPlanID != plan.ID || snapshot.TargetID != target.Target.ReleaseTargetID || snapshot.ServiceID != result.ServiceID {
		return nil, apperrors.ErrConflict
	}
	deployment, err := s.manifestDelivery.DeliveryDeployment(ctx, principal, result.ApplicationID, result.ApplicationEnvironmentID, *target.FrozenReleaseTarget)
	if err != nil {
		return nil, err
	}
	result.SourceCommit = snapshot.SourceCommit
	if snapshot.TemplateInputs != nil {
		result.Images = snapshot.TemplateInputs.ArtifactImages
	}
	result.Verdict, result.Evidence = assessManifestDeployment(deployment, snapshot, input.MaxAgeSeconds, time.Now().UTC())
	return s.manifestAccessCandidates(ctx, principal, snapshot.ClusterID, snapshot.Namespace, snapshot.Documents)
}

func assessManifestDeployment(deployment domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot, maxAge int, now time.Time) (sohaapi.DeliveryBatchAssessmentVerdict, []sohaapi.CapabilityEvidence) {
	status := deployment.Status
	evidence := []sohaapi.CapabilityEvidence{{Kind: "runtime_inventory", Source: "manifest.reconciler", ObservedAt: now, DataThrough: status.LastReconciledAt, Incomplete: true, Summary: "frozen manifest generation and resource health", Resource: &sohaapi.CapabilityResourceRef{Kind: "manifest.deployment", ID: deployment.ID, Scope: map[string]string{"clusterId": snapshot.ClusterID, "namespace": snapshot.Namespace}}}}
	selected := deployment.Spec.DeliverySnapshot
	if selected == nil || selected.DeliveryPlanID != snapshot.DeliveryPlanID || selected.RenderedDigest != snapshot.RenderedDigest {
		return "unsatisfied", evidence
	}
	if !freshAssessmentTime(status.LastReconciledAt, maxAge, now) {
		return "inconclusive", evidence
	}
	if status.Phase != domainmanifest.DeploymentPhaseConverged || status.ObservedGeneration != deployment.Generation || status.AppliedDigest != snapshot.RenderedDigest {
		return "unsatisfied", evidence
	}
	if verdict := assessManifestInventory(deployment, snapshot, maxAge, now); verdict != "satisfied" {
		return verdict, evidence
	}
	for _, condition := range status.Conditions {
		if condition.Type == "Healthy" && condition.Status == "true" && condition.ObservedGeneration == deployment.Generation {
			evidence[0].Incomplete = false
			return "satisfied", evidence
		}
	}
	return "inconclusive", evidence
}

func assessManifestInventory(deployment domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot, maxAge int, now time.Time) sohaapi.DeliveryBatchAssessmentVerdict {
	documents := slices.Concat(snapshot.Documents, snapshot.GitOpsDocuments)
	if len(documents) == 0 || len(documents) != len(deployment.Status.Inventory) {
		return "inconclusive"
	}
	for _, document := range documents {
		found := false
		for _, item := range deployment.Status.Inventory {
			if item.Kind != document.Kind || item.Name != document.Name || item.Namespace != document.Namespace || item.APIVersion != document.APIVersion {
				continue
			}
			found = true
			if item.UID == "" || item.Generation != deployment.Generation || !freshAssessmentTime(&item.LastObservedAt, maxAge, now) {
				return "inconclusive"
			}
			if item.DeletingAt != nil || item.Health != "healthy" || item.DesiredObjectDigest != document.ContentDigest || item.ObservedObjectDigest != document.ContentDigest {
				return "unsatisfied"
			}
		}
		if !found {
			return "inconclusive"
		}
	}
	return "satisfied"
}

func freshAssessmentTime(value *time.Time, maxAge int, now time.Time) bool {
	return value != nil && !value.IsZero() && !value.After(now.Add(5*time.Second)) && now.Sub(*value) <= time.Duration(maxAge)*time.Second
}

func (s *Service) assessHelmTarget(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan, target domainworkflow.DeliveryTargetSnapshot, result *sohaapi.DeliveryBatchAssessment) ([]domaindelivery.AccessCandidate, error) {
	snapshot := plan.HelmSnapshots[0]
	if s.helm.Runtime == nil || snapshot.DeliveryPlanID != plan.ID || snapshot.TargetID != target.Target.ReleaseTargetID || snapshot.ServiceID != result.ServiceID {
		return nil, apperrors.ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	observed, err := s.helm.Runtime.ExecuteHelmDelivery(ctx, principal, sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Observe, Snapshot: snapshot})
	if errors.Is(err, apperrors.ErrAccessDenied) {
		return nil, err
	}
	now := time.Now().UTC()
	evidence := sohaapi.CapabilityEvidence{Kind: "runtime_inventory", Source: "helm.runtime.observe", ObservedAt: now, DataThrough: &now, Incomplete: err != nil || len(observed.Resources) == 0, Summary: "current Helm revision and resource health", Resource: &sohaapi.CapabilityResourceRef{Kind: "kubernetes.helm_release", ID: snapshot.ReleaseName, Scope: map[string]string{"clusterId": snapshot.ClusterID, "namespace": snapshot.Namespace}}}
	result.Evidence = append(result.Evidence, evidence)
	if err == nil && !evidence.Incomplete {
		result.Verdict = "unsatisfied"
		if observed.Ready && observed.Status == "deployed" && observed.Revision == snapshot.ExpectedRevision+1 && observed.RenderedDigest == snapshot.RenderedDigest {
			result.Verdict = "satisfied"
		}
	}
	prepared, err := s.storedHelmPreparation(plan, snapshot)
	if err != nil {
		return nil, err
	}
	documents, err := accessManifestDocuments(prepared.Payload.Prepared.Manifest)
	if err != nil {
		return nil, err
	}
	return s.manifestAccessCandidates(ctx, principal, snapshot.ClusterID, snapshot.Namespace, documents)
}
