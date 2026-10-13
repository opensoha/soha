package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"time"
)

func (s *Service) ConfigurePersonalGuest(authorize func(context.Context, guest.Binding) error, checkpoint func(context.Context, guest.DataPlan, string, guest.DataResult) error, transaction func(context.Context, func(context.Context) error) error) {
	s.personalGuestAuthorize, s.personalGuestCheckpoint, s.personalGuestTransaction = authorize, checkpoint, transaction
}

func (s *Service) AuthorizePersonalGuestMutation(ctx context.Context, principal identity.Principal, binding guest.Binding) error {
	if principal.UserID != binding.OwnerID {
		return apperrors.ErrAccessDenied
	}
	if _, _, err := s.accessVM(ctx, principal, binding.ComputeRef, appaccess.PermVirtualizationVMsResize); err != nil {
		return err
	}
	if _, _, err := s.accessVM(ctx, principal, binding.ComputeRef, vmConnectPermission); err != nil {
		return err
	}
	return s.ValidatePersonalGuestVM(ctx, principal, binding)
}

func (s *Service) QueuePersonalGuest(ctx context.Context, principal identity.Principal, bundle guest.Provisioning) (domain.Task, error) {
	if s.personalGuestAuthorize == nil || s.personalGuestCheckpoint == nil || s.personalGuestTransaction == nil || bundle.Plan.Validate(time.Now()) != nil {
		return domain.Task{}, apperrors.ErrServiceUnavailable
	}
	if err := s.AuthorizePersonalGuestMutation(ctx, principal, bundle.Plan.Binding); err != nil {
		return domain.Task{}, err
	}
	if err := s.personalGuestAuthorize(ctx, bundle.Plan.Binding); err != nil {
		return domain.Task{}, err
	}
	vm, connection, err := s.accessVM(ctx, principal, bundle.Plan.Binding.ComputeRef, vmConnectPermission)
	if err != nil {
		return domain.Task{}, err
	}
	adapter, _, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		return domain.Task{}, err
	}
	if _, ok := adapter.(domain.PersonalGuestProvisioner); !ok {
		return domain.Task{}, apperrors.ErrUnsupportedOperation
	}
	payload := map[string]any{"executionActorId": principal.UserID, "executionTokenId": principal.AccessTokenID, "personalGuestPlan": bundle.Plan}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return domain.Task{}, err
	}
	if err := s.sealVMBootstrap(payload, string(raw)); err != nil {
		return domain.Task{}, err
	}
	task := domain.Task{ID: bundle.Plan.OperationID, Provider: vm.Provider, ConnectionID: connection.ID, VMID: vm.ID, TaskKind: TaskKindPersonalGuest, Status: TaskStatusQueued, RequestedBy: principal.UserID, TimeoutSeconds: 900, MaxRetries: 1, Payload: payload}
	if err := domain.CheckScope(ctx, taskCapabilityScope(task)); err != nil {
		return domain.Task{}, err
	}
	created, err := s.savePersonalGuestTask(ctx, principal, task, bundle.Plan)
	if err != nil {
		return domain.Task{}, err
	}
	if err := s.auditPersonalGuest(ctx, task, "queue"); err != nil {
		return domain.Task{}, err
	}
	return domain.WithOperationState(created, time.Now()), nil
}

func (s *Service) savePersonalGuestTask(ctx context.Context, principal identity.Principal, task domain.Task, plan guest.DataPlan) (domain.Task, error) {
	if previous, getErr := s.tasks.GetTask(ctx, task.ID); getErr == nil {
		if err := s.AuthorizePersonalGuestReconciliation(ctx, principal, task.ID, task.VMID); err != nil {
			return domain.Task{}, err
		}
		oldPlan, planErr := decodePersonalGuestPlan(previous)
		if planErr != nil || !oldPlan.Binding.SameWorkspace(plan.Binding) || oldPlan.DiskQuotaBytes != plan.DiskQuotaBytes {
			return domain.Task{}, apperrors.ErrConflict
		}
		payload := task.Payload
		task = previous
		task.Payload = payload
		task.Status, task.ClaimedByWorkerID = TaskStatusQueued, ""
		task.StartedAt, task.FinishedAt, task.LastHeartbeatAt = nil, nil, nil
		task.MaxRetries = 2
		task.Result = map[string]any{"previousAttempt": previous.Result, "message": "explicit reconciliation of the original operation; data format cannot repeat"}
		return s.tasks.UpdateTask(ctx, task)
	} else if errors.Is(mapNotFound(getErr), apperrors.ErrNotFound) {
		return s.tasks.CreateTask(ctx, task)
	} else {
		return domain.Task{}, getErr
	}
}

func (s *Service) auditPersonalGuest(ctx context.Context, task domain.Task, phase string) error {
	if s.audit == nil {
		return apperrors.ErrServiceUnavailable
	}
	return s.audit.Record(ctx, domainaudit.Entry{ActorID: task.RequestedBy, ResourceKind: "VirtualMachine", ResourceName: task.VMID, Action: "personal_guest.provision." + phase, Result: "success", Summary: "Managed personal guest provisioning " + phase, Metadata: map[string]any{"operationId": task.ID, "computeRef": task.VMID}})
}

func personalGuestPlan(task domain.Task) (guest.DataPlan, error) {
	plan, err := decodePersonalGuestPlan(task)
	if err != nil || plan.Validate(time.Now()) != nil {
		return plan, apperrors.ErrConflict
	}
	return plan, nil
}

