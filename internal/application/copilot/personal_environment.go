package copilot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type PersonalEnvironmentStore interface {
	GetPersonalEnvironment(context.Context, string, string) (domain.PersonalEnvironmentRecord, error)
	BindPersonalEnvironment(context.Context, domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error)
	ReservePersonalEnvironment(context.Context, domain.PersonalEnvironmentRecord, contractenv.Lease, string, bool) (domain.PersonalEnvironmentRecord, error)
	CompletePersonalEnvironment(context.Context, domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error)
	AbortPersonalEnvironment(context.Context, domain.PersonalEnvironmentRecord, domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error)
	ReservePersonalDataDeletion(context.Context, domain.PersonalEnvironmentRecord, string, string) (domain.PersonalEnvironmentRecord, error)
	GetPersonalCommand(context.Context, string, string) (domain.PersonalCommandRecord, error)
	ReservePersonalCommand(context.Context, domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord) (domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord, bool, error)
	ReservePersonalCommandCancellation(context.Context, domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord, string) (domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord, error)
	CompletePersonalCommand(context.Context, domain.PersonalEnvironmentRecord, domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord, bool) (domain.PersonalCommandRecord, error)
}

// Optional at startup. No personal environment is inferred from provider health.
type PersonalEnvironmentDependencies struct {
	Store       PersonalEnvironmentStore
	Policy      func(context.Context, string) (domain.PersonalEnvironmentPolicy, error)
	Templates   func(context.Context) ([]domain.PersonalEnvironmentPolicy, error)
	Preflight   func(context.Context, domainidentity.Principal, string, contractenv.Quota) error
	Authorize   func(context.Context, domainidentity.Principal, string) error
	Invoke      func(context.Context, domainidentity.Principal, string, contractenv.Request) (contractenv.Result, error)
	InvokeVM    func(context.Context, domainidentity.Principal, domain.PersonalEnvironmentRecord, guest.RuntimeOperation) (guest.RuntimeResult, error)
	RecoverVM   func(context.Context, domainidentity.Principal, domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error)
	Export      func(context.Context, domainidentity.Principal, string, contractenv.Request, io.Writer) (contractenv.ExportResult, error)
	ObserveVM   func(context.Context, domainidentity.Principal, domain.PersonalVMPolicy) (domain.PersonalVMIdentity, error)
	Transaction func(context.Context, func(context.Context) error) error
}

func (s *Service) ListPersonalEnvironmentTemplates(ctx context.Context, principal domainidentity.Principal) ([]domain.PersonalEnvironmentTemplate, error) {
	if _, err := s.EnsurePersonalAssistant(ctx, principal, "en-US"); err != nil {
		return nil, err
	}
	deps := s.personalEnvironment
	if deps == nil || deps.Templates == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	policies, err := deps.Templates(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]domain.PersonalEnvironmentTemplate, 0)
	for _, p := range policies {
		if err := s.authorizePersonalEnvironmentPolicy(ctx, principal, p); err != nil {
			if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrServiceUnavailable) || (p.Backend == "vm" && errors.Is(err, apperrors.ErrConflict)) {
				continue
			}
			return nil, err
		}
		if p.Backend != "vm" {
			p, err = personalDockerPolicy(p)
			if err != nil {
				continue
			}
		}
		items = append(items, domain.PersonalEnvironmentTemplate{ID: p.TemplateID, Name: p.Name, Backend: p.Backend, Provider: p.Provider, PolicyRevision: p.PolicyRevision, Mode: p.Mode, LeaseSeconds: p.LeaseSeconds, Quota: p.Quota})
		if len(items) == 100 {
			break
		}
	}
	return items, nil
}

func validatePersonalEnvironmentDependencies(deps *PersonalEnvironmentDependencies) error {
	if deps != nil && (isNilDependency(deps.Store) || deps.Policy == nil || deps.Preflight == nil || deps.Authorize == nil || deps.Invoke == nil) {
		return fmt.Errorf("copilot personal environment dependencies must be complete")
	}
	return nil
}

