package kubernetes

import (
	"context"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (c *Client) ExecuteWorkloadActionConditional(ctx context.Context, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return contractruntime.ApplyConditionalWorkloadAction(queryCtx, c.dynamic, namespace, kind, name, action, input, time.Now())
}
