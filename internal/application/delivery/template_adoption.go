package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type TemplateUpdates interface {
	ReleaseReference(context.Context, domainidentity.Principal, string, int64) (*sohaapi.TemplateVersionReference, error)
	AssessRelease(context.Context, domainidentity.Principal, sohaapi.TemplateUpdateKind, string, int64, int64) (*sohaapi.TemplateMigrationAssessment, error)
	Candidate(context.Context, domainidentity.Principal, string, bool) (domainupdate.FrozenCandidate, error)
	ReadCandidate(context.Context, domainidentity.Principal, string) (domainupdate.FrozenCandidate, error)
	AuthorizeApproval(context.Context, domainidentity.Principal, string) error
	Apply(context.Context, domainidentity.Principal, string, string, func(context.Context) error) error
}

func (s *Service) SetTemplateUpdates(owner TemplateUpdates) { s.templateUpdates = owner }

func (s *Service) createTemplateAdoptionPlan(ctx context.Context, p domainidentity.Principal, input domaindelivery.DeliveryPlanInput) (domaindelivery.DeliveryPlan, error) {
	if s.templateUpdates == nil || input.TemplateCandidateID == "" || input.Source != domainworkflow.ScopeDeliveryBatch || input.Action != domaindelivery.ApplicationDeliveryActionDeploy {
		return domaindelivery.DeliveryPlan{}, fmt.Errorf("%w: create template Plans from a frozen adoption batch", apperrors.ErrInvalidArgument)
	}
	if input.ReleaseBundleID != "" || input.ManifestRevision != 0 || input.HelmRevision != 0 || len(input.Variables) > 0 || len(input.BuildArgs) > 0 {
		return domaindelivery.DeliveryPlan{}, apperrors.ErrInvalidArgument
	}
	frozen, err := s.templateUpdates.Candidate(ctx, p, input.TemplateCandidateID, true)
	if err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	target := frozen.Candidate.Target
	if input.ApplicationID != target.ApplicationID || input.ApplicationEnvironmentID != target.ApplicationEnvironmentID || input.TargetID != target.ReleaseTargetID {
		return domaindelivery.DeliveryPlan{}, apperrors.ErrConflict
	}
	input.ID = firstNonEmpty(input.ID, uuid.NewString())
	input.ApplicationName = target.ApplicationName
	input.RequiresApproval = frozen.Candidate.RequiresApproval
	input.RiskLevel = "medium"
	if input.RequiresApproval {
		input.RiskLevel = "high"
	}
	input.TargetSummary = fmt.Sprintf("%s v%d → v%d · 配置采用，尚未部署", target.TemplateID, target.FromVersion, frozen.Candidate.ToVersion)
	input.Impact = map[string]any{"templateCandidateId": frozen.Candidate.ID, "templatePreviewId": frozen.PreviewID, "templateDigest": frozen.TemplateDigest, "candidateDigest": frozen.Candidate.Digest, "configurationOnly": true, "requiresValidation": false}
	if migration := frozen.Candidate.Cumulative.Migration; migration != nil && migration.RequiresApproval {
		input.Impact["templateMigration"] = frozen.Candidate.Cumulative.Migration
	}
	input.RollbackStrategy = "采用历史模板版本需重新比较并创建配置 Plan；已运行资源保持原部署版本"
	return s.createPlanWithServiceReferenceImpact(ctx, p, input)
}

