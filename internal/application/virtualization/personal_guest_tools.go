package virtualization

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

const TaskKindPersonalGuestTools = "personal_guest_tools"

func (s *Service) ConfigurePersonalGuestTools(checkpoint func(context.Context, guest.ToolsPlan) error) {
	s.personalGuestToolsCheckpoint = checkpoint
}

func (s *Service) FindPersonalGuestTools(ctx context.Context, principal identity.Principal, id string, version int64) (domain.Task, error) {
	task, err := s.tasks.GetTask(ctx, id)
	if err != nil {
		return task, mapNotFound(err)
	}
	if task.TaskKind != TaskKindPersonalGuestTools || task.RequestedBy != principal.UserID {
		return task, apperrors.ErrAccessDenied
	}
	if payloadString(task.Payload, "environmentVersion") != strconv.FormatInt(version, 10) {
		return task, apperrors.ErrConflict
	}
	plan, err := decodeGuestToolsPlan(task)
	if err == nil {
		err = s.AuthorizePersonalGuestMutation(ctx, principal, plan.Binding)
	}
	if err == nil {
		err = domain.CheckScope(ctx, taskCapabilityScope(task))
	}
	return domain.WithOperationState(task, time.Now()), err
}

func (s *Service) QueuePersonalGuestTools(ctx context.Context, principal identity.Principal, plan guest.ToolsPlan, version int64) (domain.Task, error) {
	if plan.Validate(time.Now()) != nil || s.personalGuestToolsCheckpoint == nil || s.personalGuestTransaction == nil || s.personalGuestAuthorize == nil {
		return domain.Task{}, apperrors.ErrServiceUnavailable
	}
	if err := s.AuthorizePersonalGuestMutation(ctx, principal, plan.Binding); err != nil {
		return domain.Task{}, err
	}
	if err := s.personalGuestAuthorize(ctx, plan.Binding); err != nil {
		return domain.Task{}, err
	}
	vm, connection, err := s.accessVM(ctx, principal, plan.Binding.ComputeRef, vmConnectPermission)
	if err != nil {
		return domain.Task{}, err
	}
	task := domain.Task{ID: plan.OperationID, Provider: vm.Provider, ConnectionID: connection.ID, VMID: vm.ID, TaskKind: TaskKindPersonalGuestTools, Status: TaskStatusQueued, RequestedBy: principal.UserID, TimeoutSeconds: 900, MaxRetries: 1, Payload: map[string]any{"executionActorId": principal.UserID, "executionTokenId": principal.AccessTokenID, "personalGuestToolsPlan": plan, "environmentVersion": strconv.FormatInt(version, 10)}}
	if err := domain.CheckScope(ctx, taskCapabilityScope(task)); err != nil {
		return domain.Task{}, err
	}
	created, err := s.tasks.CreateTask(ctx, task)
	if err != nil {
		return domain.Task{}, err
	}
	if err := s.auditGuestTools(ctx, task, "queue"); err != nil {
		return domain.Task{}, err
	}
	return domain.WithOperationState(created, time.Now()), nil
}

func decodeGuestToolsPlan(task domain.Task) (guest.ToolsPlan, error) {
	var plan guest.ToolsPlan
	raw, err := json.Marshal(task.Payload["personalGuestToolsPlan"])
	if err != nil || json.Unmarshal(raw, &plan) != nil || plan.Validate(plan.Binding.IssuedAt) != nil || plan.OperationID != task.ID || plan.Binding.ComputeRef != task.VMID || plan.Binding.OwnerID != task.RequestedBy || plan.Binding.Provider != task.Provider {
		return plan, apperrors.ErrConflict
	}
	return plan, nil
}

func (s *Service) authorizeGuestTools(ctx context.Context, task domain.Task) (guest.ToolsPlan, error) {
	plan, err := decodeGuestToolsPlan(task)
	if err != nil || plan.Validate(time.Now()) != nil {
		return plan, apperrors.ErrConflict
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

func (s *Service) PersonalGuestToolsGrant(ctx context.Context, binding guest.Binding, id string) (guest.ToolsPlan, error) {
	task, err := s.tasks.GetTask(ctx, id)
	if err != nil {
		return guest.ToolsPlan{}, err
	}
	if task.TaskKind != TaskKindPersonalGuestTools || task.Status != TaskStatusRunning || !boolValue(task.Payload, "providerDispatchStarted") {
		return guest.ToolsPlan{}, apperrors.ErrAccessDenied
	}
	plan, err := s.authorizeGuestTools(ctx, task)
	if err == nil && !binding.SameWorkspace(plan.Binding) {
		err = apperrors.ErrAccessDenied
	}
	return plan, err
}

func (s *Service) executeGuestTools(ctx context.Context, task domain.Task) (string, error) {
	err := s.dispatchGuestTools(ctx, task)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	return runtimeobs.OutcomeSucceeded, nil
}

func (s *Service) dispatchGuestTools(ctx context.Context, task domain.Task) error {
	plan, err := s.authorizeGuestTools(ctx, task)
	if err != nil {
		return err
	}
	if boolValue(task.Payload, "providerDispatchStarted") || s.personalGuestToolsCheckpoint == nil {
		return apperrors.ErrConflict
	}
	vm, err := s.vms.GetVM(ctx, task.VMID)
	if err != nil {
		return err
	}
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil || vm.ConnectionID != task.ConnectionID {
		return apperrors.ErrConflict
	}
	adapter, access, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		return err
	}
	installer, ok := adapter.(domain.PersonalGuestToolsInstaller)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	err = s.personalGuestTransaction(ctx, func(txCtx context.Context) error {
		task.Payload["providerDispatchStarted"] = true
		updated, err := s.tasks.UpdateTask(txCtx, task)
		if err != nil {
			return err
		}
		task = updated
		return s.auditGuestTools(txCtx, task, "dispatch")
	})
	if err != nil {
		return err
	}
	result, err := installer.InstallPersonalGuestTools(ctx, access, domain.AdapterVM{ID: firstNonEmpty(vm.ExternalID, vm.ID), Node: vm.NodeName, Name: vm.Name}, plan)
	if err != nil {
		return err
	}
	if result.OperationID != plan.OperationID || result.DataRef != plan.Binding.DataRef || result.ArtifactSHA256 != plan.ArtifactSHA256 {
		return apperrors.ErrConflict
	}
	return s.personalGuestTransaction(ctx, func(txCtx context.Context) error {
		if err := s.personalGuestToolsCheckpoint(txCtx, plan); err != nil {
			return err
		}
		task.Result = map[string]any{"installed": true, "dataRef": result.DataRef, "artifactSHA256": result.ArtifactSHA256, "executionReadiness": "pending"}
		if _, err := s.completeTask(txCtx, task); err != nil {
			return err
		}
		return s.auditGuestTools(txCtx, task, "complete")
	})
}

func (s *Service) auditGuestTools(ctx context.Context, task domain.Task, phase string) error {
	if s.audit == nil {
		return apperrors.ErrServiceUnavailable
	}
	return s.audit.Record(ctx, domainaudit.Entry{ActorID: task.RequestedBy, ResourceKind: "VirtualMachine", ResourceName: task.VMID, Action: "personal_guest.tools." + phase, Result: "success", Summary: "Managed personal guest tools " + phase, Metadata: map[string]any{"operationId": task.ID, "computeRef": task.VMID}})
}
