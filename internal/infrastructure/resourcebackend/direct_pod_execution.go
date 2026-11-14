package resourcebackend

import (
	"context"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

func (d *Direct) InspectPodExecutionTarget(ctx context.Context, clusterID, namespace, name, uid, container string) (contractresource.PodExecutionTarget, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	bundle, err := d.directClients(queryCtx, clusterID)
	if err != nil {
		return contractresource.PodExecutionTarget{}, conditionalResourceError(err)
	}
	item, err := contractruntime.InspectPodExecutionTarget(queryCtx, bundle.Typed, namespace, name, uid, container)
	return item, conditionalResourceError(err)
}

func (d *Direct) ExecutePodArgv(ctx context.Context, clusterID, namespace, name string, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return contractresource.PodArgvResult{}, conditionalResourceError(err)
	}
	item, err := contractruntime.ExecutePodArgv(ctx, bundle.Typed, bundle.RESTConfig, namespace, name, input, nil)
	return item, conditionalResourceError(err)
}
