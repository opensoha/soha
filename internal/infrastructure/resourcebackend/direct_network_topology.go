package resourcebackend

import (
	"context"
	resource "github.com/opensoha/soha-contracts/resource"
	runtime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (d *Direct) NetworkTopologySnapshot(ctx context.Context, clusterID string, input resource.NetworkTopologySnapshotRequest) (resource.NetworkTopologySnapshot, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return resource.NetworkTopologySnapshot{}, err
	}
	return runtime.ReadNetworkTopologySnapshot(ctx, bundle.Typed.CoreV1().RESTClient(), clusterID, input)
}
