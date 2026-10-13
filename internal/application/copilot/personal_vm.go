package copilot

import (
	"context"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) authorizePersonalEnvironmentPolicy(ctx context.Context, principal identity.Principal, policy domain.PersonalEnvironmentPolicy) error {
	if policy.Backend != "vm" {
		return s.personalEnvironment.Authorize(ctx, principal, policy.AgentClusterID)
	}
	_, err := s.observePersonalVMPolicy(ctx, principal, policy)
	return err
}

func (s *Service) observePersonalVMPolicy(ctx context.Context, principal identity.Principal, policy domain.PersonalEnvironmentPolicy) (domain.PersonalVMIdentity, error) {
	if policy.VM == nil || policy.VM.OwnerID != principal.UserID {
		return domain.PersonalVMIdentity{}, apperrors.ErrAccessDenied
	}
	if policy.VM.Validate() != nil || policy.Provider != policy.VM.Provider || policy.AgentClusterID != "" || policy.LeaseSeconds < 60 || policy.LeaseSeconds > 3600 || policy.Quota.ValidatePersonalDocker() != nil || policy.Quota.DiskBytes > policy.VM.DataDiskBytes {
		return domain.PersonalVMIdentity{}, apperrors.ErrInvalidArgument
	}
	if s.personalEnvironment.ObserveVM == nil {
		return domain.PersonalVMIdentity{}, apperrors.ErrServiceUnavailable
	}
	proof, err := s.personalEnvironment.ObserveVM(ctx, principal, *policy.VM)
	if err != nil {
		return domain.PersonalVMIdentity{}, err
	}
	if proof.Validate() != nil || proof.DataDiskSerial != policy.VM.DataDiskSerial || proof.DataDiskBytes != policy.VM.DataDiskBytes {
		return domain.PersonalVMIdentity{}, apperrors.ErrConflict
	}
	return proof, nil
}

func (s *Service) bindPersonalVMEnvironment(ctx context.Context, principal identity.Principal, assistant domain.PersonalAssistant, templateID string, policy domain.PersonalEnvironmentPolicy) (domain.PersonalEnvironmentRecord, error) {
	if policy.TemplateID != templateID || !domain.ValidPersonalVMPolicyRevision(policy.PolicyRevision) {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	if s.personalEnvironment.Transaction == nil || isNilDependency(s.audits) {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrServiceUnavailable
	}
	proof, err := s.observePersonalVMPolicy(ctx, principal, policy)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	now := time.Now().UTC()
	lease := personalEnvironmentLease(assistant.ID, principal.UserID, policy, now)
	if (contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "acquire", Lease: lease}).Validate() != nil {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	candidate := domain.PersonalEnvironmentRecord{AssistantID: assistant.ID, CreatedBy: principal.UserID, TemplateID: templateID,
		Backend: "vm", Provider: policy.Provider, PolicyRevision: policy.PolicyRevision, ComputeRef: policy.VM.ComputeRef, ComputeGeneration: 1,
		DataRef: uuid.NewString(), VMIdentity: &proof, LeaseSeconds: policy.LeaseSeconds, Lease: lease, UpdatedAt: now}
	var item domain.PersonalEnvironmentRecord
	err = s.personalEnvironment.Transaction(ctx, func(txCtx context.Context) error {
		var bindErr error
		item, bindErr = s.personalEnvironment.Store.BindPersonalEnvironment(txCtx, candidate)
		if bindErr != nil {
			return bindErr
		}
		return s.audits.Record(txCtx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, ResourceKind: "PersonalEnvironment", ResourceName: assistant.ID,
			Action: "personal_environment.bind_vm", Result: "pending", Summary: "Assigned personal VM; guest and data readiness pending",
			Metadata: map[string]any{"templateId": item.TemplateID, "computeRef": item.ComputeRef, "computeGeneration": item.ComputeGeneration, "dataRef": item.DataRef, "policyRevision": item.PolicyRevision}})
	})
	return item, err
}

func (s *Service) validatePersonalVMBinding(ctx context.Context, principal identity.Principal, item domain.PersonalEnvironmentRecord) error {
	if item.VMIdentity == nil || item.ComputeGeneration < 1 || item.State == "deleted" || item.State == "deleting" || item.State == "deletion_unknown" {
		return apperrors.ErrServiceUnavailable
	}
	p := domain.PersonalEnvironmentPolicy{Backend: "vm", Provider: item.Provider, LeaseSeconds: item.LeaseSeconds, Quota: item.Lease.Quota,
		VM: &domain.PersonalVMPolicy{OwnerID: item.CreatedBy, ComputeRef: item.ComputeRef, Provider: item.Provider, DataDiskSerial: item.VMIdentity.DataDiskSerial, DataDiskBytes: item.VMIdentity.DataDiskBytes}}
	proof, err := s.observePersonalVMPolicy(ctx, principal, p)
	if err == nil && proof != *item.VMIdentity {
		err = apperrors.ErrConflict
	}
	return err
}

func (s *Service) changePersonalVM(ctx context.Context, principal identity.Principal, item domain.PersonalEnvironmentRecord, action string) (domain.PersonalEnvironment, error) {
	if action == "sleep" {
		return domain.PersonalEnvironment{}, apperrors.ErrUnsupportedOperation
	}
	if !item.DataInitialized {
		return personalEnvironmentView(item), nil
	}
	if item.OperationID != "" || item.ActiveTaskID != "" {
		if action != "reconcile" || item.OperationUntil == nil || item.OperationUntil.After(time.Now()) || s.personalEnvironment.RecoverVM == nil {
			return personalEnvironmentView(item), apperrors.ErrConflict
		}
		var err error
		item, err = s.personalEnvironment.RecoverVM(ctx, principal, item)
		if err != nil {
			return personalEnvironmentView(item), err
		}
	}
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "inspect", Lease: item.Lease}
	if !request.Lease.ExpiresAt.After(time.Now().Add(time.Minute)) {
		if item.ActiveTaskID != "" || item.OperationID != "" {
			return personalEnvironmentView(item), apperrors.ErrConflict
		}
		now := time.Now().UTC()
		id := uuid.NewString()
		request.Lease.ID, request.Lease.RunID, request.Lease.IssuedAt, request.Lease.ExpiresAt = id, id, now, now.Add(time.Duration(item.LeaseSeconds)*time.Second)
	}
	view, _, err := s.dispatchPersonalEnvironment(ctx, principal, action, item, request)
	return view, err
}