func (s *Service) confirmTemplateAdoptionPlan(ctx context.Context, p domainidentity.Principal, plan domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlanConfirmResult, error) {
	result := domaindelivery.DeliveryPlanConfirmResult{Plan: plan, Result: domaindelivery.ApplicationDeliveryActionResult{Action: domaindelivery.ApplicationDeliveryActionDeploy, ApplicationID: plan.ApplicationID, ApplicationEnvironmentID: plan.ApplicationEnvironmentID}}
	if s.templateUpdates == nil || plan.TemplateCandidateID == "" || plan.Source != domainworkflow.ScopeDeliveryBatch {
		return result, apperrors.ErrConflict
	}
	frozen, err := s.templateUpdates.Candidate(ctx, p, plan.TemplateCandidateID, true)
	if err != nil {
		return result, err
	}
	if !templatePlanMatchesCandidate(plan, frozen) {
		return result, apperrors.ErrConflict
	}
	if plan.Status == domaindelivery.DeliveryPlanStatusConfirmed {
		if frozen.AppliedPlanID != plan.ID {
			return result, apperrors.ErrConflict
		}
		return result, nil
	}
	if err := s.validateServiceReferenceImpact(ctx, p, plan); err != nil {
		return result, err
	}
	if err := validateDeliveryPlanConfirmStatus(plan); err != nil {
		return result, err
	}
	if plan.RequiresApproval && !deliveryPlanApprovalGranted(plan) {
		return result, fmt.Errorf("%w: template adoption approval is required", apperrors.ErrConflict)
	}
	previous := plan
	plan.Status = domaindelivery.DeliveryPlanStatusConfirming
	err = s.withValidatedServiceReferenceImpact(ctx, p, plan, func(lockctx context.Context) error {
		var updateErr error
		plan, updateErr = s.repository.UpdateDeliveryPlan(lockctx, plan)
		return updateErr
	})
	if err != nil {
		return result, err
	}
	// Keep the claimed CAS immutable: a failed receipt callback may have changed
	// its local confirmed value even though the adoption transaction rolled back.
	claimed := plan
	err = s.templateUpdates.Apply(ctx, p, plan.TemplateCandidateID, plan.ID, func(txctx context.Context) error {
		now := time.Now().UTC()
		confirmed := claimed
		confirmed.Status = domaindelivery.DeliveryPlanStatusConfirmed
		confirmed.ConfirmedAt = &now
		var updateErr error
		plan, updateErr = s.repository.UpdateDeliveryPlan(txctx, confirmed)
		return updateErr
	})
	if err != nil {
		result.Plan, err = s.recoverFailedTemplateAdoption(ctx, p, previous, claimed, err)
		return result, err
	}
	result.Plan = plan
	return result, nil
}

func templatePlanMatchesCandidate(plan domaindelivery.DeliveryPlan, frozen domainupdate.FrozenCandidate) bool {
	return plan.TemplateCandidateID == frozen.Candidate.ID && plan.Impact["candidateDigest"] == frozen.Candidate.Digest && plan.ApplicationID == frozen.Candidate.Target.ApplicationID && plan.ApplicationEnvironmentID == frozen.Candidate.Target.ApplicationEnvironmentID && plan.RequiresApproval == frozen.Candidate.RequiresApproval
}

func (s *Service) recoverFailedTemplateAdoption(ctx context.Context, p domainidentity.Principal, previous, claimed domaindelivery.DeliveryPlan, cause error) (domaindelivery.DeliveryPlan, error) {
	// The original request may have expired. Recovery is a bounded read/CAS, never
	// a retry of adoption, a new worker, or a relaxation of the source busy guard.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := s.repository.GetDeliveryPlan(ctx, claimed.ID)
	if err != nil {
		return claimed, errors.Join(cause, fmt.Errorf("read template Plan outcome: %w", err))
	}
	if current.ID != claimed.ID || current.TemplateCandidateID != claimed.TemplateCandidateID || current.Source != claimed.Source {
		return current, errors.Join(cause, apperrors.ErrConflict)
	}
	frozen, readErr := s.templateUpdates.ReadCandidate(ctx, p, claimed.TemplateCandidateID)
	if readErr == nil && !templatePlanMatchesCandidate(current, frozen) {
		return current, errors.Join(cause, apperrors.ErrConflict)
	}
	if readErr == nil && current.Status == domaindelivery.DeliveryPlanStatusConfirmed && frozen.AppliedPlanID == current.ID {
		return current, nil
	}
	if current.Status != domaindelivery.DeliveryPlanStatusConfirming || claimed.UpdatedAt.IsZero() || !current.UpdatedAt.Equal(claimed.UpdatedAt) {
		return current, errors.Join(cause, apperrors.ErrConflict)
	}
	if !templateAdoptionDefinitelyRolledBack(cause) {
		return current, errors.Join(cause, readErr)
	}
	// A deterministic failure and this exact persisted claim prove the atomic
	// adoption did not commit. Candidate read failures or another Plan's adoption
	// cannot strand it; an observed own applied receipt would contradict that proof.
	if readErr == nil && frozen.AppliedPlanID == claimed.ID {
		return current, errors.Join(cause, apperrors.ErrConflict)
	}
	restored := claimed
	restored.Status = domaindelivery.DeliveryPlanStatusDraft
	restored.ConfirmedAt = previous.ConfirmedAt
	stamp := claimed.UpdatedAt
	restored.ExpectedUpdatedAt = &stamp
	restored, err = s.repository.UpdateDeliveryPlan(ctx, restored)
	if err != nil {
		return current, errors.Join(cause, fmt.Errorf("restore template Plan after rollback: %w", err))
	}
	return restored, cause
}

