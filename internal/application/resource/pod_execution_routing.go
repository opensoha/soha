package resource

import (
	"context"
	"fmt"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) podExecutionRoute(connection domaincluster.Connection) (PodExecutionAgent, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := w.workloadAgentClient(connection)
		if err != nil {
			return nil, err
		}
		adapter, ok := client.(PodExecutionAgent)
		if !ok {
			return nil, unsupportedAgentOperation("Agent does not support bounded Pod argv execution")
		}
		return adapter, nil
	}
	adapter, ok := w.directPods.(PodExecutionDirect)
	if !ok {
		return nil, fmt.Errorf("%w: direct adapter does not support bounded Pod argv execution", apperrors.ErrUnsupportedOperation)
	}
	return directPodExecutionRoute{adapter: adapter, clusterID: connection.Summary.ID}, nil
}

type directPodExecutionRoute struct {
	adapter   PodExecutionDirect
	clusterID string
}

func (r directPodExecutionRoute) InspectPodExecutionTarget(ctx context.Context, namespace, name, uid, container string) (contractresource.PodExecutionTarget, error) {
	return r.adapter.InspectPodExecutionTarget(ctx, r.clusterID, namespace, name, uid, container)
}

func (r directPodExecutionRoute) ExecutePodArgv(ctx context.Context, namespace, name string, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
	return r.adapter.ExecutePodArgv(ctx, r.clusterID, namespace, name, input)
}
