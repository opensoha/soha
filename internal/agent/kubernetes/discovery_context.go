package kubernetes

import (
	"context"
	"errors"
	"time"

	"k8s.io/client-go/discovery"
)

const resourceDiscoveryTimeout = 8 * time.Second

// Require the context-aware interface rather than adapting legacy discovery,
// whose background requests could outlive the caller's budget.
func (c *Client) discoveryForContext(ctx context.Context) (discovery.DiscoveryInterfaceWithContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, ok := c.discovery.(discovery.DiscoveryInterfaceWithContext)
	if !ok || client == nil {
		return nil, errors.New("context-aware Kubernetes discovery is unavailable")
	}
	return client, nil
}