func templateAdoptionDefinitelyRolledBack(err error) bool {
	// ApplyCandidate commits configuration, the Plan receipt and candidate adoption
	// together. Explicit business failures abort that callback; transport, timeout
	// and unknown database errors cannot prove its transaction outcome.
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		causes := group.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !templateAdoptionDefinitelyRolledBack(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return templateAdoptionDefinitelyRolledBack(wrapped.Unwrap())
	}
	switch err {
	case apperrors.ErrConflict, apperrors.ErrAccessDenied, apperrors.ErrUnauthorized, apperrors.ErrInvalidArgument, apperrors.ErrUnsupportedOperation, apperrors.ErrNotFound, apperrors.ErrGone:
		return true
	default:
		return false
	}
}

func (s *Service) freezeTemplateAdoption(ctx context.Context, p domainidentity.Principal, target domainworkflow.DeliveryTargetInput) (domainworkflow.DeliveryTargetSnapshot, error) {
	var snapshot domainworkflow.DeliveryTargetSnapshot
	if target.Action != "config_update" || target.TemplateCandidateID == "" || target.ReleaseBundleID != "" || target.HelmRevision != 0 || len(target.RepositoryRefs) > 0 || len(target.BuildArgs) > 0 {
		return snapshot, apperrors.ErrInvalidArgument
	}
	if s.templateUpdates == nil {
		return snapshot, apperrors.ErrConflict
	}
	frozen, err := s.templateUpdates.Candidate(ctx, p, target.TemplateCandidateID, true)
	if err != nil {
		return snapshot, err
	}
	if !domainupdate.IsPreviewBatch(ctx, frozen.PreviewID) {
		return snapshot, fmt.Errorf("%w: template adoption batches require their reviewed preview receipt", apperrors.ErrConflict)
	}
	t := frozen.Candidate.Target
	if target.ApplicationID != t.ApplicationID || target.ServiceID != t.ServiceID || target.ApplicationEnvironmentID != t.ApplicationEnvironmentID || target.ReleaseTargetID != t.ReleaseTargetID || frozen.AppliedPlanID != "" {
		return snapshot, apperrors.ErrConflict
	}
	snapshot = domainworkflow.DeliveryTargetSnapshot{Target: target, ApplicationName: t.ApplicationName, ServiceName: t.ServiceName, EnvironmentName: t.EnvironmentName, ConfigurationDigest: frozen.Candidate.Digest}
	snapshot.TemplateUpdate = &sohaapi.TemplateUpdateReceipt{Kind: t.Kind, TemplateID: t.TemplateID, BindingID: t.ID, PreviewID: frozen.PreviewID, CandidateID: frozen.Candidate.ID, FromVersion: t.FromVersion, ToVersion: frozen.Candidate.ToVersion, RequiresApproval: frozen.Candidate.RequiresApproval}
	snapshot.FrozenScope = deliveryTargetScope(frozen.Binding.Application, target, nil)
	return snapshot, nil
}

