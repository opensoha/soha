package resource

import (
	"context"
	"io"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) podTerminalRoute(connection domaincluster.Connection) (PodTerminalAgent, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := w.workloadAgentClient(connection)
		if err != nil {
			return nil, err
		}
		adapter, ok := client.(PodTerminalAgent)
		if !ok {
			return nil, unsupportedAgentOperation("Agent does not support native Pod terminal leases")
		}
		return adapter, nil
	}
	adapter, ok := w.directPods.(PodTerminalDirect)
	if !ok {
		return nil, apperrors.ErrUnsupportedOperation
	}
	return directPodTerminalRoute{adapter: adapter, clusterID: connection.Summary.ID}, nil
}

type directPodTerminalRoute struct {
	adapter   PodTerminalDirect
	clusterID string
}

func (route directPodTerminalRoute) CheckPodTerminal(ctx context.Context) error {
	return route.adapter.CheckPodTerminal(ctx, route.clusterID)
}
func (route directPodTerminalRoute) InspectPodExecutionTarget(ctx context.Context, namespace, name, uid, container string) (contractresource.PodExecutionTarget, error) {
	return route.adapter.InspectPodExecutionTarget(ctx, route.clusterID, namespace, name, uid, container)
}
func (route directPodTerminalRoute) StreamNativePodTerminal(ctx context.Context, namespace, name string, input contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	return route.adapter.StreamNativePodTerminal(ctx, route.clusterID, namespace, name, input, stdin, stdout, sizes)
}
