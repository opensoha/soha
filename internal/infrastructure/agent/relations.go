package agent

import (
	"context"
	cr "github.com/opensoha/soha/contracts/resource"
	"net/http"
	"slices"
)

func (c *Client) RelationSnapshot(ctx context.Context, input cr.RelationSnapshotRequest) (cr.RelationSnapshot, error) {
	summary, err := c.GetSummary(ctx)
	if err != nil {
		return cr.RelationSnapshot{}, err
	}
	if !slices.Contains(summary.Capabilities, cr.RelationSnapshotFeature) {
		return cr.RelationSnapshot{}, cr.ErrRelationSnapshotUnsupported
	}
	var payload struct {
		Data cr.RelationSnapshot `json:"data"`
	}
	err = c.request(ctx, http.MethodPost, "/api/v1/platform/resources/relations/snapshot", input, &payload)
	return payload.Data, err
}
