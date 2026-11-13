package kubernetes

import (
	"context"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

func (c *Client) InspectPodExecutionTarget(ctx context.Context, namespace, name, uid, container string) (contractresource.PodExecutionTarget, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return contractruntime.InspectPodExecutionTarget(queryCtx, c.typed, namespace, name, uid, container)
}

func (c *Client) ExecutePodArgv(ctx context.Context, namespace, name string, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
	return contractruntime.ExecutePodArgv(ctx, c.typed, c.restConfig, namespace, name, input, nil)
}
