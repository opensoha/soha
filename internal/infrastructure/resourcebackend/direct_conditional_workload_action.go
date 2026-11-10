package resourcebackend

import (
	"context"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (d *Direct) ExecuteWorkloadActionConditional(ctx context.Context, clusterID, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	bundle, err := d.directClients(queryCtx, clusterID)
	if err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, conditionalResourceError(err)
	}
	item, err := contractruntime.ApplyConditionalWorkloadAction(queryCtx, bundle.Dynamic, namespace, kind, name, action, input, time.Now())
	return item, conditionalResourceError(err)
}
