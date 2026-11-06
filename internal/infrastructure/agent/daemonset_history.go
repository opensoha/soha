package agent

import (
	"context"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (c *Client) ListDaemonSetRolloutHistory(ctx context.Context, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	return c.listControllerRolloutHistory(ctx, "daemonsets", namespace, name)
}
func (c *Client) GetDaemonSetRevisionYAML(ctx context.Context, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	return c.getControllerRevisionYAML(ctx, "daemonsets", namespace, name, revisionName)
}
