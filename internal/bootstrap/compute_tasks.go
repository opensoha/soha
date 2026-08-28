package bootstrap

import (
	"context"

	appcompute "github.com/opensoha/soha/internal/application/compute"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

type computeVirtualizationTaskController struct{ service *appvirtualization.Service }

func (c computeVirtualizationTaskController) GetOperation(ctx context.Context, principal domainidentity.Principal, id string) (domainvirtualization.Task, error) {
	return c.service.GetOperation(ctx, principal, id)
}

func (c computeVirtualizationTaskController) ListOperationLogs(ctx context.Context, principal domainidentity.Principal, id string, limit int) ([]domainvirtualization.TaskLog, error) {
	return c.service.ListOperationLogs(ctx, principal, id, limit)
}

func (c computeVirtualizationTaskController) CancelOperation(ctx context.Context, principal domainidentity.Principal, id string, input appcompute.TaskMutationInput) (domainvirtualization.Task, error) {
	return c.service.CancelOperationIdempotent(ctx, principal, id, appvirtualization.OperationMutationInput{IdempotencyKey: input.IdempotencyKey, Reason: input.Reason})
}

func (c computeVirtualizationTaskController) RetryOperation(ctx context.Context, principal domainidentity.Principal, id string, input appcompute.TaskMutationInput) (domainvirtualization.Task, error) {
	return c.service.RetryOperationIdempotent(ctx, principal, id, appvirtualization.OperationMutationInput{IdempotencyKey: input.IdempotencyKey, Reason: input.Reason})
}

type computeRuntimeTaskController struct{ service *appdocker.Service }

func (c computeRuntimeTaskController) GetOperation(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Operation, error) {
	return c.service.GetOperation(ctx, principal, id)
}

func (c computeRuntimeTaskController) ListOperationLogs(ctx context.Context, principal domainidentity.Principal, id string, limit int) ([]domaindocker.OperationLog, error) {
	return c.service.ListOperationLogs(ctx, principal, id, limit)
}

func (c computeRuntimeTaskController) CancelOperation(ctx context.Context, principal domainidentity.Principal, id string, input appcompute.TaskMutationInput) (domaindocker.Operation, error) {
	return c.service.CancelOperationIdempotent(ctx, principal, id, appdocker.OperationMutationInput{IdempotencyKey: input.IdempotencyKey, Reason: input.Reason})
}

func (c computeRuntimeTaskController) RetryOperation(ctx context.Context, principal domainidentity.Principal, id string, input appcompute.TaskMutationInput) (domaindocker.Operation, error) {
	return c.service.RetryOperationIdempotent(ctx, principal, id, appdocker.OperationMutationInput{IdempotencyKey: input.IdempotencyKey, Reason: input.Reason})
}
