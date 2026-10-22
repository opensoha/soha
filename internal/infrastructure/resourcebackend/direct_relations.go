package resourcebackend

import (
	"context"
	cr "github.com/opensoha/soha-contracts/resource"
	runtime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (d *Direct) RelationSnapshot(ctx context.Context, clusterID string, input cr.RelationSnapshotRequest) (cr.RelationSnapshot, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return cr.RelationSnapshot{}, err
	}
	return runtime.ReadRelationSnapshot(ctx, bundle.Typed.CoreV1().RESTClient(), clusterID, input)
}
