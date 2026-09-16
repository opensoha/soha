package virtualization

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func connectionCapabilityScope(connection domain.Connection, namespace string) map[string]string {
	scope := map[string]string{"virtualizationConnectionId": connection.ID, "connectionRevision": vmCreateConnectionIdentity(connection)}
	if connection.Provider == ProviderKubeVirt {
		scope["namespace"] = firstNonEmpty(namespace, connection.DefaultNamespace, stringValue(connection.Config, "namespace"), "default")
		if connection.KubernetesClusterID != "" {
			scope["clusterId"] = connection.KubernetesClusterID
		}
	}
	return scope
}

func taskCapabilityScope(task domain.Task) map[string]string {
	scope := map[string]string{"operationId": task.ID, "virtualizationConnectionId": task.ConnectionID}
	if isWorkerCreation(task) {
		if pool, err := workerPoolFromTask(task); err == nil {
			for key, value := range workerPoolScope(pool) {
				scope[key] = value
			}
		}
	}
	if task.VMID != "" && task.TaskKind != TaskKindVMCreate {
		scope["vmId"] = task.VMID
	}
	if namespace := stringValue(task.Payload, "namespace"); namespace != "" {
		scope["namespace"] = namespace
	}
	return scope
}

// ToolInvocationScopes resolves local domain records before Gateway policy evaluation.
func (s *Service) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) ([]map[string]string, error) {
	for _, permission := range tool.PermissionKeys {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return nil, err
		}
	}
	switch tool.Name {
	case "virtualization.worker_pools.list", "virtualization.worker_pools.get", "virtualization.workers.create", "virtualization.workers.assess":
		return s.workerToolScopes(ctx, principal, tool.Name, input)
	case "virtualization.capacity.check":
		connection, err := s.connections.GetConnection(ctx, strings.TrimSpace(stringValue(input, "connectionId")))
		if err != nil {
			return nil, mapNotFound(err)
		}
		return []map[string]string{connectionCapabilityScope(connection, strings.TrimSpace(stringValue(input, "namespace")))}, nil
	case "virtualization.vms.create.plan", "virtualization.vms.create.trigger":
		var request CreateVMInput
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		prepared, err := s.prepareVMCreate(ctx, request)
		if err != nil {
			return nil, err
		}
		return []map[string]string{connectionCapabilityScope(prepared.connection, prepared.input.Namespace)}, nil
	case "virtualization.operations.get", "virtualization.operations.cancel", "virtualization.operations.retry":
		task, err := s.tasks.GetTask(ctx, strings.TrimSpace(stringValue(input, "operationId")))
		if err != nil {
			return nil, mapNotFound(err)
		}
		if task.TaskKind != TaskKindVMCreate && task.TaskKind != TaskKindVMAction {
			return nil, apperrors.ErrUnsupportedOperation
		}
		return []map[string]string{taskCapabilityScope(task)}, nil
	default:
		return nil, apperrors.ErrUnsupportedOperation
	}
}

// FindVMCreation reads the original actor/key receipt. It never contacts a provider
// or enqueues work, and changed normalized input cannot adopt a previous request.
func (s *Service) FindVMCreation(ctx context.Context, principal domainidentity.Principal, input CreateVMInput) (domain.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationVMsCreate); err != nil {
		return domain.Task{}, err
	}
	prepared, err := s.prepareVMCreate(ctx, input)
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.CheckScope(ctx, connectionCapabilityScope(prepared.connection, prepared.input.Namespace)); err != nil {
		return domain.Task{}, err
	}
	item, found, err := s.existingIdempotentTask(ctx, "virtualization.vm.create", principal, input.IdempotencyKey, prepared.input)
	if err != nil {
		return domain.Task{}, err
	}
	if !found {
		return domain.Task{}, apperrors.ErrNotFound
	}
	return domain.WithOperationState(item, time.Now().UTC()), nil
}

func (s *Service) authorizeVMRetry(ctx context.Context, principal domainidentity.Principal, task domain.Task) error {
	if isWorkerCreation(task) {
		if err := s.authorizeWorkerCreate(ctx, principal); err != nil {
			return err
		}
		pool, err := workerPoolFromTask(task)
		if err != nil {
			return err
		}
		return s.checkWorkerClusterAccess(ctx, principal, pool.Spec.ClusterID, true)
	}
	switch task.TaskKind {
	case TaskKindVMCreate:
		return s.authorize(ctx, principal, appaccess.PermVirtualizationVMsCreate)
	case TaskKindVMAction:
		action, err := normalizeAction(stringValue(task.Payload, "action"))
		if err != nil {
			return err
		}
		return s.authorize(ctx, principal, vmActionPermission(action))
	default:
		return nil
	}
}

// FindOperationMutation reads the original mutation receipt without repeating it.
func (s *Service) FindOperationMutation(ctx context.Context, principal domainidentity.Principal, id, action string, input OperationMutationInput) (domain.Task, error) {
	if action != "cancel" && action != "retry" {
		return domain.Task{}, apperrors.ErrUnsupportedOperation
	}
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, action)); err != nil {
		return domain.Task{}, err
	}
	task, err := s.GetOperation(ctx, principal, id)
	if err != nil {
		return domain.Task{}, err
	}
	if action == "retry" {
		if err := s.authorizeVMRetry(ctx, principal, task); err != nil {
			return domain.Task{}, err
		}
	}
	_, _, found, err := operationMutationReceipt(task.Payload, "virtualization.operation."+action+"/"+task.ID, principal, input)
	if err != nil {
		return domain.Task{}, err
	}
	if !found {
		return domain.Task{}, apperrors.ErrNotFound
	}
	return task, nil
}
