package kubernetes

import (
	"context"
	cr "github.com/opensoha/soha-contracts/resource"
	runtime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (c *Client) RelationSnapshot(ctx context.Context, input cr.RelationSnapshotRequest) (cr.RelationSnapshot, error) {
	return runtime.ReadRelationSnapshot(ctx, c.typed.CoreV1().RESTClient(), c.cfg.ID, input)
}