func (s *Service) executeTemplateBatchPlan(ctx context.Context, p domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	if s.templateUpdates == nil {
		return node, apperrors.ErrConflict
	}
	if _, err := s.templateUpdates.Candidate(ctx, p, snapshot.Target.TemplateCandidateID, true); err != nil {
		return node, err
	}
	capability, _ := domainworkflow.NodeExecutionFrom(ctx)
	plan, err := s.repository.GetDeliveryPlan(ctx, capability.ResourceID("plan"))
	if errors.Is(err, apperrors.ErrNotFound) {
		plan, err = s.CreateDeliveryPlan(ctx, p, domaindelivery.DeliveryPlanInput{ID: capability.ResourceID("plan"), Source: domainworkflow.ScopeDeliveryBatch, Action: domaindelivery.ApplicationDeliveryActionDeploy, TemplateCandidateID: snapshot.Target.TemplateCandidateID, ApplicationID: snapshot.Target.ApplicationID, ApplicationEnvironmentID: snapshot.Target.ApplicationEnvironmentID, TargetID: snapshot.Target.ReleaseTargetID})
	}
	if err != nil {
		return node, err
	}
	node.DeliveryPlanID = plan.ID
	if approvalStatus(plan) == "rejected" {
		node.Status, node.Summary = "failed", "template adoption Plan was rejected"
		return node, nil
	}
	if plan.RequiresApproval && !deliveryPlanApprovalGranted(plan) {
		if plan.Status != domaindelivery.DeliveryPlanStatusWaitingApproval {
			plan.Status = domaindelivery.DeliveryPlanStatusWaitingApproval
			plan.Impact = deliveryPlanApprovalImpact(plan, p, "requested", "")
			plan, err = s.repository.UpdateDeliveryPlan(ctx, plan)
			if err != nil {
				return node, err
			}
			s.recordDeliveryPlanApproval(ctx, p, plan, "requested", "")
		}
		node.Status, node.Summary = "waiting_approval", "template configuration is waiting for approval"
		return node, nil
	}
	if _, err := s.templateUpdates.Candidate(ctx, p, snapshot.Target.TemplateCandidateID, true); err != nil {
		return node, err
	}
	node.Status, node.Summary = "completed", "template configuration Plan is ready"
	return node, nil
}

func (s *Service) executeTemplateBatchAdoption(ctx context.Context, p domainidentity.Principal, run domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	node.DeliveryPlanID = batchStageNode(run, node.TargetID, "plan").DeliveryPlanID
	result, err := s.ConfirmDeliveryPlan(ctx, p, node.DeliveryPlanID)
	if err != nil {
		return node, err
	}
	if result.Plan.Status != domaindelivery.DeliveryPlanStatusConfirmed {
		return node, apperrors.ErrConflict
	}
	node.Status, node.Summary = "completed", "配置已采用，尚未部署"
	return node, nil
}

func (s *Service) templateAdoptionScopes(ctx context.Context, p domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot) ([]map[string]string, error) {
	if s.templateUpdates == nil {
		return nil, apperrors.ErrConflict
	}
	frozen, err := s.templateUpdates.ReadCandidate(ctx, p, snapshot.Target.TemplateCandidateID)
	if err != nil {
		return nil, err
	}
	current := deliveryTargetScope(frozen.Binding.Application, snapshot.Target, nil)
	if frozen.Candidate.Target.Scope == "application" && len(frozen.Binding.Environments) > 0 {
		scopes := make([]map[string]string, 0, len(frozen.Binding.Environments))
		for _, env := range frozen.Binding.Environments {
			target := snapshot.Target
			target.ApplicationEnvironmentID = env.ID
			scopes = append(scopes, deliveryTargetScope(frozen.Binding.Application, target, nil))
		}
		return scopes, nil
	}
	return []map[string]string{current}, nil
}
