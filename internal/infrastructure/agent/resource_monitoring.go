package agent

import (
	"context"
	cr "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
	"slices"
)

func (c *Client) ResourceMonitoringInventory(ctx context.Context) (cr.ResourceMonitoringInventory, error) {
	summary, err := c.GetSummary(ctx)
	if err != nil {
		return cr.ResourceMonitoringInventory{}, err
	}
	if !slices.Contains(summary.Capabilities, cr.ResourceMonitoringSnapshotFeature) {
		return cr.ResourceMonitoringInventory{}, apperrors.ErrUnsupportedOperation
	}
	var payload struct {
		Data cr.ResourceMonitoringInventory `json:"data"`
	}
	err = c.request(ctx, http.MethodGet, "/api/v1/platform/infrastructure/resource-monitoring", nil, &payload)
	return payload.Data, err
}
