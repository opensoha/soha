package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (c *Client) PrepareHelmDelivery(ctx context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	var response struct {
		Data sohaapi.HelmExecutionTaskPayload `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, "/api/v1/platform/helm/delivery/prepare", input, &response); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return input, err
		}
		return input, fmt.Errorf("%w: Agent Helm preparation is unavailable", apperrors.ErrClusterUnready)
	}
	return response.Data, nil
}

func (c *Client) ExecuteHelmDelivery(ctx context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	if input.Action != sohaapi.Preflight && input.Action != sohaapi.Observe {
		return sohaapi.HelmExecutionTaskResult{}, fmt.Errorf("%w: Agent Helm mutations require a claimed task", apperrors.ErrInvalidArgument)
	}
	var response struct {
		Data sohaapi.HelmExecutionTaskResult `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, "/api/v1/platform/helm/delivery/"+string(input.Action), input, &response); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return response.Data, err
		}
		return response.Data, fmt.Errorf("%w: Agent Helm observation is unavailable", apperrors.ErrClusterUnready)
	}
	return response.Data, nil
}
