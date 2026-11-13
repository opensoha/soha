package kubernetes

import (
	"context"
	"fmt"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	helmruntime "github.com/opensoha/soha-contracts/helmrelease/runtime"
)

func (c *Client) PrepareHelmDelivery(ctx context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	if err := c.validateHelmDelivery(input); err != nil {
		return input, err
	}
	if input.Action != sohaapi.Preflight {
		return input, fmt.Errorf("helm preparation requires a preflight action")
	}
	cfg, err := c.helmActionConfig(input.Snapshot.Namespace)
	if err != nil {
		return input, err
	}
	return helmruntime.Prepare(ctx, cfg, input)
}

func (c *Client) ExecuteHelmDelivery(ctx context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	if err := c.validateHelmDelivery(input); err != nil {
		return sohaapi.HelmExecutionTaskResult{Stopped: true}, err
	}
	cfg, err := c.helmActionConfig(input.Snapshot.Namespace)
	if err != nil {
		return sohaapi.HelmExecutionTaskResult{Stopped: true}, err
	}
	return helmruntime.Execute(ctx, cfg, input)
}

func (c *Client) validateHelmDelivery(input sohaapi.HelmExecutionTaskPayload) error {
	if c == nil || c.cfg.ID == "" || input.Snapshot.ClusterID != c.cfg.ID {
		return fmt.Errorf("helm task targets a different cluster")
	}
	return helmrelease.ValidateTaskIdentity(input)
}