func decodePersonalGuestPlan(task domain.Task) (guest.DataPlan, error) {
	var plan guest.DataPlan
	raw, err := json.Marshal(task.Payload["personalGuestPlan"])
	if err != nil || json.Unmarshal(raw, &plan) != nil || plan.Validate(plan.Binding.IssuedAt) != nil || task.ID != plan.OperationID || task.VMID != plan.Binding.ComputeRef || task.RequestedBy != plan.Binding.OwnerID || task.Provider != plan.Binding.Provider {
		return plan, apperrors.ErrConflict
	}
	return plan, nil
}

func (s *Service) AuthorizePersonalGuestReconciliation(ctx context.Context, principal identity.Principal, operationID, vmID string) error {
	task, err := s.tasks.GetTask(ctx, operationID)
	if err != nil {
		return err
	}
	if task.TaskKind != TaskKindPersonalGuest || task.RequestedBy != principal.UserID || task.VMID != vmID || task.AttemptCount >= 3 || (task.Status != TaskStatusFailed && task.Status != TaskStatusTimeout) {
		return apperrors.ErrConflict
	}
	plan, err := decodePersonalGuestPlan(task)
	if err != nil {
		return err
	}
	if err := domain.CheckScope(ctx, taskCapabilityScope(task)); err != nil {
		return err
	}
	return s.AuthorizePersonalGuestMutation(ctx, principal, plan.Binding)
}

func (s *Service) authorizePersonalGuestTask(ctx context.Context, task domain.Task) (guest.DataPlan, error) {
	plan, err := personalGuestPlan(task)
	if err != nil {
		return plan, err
	}
	if s.executionPrincipals == nil || s.personalGuestAuthorize == nil {
		return plan, apperrors.ErrAccessDenied
	}
	principal, err := s.executionPrincipals.CurrentExecutionPrincipal(ctx, payloadString(task.Payload, "executionActorId"), payloadString(task.Payload, "executionTokenId"))
	if err != nil {
		return plan, apperrors.ErrAccessDenied
	}
	if err := s.AuthorizePersonalGuestMutation(ctx, principal, plan.Binding); err != nil {
		return plan, err
	}
	if err := domain.CheckScope(ctx, taskCapabilityScope(task)); err != nil {
		return plan, err
	}
	return plan, s.personalGuestAuthorize(ctx, plan.Binding)
}

func (s *Service) PersonalGuestDataGrant(ctx context.Context, binding guest.Binding, operationID string) (guest.DataPlan, error) {
	task, err := s.tasks.GetTask(ctx, operationID)
	if err != nil {
		return guest.DataPlan{}, err
	}
	if task.TaskKind != TaskKindPersonalGuest || task.Status != TaskStatusRunning || !boolValue(task.Payload, "providerDispatchStarted") {
		return guest.DataPlan{}, apperrors.ErrAccessDenied
	}
	plan, err := s.authorizePersonalGuestTask(ctx, task)
	if err != nil {
		return plan, err
	}
	if binding != plan.Binding {
		return plan, apperrors.ErrAccessDenied
	}
	return plan, nil
}

func (s *Service) executePersonalGuest(ctx context.Context, task domain.Task) (string, error) {
	plan, err := s.authorizePersonalGuestTask(ctx, task)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	if boolValue(task.Payload, "providerDispatchStarted") {
		err := apperrors.ErrConflict
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	content, err := s.vmBootstrapContent(task.Payload)
	var bundle guest.Provisioning
	if err != nil || json.Unmarshal([]byte(content), &bundle) != nil || bundle.Plan != plan {
		err := apperrors.ErrConflict
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	vm, err := s.vms.GetVM(ctx, task.VMID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil || vm.ConnectionID != task.ConnectionID {
		err := apperrors.ErrConflict
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	adapter, adapterConnection, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	provisioner, ok := adapter.(domain.PersonalGuestProvisioner)
	if !ok {
		err := apperrors.ErrUnsupportedOperation
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	err = s.personalGuestTransaction(ctx, func(txCtx context.Context) error {
		task.Payload["providerDispatchStarted"] = true
		updated, err := s.tasks.UpdateTask(txCtx, task)
		if err != nil {
			return err
		}
		if err := s.personalGuestCheckpoint(txCtx, plan, "dispatched", guest.DataResult{}); err != nil {
			return err
		}
		if err := s.auditPersonalGuest(txCtx, task, "dispatch"); err != nil {
			return err
		}
		task = updated
		return nil
	})
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	result, err := provisioner.ProvisionPersonalGuest(ctx, adapterConnection, domain.AdapterVM{ID: firstNonEmpty(vm.ExternalID, vm.ID), Node: vm.NodeName, Name: vm.Name}, bundle)
	if err != nil {
		task.Result = personalGuestFailureResult(err)
		_, _ = s.tasks.UpdateTask(ctx, task)
		s.failTask(ctx, task, apperrors.ErrConflict)
		return runtimeobs.OutcomeFailed, apperrors.ErrConflict
	}
	if _, err := s.authorizePersonalGuestTask(ctx, task); err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	err = s.personalGuestTransaction(ctx, func(txCtx context.Context) error {
		if err := s.personalGuestCheckpoint(txCtx, plan, "complete", result); err != nil {
			return err
		}
		task.Result = map[string]any{"providerEffect": "initialized", "data": result, "message": "data initialized; tool execution readiness is checked separately"}
		if err := s.auditPersonalGuest(txCtx, task, "complete"); err != nil {
			return err
		}
		_, err := s.completeTask(txCtx, task)
		return err
	})
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	return runtimeobs.OutcomeSucceeded, nil
}

func personalGuestFailureResult(err error) map[string]any {
	result := map[string]any{"providerEffect": "unknown", "message": "guest provisioning outcome requires inspection; retain the original operation"}
	var diagnostic interface{ ProvisioningStage() string }
	if errors.As(err, &diagnostic) {
		result["stage"] = diagnostic.ProvisioningStage()
	}
	return result
}