func (s *Service) GetPersonalEnvironment(ctx context.Context, principal domainidentity.Principal, locale string) (domain.PersonalEnvironment, error) {
	assistant, err := s.EnsurePersonalAssistant(ctx, principal, locale)
	if err != nil {
		return domain.PersonalEnvironment{}, err
	}
	if s.personalEnvironment == nil {
		return domain.PersonalEnvironment{AssistantID: assistant.ID, State: "unprovisioned"}, nil
	}
	item, err := s.personalEnvironment.Store.GetPersonalEnvironment(ctx, principal.UserID, assistant.ID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return domain.PersonalEnvironment{AssistantID: assistant.ID, State: "unprovisioned"}, nil
	}
	return personalEnvironmentView(item), err
}

// Harness inspection is an explicit, budgeted tool call. An unbound assistant
// is reported from the owner record; a bound one requires a fresh Agent receipt.
func (s *Service) InspectPersonalEnvironment(ctx context.Context, principal domainidentity.Principal) (domain.PersonalEnvironment, error) {
	item, err := s.GetPersonalEnvironment(ctx, principal, "en-US")
	if err != nil || item.TemplateID == "" {
		return item, err
	}
	return s.ChangePersonalEnvironment(ctx, principal, "reconcile", "", "en-US")
}

func personalEnvironmentView(item domain.PersonalEnvironmentRecord) domain.PersonalEnvironment {
	state := item.State
	now := time.Now().UTC()
	if (state == "pending" && item.OperationUntil != nil && !item.OperationUntil.After(now)) || ((state == "ready" || state == "quota_exceeded") && !item.Lease.ExpiresAt.After(now)) {
		state = "unknown"
	}
	if state == "deleting" && item.OperationUntil != nil && !item.OperationUntil.After(now) {
		state = "deletion_unknown"
	}
	view := domain.PersonalEnvironment{AssistantID: item.AssistantID, TemplateID: item.TemplateID, State: state, Version: item.Version, DataInitialized: item.DataInitialized, Quota: &item.Lease.Quota, ObservedAt: item.ObservedAt, ExpiresAt: &item.Lease.ExpiresAt, UpdatedAt: &item.UpdatedAt}
	view.Backend, view.Provider, view.PolicyRevision = item.Backend, item.Provider, item.PolicyRevision
	view.ComputeRef, view.ComputeGeneration, view.DataRef = item.ComputeRef, item.ComputeGeneration, item.DataRef
	if state == "ready" || state == "quota_exceeded" {
		view.Usage = item.Usage
	}
	return view
}

func (s *Service) ChangePersonalEnvironment(ctx context.Context, principal domainidentity.Principal, action, templateID, locale string) (domain.PersonalEnvironment, error) {
	item, err := s.personalEnvironmentForOwner(ctx, principal, action, templateID, locale)
	if err != nil {
		return domain.PersonalEnvironment{}, err
	}
	if item.Backend == "vm" {
		return s.changePersonalVM(ctx, principal, item, action)
	}
	request, dispatch, err := personalEnvironmentRequest(item, action)
	if err != nil || !dispatch {
		return personalEnvironmentView(item), err
	}
	if request.CreateData {
		// A failed quota probe has no authority to create personal data and must
		// not consume the durable once-only personal acquisition reservation.
		if s.personalEnvironment.Preflight == nil || s.personalEnvironment.Preflight(ctx, principal, item.AgentClusterID, item.Lease.Quota) != nil {
			s.auditPersonalEnvironment(ctx, principal, item, "preflight", "unavailable")
			return personalEnvironmentView(item), fmt.Errorf("%w: personal Docker quota preflight is unavailable", apperrors.ErrServiceUnavailable)
		}
		s.auditPersonalEnvironment(ctx, principal, item, "preflight", "verified")
		// The first personal lease starts after the disposable probe. Probe time
		// must not consume the user's 60–3600 second execution lease.
		now := time.Now().UTC()
		id := uuid.NewString()
		request.Lease.ID, request.Lease.RunID = id, id
		request.Lease.IssuedAt, request.Lease.ExpiresAt = now, now.Add(time.Duration(item.LeaseSeconds)*time.Second)
	}
	view, _, err := s.dispatchPersonalEnvironment(ctx, principal, action, item, request)
	return view, err
}

