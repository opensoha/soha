package agent

import (
	"context"
	"fmt"
	"net/http"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

func (c *Client) ExecuteManifestRollout(ctx context.Context, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	var result struct {
		Data domainmanifest.TaskResult `json:"data"`
	}
	operation := "observe"
	switch payload.Action {
	case domainmanifest.TaskActionObserve:
	case domainmanifest.TaskActionRolloutControl:
		operation = "control"
	default:
		return result.Data, fmt.Errorf("only rollout observation and control use the Agent API")
	}
	err := c.request(ctx, http.MethodPost, "/api/v1/platform/ownership-v2/manifests/rollout/"+operation, payload, &result)
	return result.Data, err
}
