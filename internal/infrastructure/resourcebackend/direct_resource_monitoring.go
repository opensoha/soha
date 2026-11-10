package resourcebackend

import (
	"context"
	cr "github.com/opensoha/soha-contracts/resource"
	runtime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (d *Direct) ResourceMonitoringInventory(ctx context.Context, clusterID string) (cr.ResourceMonitoringInventory, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return cr.ResourceMonitoringInventory{}, err
	}
	return runtime.ReadResourceMonitoringInventory(ctx, bundle.Typed.CoreV1().RESTClient(), clusterID)
}