func (s *Service) dispatchPersonalEnvironment(ctx context.Context, principal domainidentity.Principal, action string, item domain.PersonalEnvironmentRecord, request contractenv.Request) (domain.PersonalEnvironment, contractenv.Result, error) {
	reserved, err := s.personalEnvironment.Store.ReservePersonalEnvironment(ctx, item, request.Lease, uuid.NewString(), request.CreateData)
	if err != nil {
		return domain.PersonalEnvironment{}, contractenv.Result{}, err
	}
	return s.invokeReservedPersonalEnvironment(ctx, principal, action, item, reserved, request)
}

func (s *Service) invokeReservedPersonalEnvironment(ctx context.Context, principal domainidentity.Principal, action string, item, reserved domain.PersonalEnvironmentRecord, request contractenv.Request) (domain.PersonalEnvironment, contractenv.Result, error) {
	s.auditPersonalEnvironment(ctx, principal, reserved, action, "reserved")
	// Personal-data acquisition follows its durable reservation, without retries.
	result, invokeErr := s.invokePersonalRuntime(ctx, principal, reserved, request)
	if errors.Is(invokeErr, domain.ErrPersonalEnvironmentNotDispatched) {
		view, err := s.abortPersonalEnvironment(ctx, principal, action, reserved, item, invokeErr)
		return view, contractenv.Result{}, err
	}
	if invokeErr == nil {
		invokeErr = validatePersonalEnvironmentReceipt(result, request)
	}
	applyPersonalEnvironmentReceipt(&reserved, result, invokeErr)
	if reserved.Backend == "vm" && request.Action == "release" && invokeErr == nil {
		reserved.State, reserved.ActiveTaskID = "ready", ""
		reserved.Usage, reserved.ObservedAt = item.Usage, item.ObservedAt
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	completed, err := s.personalEnvironment.Store.CompletePersonalEnvironment(commitCtx, reserved)
	if err != nil {
		return domain.PersonalEnvironment{}, contractenv.Result{}, err
	}
	s.auditPersonalEnvironment(commitCtx, principal, completed, action, completed.State)
	if invokeErr != nil {
		return personalEnvironmentView(completed), contractenv.Result{}, fmt.Errorf("%w: personal environment outcome is unknown; reconcile the original lease", apperrors.ErrServiceUnavailable)
	}
	return personalEnvironmentView(completed), result, nil
}

func (s *Service) abortPersonalEnvironment(ctx context.Context, principal domainidentity.Principal, action string, reserved, previous domain.PersonalEnvironmentRecord, invokeErr error) (domain.PersonalEnvironment, error) {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	item, err := s.personalEnvironment.Store.AbortPersonalEnvironment(commitCtx, reserved, previous)
	if err != nil {
		return domain.PersonalEnvironment{}, err
	}
	s.auditPersonalEnvironment(commitCtx, principal, item, action, "not_dispatched")
	if errors.Is(invokeErr, apperrors.ErrAccessDenied) {
		return personalEnvironmentView(item), apperrors.ErrAccessDenied
	}
	return personalEnvironmentView(item), fmt.Errorf("%w: personal environment request was not dispatched", apperrors.ErrServiceUnavailable)
}

func (s *Service) personalEnvironmentForChange(ctx context.Context, principal domainidentity.Principal, action, templateID, locale string) (domain.PersonalEnvironmentRecord, error) {
	item, err := s.personalEnvironmentForOwner(ctx, principal, action, templateID, locale)
	if err == nil && item.Backend != "vm" && (item.Backend != "container" || item.Provider != "docker") {
		err = apperrors.ErrServiceUnavailable
	}
	return item, err
}

func (s *Service) invokePersonalRuntime(ctx context.Context, principal domainidentity.Principal, item domain.PersonalEnvironmentRecord, request contractenv.Request) (contractenv.Result, error) {
	if item.Backend == "vm" {
		if s.personalEnvironment.InvokeVM == nil {
			return contractenv.Result{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, apperrors.ErrServiceUnavailable)
		}
		result, err := s.personalEnvironment.InvokeVM(ctx, principal, item, guest.RuntimeOperation{Kind: "environment", Request: request})
		return result.Environment, err
	}
	return s.personalEnvironment.Invoke(ctx, principal, item.AgentClusterID, request)
}

func (s *Service) personalEnvironmentForOwner(ctx context.Context, principal domainidentity.Principal, action, templateID, locale string) (domain.PersonalEnvironmentRecord, error) {
	if err := validatePersonalEnvironmentAction(action, templateID); err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	assistant, err := s.EnsurePersonalAssistant(ctx, principal, locale)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	deps := s.personalEnvironment
	if deps == nil {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrServiceUnavailable
	}
	item, err := deps.Store.GetPersonalEnvironment(ctx, principal.UserID, assistant.ID)
	if errors.Is(err, apperrors.ErrNotFound) && action == "wake" && templateID != "" {
		item, err = s.bindPersonalEnvironment(ctx, principal, assistant, templateID)
	}
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	if item.CreatedBy != principal.UserID || item.AssistantID != assistant.ID || (templateID != "" && templateID != item.TemplateID) {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrAccessDenied
	}
	if item.Backend == "vm" {
		return item, s.validatePersonalVMBinding(ctx, principal, item)
	}
	if item.Backend != "container" || item.Provider != "docker" {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrServiceUnavailable
	}
	if err := deps.Authorize(ctx, principal, item.AgentClusterID); err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	return item, nil
}

func validatePersonalEnvironmentAction(action, templateID string) error {
	if (action != "wake" && action != "sleep" && action != "reconcile") || (action != "wake" && templateID != "") || len(templateID) > 128 || templateID != strings.TrimSpace(templateID) {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func (s *Service) bindPersonalEnvironment(ctx context.Context, principal domainidentity.Principal, assistant domain.PersonalAssistant, templateID string) (domain.PersonalEnvironmentRecord, error) {
	policy, err := s.personalEnvironment.Policy(ctx, templateID)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	if policy.Backend == "vm" {
		if policy.VM == nil {
			return domain.PersonalEnvironmentRecord{}, apperrors.ErrServiceUnavailable
		}
		return s.bindPersonalVMEnvironment(ctx, principal, assistant, templateID, policy)
	}
	policy, err = personalDockerPolicy(policy)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	if policy.TemplateID != templateID || policy.AgentClusterID == "" || policy.LeaseSeconds < 60 || policy.LeaseSeconds > 3600 {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	if err := s.personalEnvironment.Authorize(ctx, principal, policy.AgentClusterID); err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	now := time.Now().UTC()
	lease := personalEnvironmentLease(assistant.ID, principal.UserID, policy, now)
	if err := (contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "acquire", Lease: lease}).Validate(); err != nil {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	return s.personalEnvironment.Store.BindPersonalEnvironment(ctx, domain.PersonalEnvironmentRecord{AssistantID: assistant.ID, CreatedBy: principal.UserID, TemplateID: templateID, Backend: policy.Backend, Provider: policy.Provider, PolicyRevision: policy.PolicyRevision, DataRef: "personal-data:" + assistant.ID, AgentClusterID: policy.AgentClusterID, LeaseSeconds: policy.LeaseSeconds, Lease: lease, UpdatedAt: now})
}

func personalEnvironmentLease(assistant, owner string, policy domain.PersonalEnvironmentPolicy, now time.Time) contractenv.Lease {
	id := uuid.NewString()
	scope := sha256.Sum256([]byte("personal-environment:" + assistant + ":" + owner))
	return contractenv.Lease{SchemaVersion: contractenv.LeaseVersion, ID: id, RunID: id, OwnerID: assistant, ScopeHash: fmt.Sprintf("sha256:%x", scope), Mode: policy.Mode, Quota: policy.Quota, IssuedAt: now, ExpiresAt: now.Add(time.Duration(policy.LeaseSeconds) * time.Second)}
}

// Empty fields are the existing in-process Docker policy contract. An explicit
// unsupported backend must never reach Docker preflight or dispatch.
func personalDockerPolicy(policy domain.PersonalEnvironmentPolicy) (domain.PersonalEnvironmentPolicy, error) {
	if policy.Backend == "" {
		policy.Backend = "container"
	}
	if policy.Provider == "" {
		policy.Provider = "docker"
	}
	if policy.PolicyRevision == "" {
		policy.PolicyRevision = "legacy-docker-v1"
	}
	if policy.Backend != "container" || policy.Provider != "docker" {
		return policy, apperrors.ErrServiceUnavailable
	}
	return policy, nil
}

func personalEnvironmentRequest(item domain.PersonalEnvironmentRecord, action string) (contractenv.Request, bool, error) {
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Lease: item.Lease}
	now := time.Now().UTC()
	if item.OperationUntil != nil && item.OperationUntil.After(now) {
		return request, false, apperrors.ErrConflict
	}
	if action != "reconcile" && slices.Contains([]string{"deleting", "deletion_unknown", "deleted"}, item.State) {
		return request, false, fmt.Errorf("%w: personal data deletion cannot be undone by a lifecycle action", apperrors.ErrConflict)
	}
	switch action {
	case "reconcile":
		request.Action = "inspect"
	case "sleep":
		request.Action = "release"
		if item.State == "sleeping" {
			return request, false, nil
		}
	case "wake":
		if (item.State == "ready" || item.State == "quota_exceeded") && item.Lease.ExpiresAt.After(now) {
			return request, false, nil
		}
		if item.State == "unknown" || item.State == "pending" || ((item.State == "ready" || item.State == "quota_exceeded") && !item.Lease.ExpiresAt.After(now)) {
			return request, false, fmt.Errorf("%w: reconcile the original lease before waking", apperrors.ErrConflict)
		}
		request.Action = "acquire"
		request.CreateData = !item.DataCreationAttempted
		if item.DataCreationAttempted || !item.Lease.ExpiresAt.After(now) {
			id := uuid.NewString()
			request.Lease.ID, request.Lease.RunID = id, id
			request.Lease.IssuedAt, request.Lease.ExpiresAt = now, now.Add(time.Duration(item.LeaseSeconds)*time.Second)
		}
	}
	return request, true, request.Validate()
}

func (s *Service) auditPersonalEnvironment(ctx context.Context, principal domainidentity.Principal, item domain.PersonalEnvironmentRecord, action, result string) {
	if s.audits == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audits.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, ResourceKind: "PersonalEnvironment", ResourceName: item.AssistantID, Action: "personal_environment." + action, Result: result, Summary: "Personal environment " + action, RequestID: meta.RequestID, RequestPath: meta.Path, RequestMethod: meta.Method, SourceIP: meta.SourceIP, Metadata: map[string]any{"version": item.Version, "templateId": item.TemplateID}})
}

func validatePersonalEnvironmentReceipt(result contractenv.Result, request contractenv.Request) error {
	if err := result.ValidateFor(request); err != nil {
		return err
	}
	now := time.Now().UTC()
	if result.ObservedAt.Before(now.Add(-time.Minute)) || result.ObservedAt.After(now.Add(time.Minute)) {
		return fmt.Errorf("personal environment receipt is stale")
	}
	return nil
}

func applyPersonalEnvironmentReceipt(item *domain.PersonalEnvironmentRecord, result contractenv.Result, err error) {
	deleting := item.State == "deleting"
	item.Usage, item.ObservedAt = nil, nil
	if deleting && (err != nil || result.State != "deleted") {
		item.State = "deletion_unknown"
		return
	}
	if err != nil {
		item.State = "unknown"
		return
	}
	item.State, item.ObservedAt = result.State, &result.ObservedAt
	switch result.State {
	case "active":
		item.State = "ready"
		item.DataInitialized = true
		item.Usage = &result.Snapshot.ResourceUsage
	case "quota_exceeded":
		item.DataInitialized = true
		item.Usage = &result.Snapshot.ResourceUsage
	case "released":
		item.State = "sleeping"
	}
}
