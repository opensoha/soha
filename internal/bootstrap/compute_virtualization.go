package bootstrap

import (
	"context"

	appcompute "github.com/opensoha/soha/internal/application/compute"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

type computeVirtualizationController struct{ service *appvirtualization.Service }

func (c computeVirtualizationController) TestConnectionIdempotent(ctx context.Context, principal domainidentity.Principal, id, key string) (domainvirtualization.Task, error) {
	return c.service.TestConnectionIdempotent(ctx, principal, id, key)
}

func (c computeVirtualizationController) SyncConnectionIdempotent(ctx context.Context, principal domainidentity.Principal, id, key string) (domainvirtualization.Task, error) {
	return c.service.SyncConnectionIdempotent(ctx, principal, id, key)
}

func (c computeVirtualizationController) ExecuteVMAction(ctx context.Context, principal domainidentity.Principal, id string, input appcompute.VirtualizationActionInput) (domainvirtualization.Task, error) {
	return c.service.VMAction(ctx, principal, id, appvirtualization.VMActionInput{
		Action: input.Action, CPU: input.CPU, MemoryMiB: input.MemoryMiB, DiskGiB: input.DiskGiB, IdempotencyKey: input.IdempotencyKey,
	})
}
