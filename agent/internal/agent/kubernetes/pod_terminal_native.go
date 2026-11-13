package kubernetes

import (
	"context"
	"io"

	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

func (client *Client) StreamNativePodTerminal(ctx context.Context, namespace, name string, input contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	return contractruntime.StreamPodTerminal(ctx, client.typed, client.restConfig, namespace, name, input, stdin, stdout, sizes, nil)
}
