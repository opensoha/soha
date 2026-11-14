package resourcebackend

import (
	"context"
	"io"

	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

func (direct *Direct) CheckPodTerminal(ctx context.Context, clusterID string) error {
	_, err := direct.directClients(ctx, clusterID)
	return conditionalResourceError(err)
}

func (direct *Direct) StreamNativePodTerminal(ctx context.Context, clusterID, namespace, name string, input contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	bundle, err := direct.directClients(ctx, clusterID)
	if err != nil {
		return contractresource.PodTerminalResult{}, conditionalResourceError(err)
	}
	result, err := contractruntime.StreamPodTerminal(ctx, bundle.Typed, bundle.RESTConfig, namespace, name, input, stdin, stdout, sizes, nil)
	return result, conditionalResourceError(err)
}
