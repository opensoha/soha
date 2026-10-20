package agent

import (
	"context"
	"fmt"
	resource "github.com/opensoha/soha-contracts/resource"
	"net/http"
	"slices"
)

func (c *Client) NetworkTopologySnapshot(ctx context.Context, input resource.NetworkTopologySnapshotRequest) (resource.NetworkTopologySnapshot, error) {
	summary, err := c.GetSummary(ctx)
	if err != nil {
		return resource.NetworkTopologySnapshot{}, err
	}
	if !slices.Contains(summary.Capabilities, resource.NetworkTopologySnapshotFeature) {
		return resource.NetworkTopologySnapshot{}, fmt.Errorf("%w", resource.ErrNetworkTopologySnapshotUnsupported)
	}
	unsupported := []resource.NetworkTopologySourceResult{}
	if !slices.Contains(summary.Capabilities, resource.NetworkTopologyGatewayL4Feature) {
		filtered := resource.NetworkTopologySnapshotRequest{}
		for _, query := range input.Queries {
			if resource.IsGatewayL4TopologySource(query.SourceID) {
				unsupported = append(unsupported, resource.NetworkTopologySourceResult{Coverage: resource.NetworkTopologyCoverage{Provider: "gateway", SourceID: query.SourceID, Namespace: query.Namespace, State: "unsupported", ReasonCode: "agent_upgrade_required"}})
			} else {
				filtered.Queries = append(filtered.Queries, query)
			}
		}
		input = filtered
	}
	var payload struct {
		Data resource.NetworkTopologySnapshot `json:"data"`
	}
	payload.Data.Version = resource.NetworkTopologySnapshotFeature
	if len(input.Queries) > 0 {
		err = c.request(ctx, http.MethodPost, "/api/v1/platform/network/topology/snapshot", input, &payload)
	}
	payload.Data.Sources = append(payload.Data.Sources, unsupported...)
	return payload.Data, err
}
