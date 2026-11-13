package kubernetes

import (
	"context"
	resource "github.com/opensoha/soha-contracts/resource"
	runtime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (c *Client) NetworkTopologySnapshot(ctx context.Context, input resource.NetworkTopologySnapshotRequest) (resource.NetworkTopologySnapshot, error) {
	return runtime.ReadNetworkTopologySnapshot(ctx, c.typed.CoreV1().RESTClient(), c.cfg.ID, input)
}
