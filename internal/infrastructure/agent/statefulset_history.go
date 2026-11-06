package agent

import (
	"context"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (c *Client) ListStatefulSetRolloutHistory(ctx context.Context, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	return c.listControllerRolloutHistory(ctx, "statefulsets", namespace, name)
}
func (c *Client) GetStatefulSetRevisionYAML(ctx context.Context, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	return c.getControllerRevisionYAML(ctx, "statefulsets", namespace, name, revisionName)
}
