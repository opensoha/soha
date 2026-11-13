package kubernetes

import (
	"context"
	cr "github.com/opensoha/soha-contracts/resource"
	runtime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (c *Client) ResourceMonitoringInventory(ctx context.Context) (cr.ResourceMonitoringInventory, error) {
	return runtime.ReadResourceMonitoringInventory(ctx, c.typed.CoreV1().RESTClient(), c.cfg.ID)
}
