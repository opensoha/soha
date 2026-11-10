package agent

import (
	"context"
	"net/http"

	contractresource "github.com/opensoha/soha-contracts/resource"
)

func (c *Client) GetActionChecks(ctx context.Context) ([]contractresource.AgentActionCheck, error) {
	var payload struct {
		Items []contractresource.AgentActionCheck `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/diagnostics/actions", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}
