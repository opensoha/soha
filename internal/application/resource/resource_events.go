package resource

import (
	"context"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var defaultResourceEventKinds = []string{
	"Namespace", "Node", "Pod", "Service", "Event", "Deployment", "StatefulSet",
	"DaemonSet", "ReplicaSet", "Job", "CronJob", "Ingress", "EndpointSlice", "NetworkPolicy",
}

func (g *GenericResources) SubscribeResourceEvents(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, kinds []string) (<-chan domainresource.ResourceStreamEvent, func(), error) {
	normalizedKinds, err := normalizeResourceEventKinds(kinds)
	if err != nil {
		return nil, nil, err
	}
	decisions := make(map[string]domainaccess.Decision, len(normalizedKinds))
	var connection domaincluster.Connection
	for _, kind := range normalizedKinds {
		resolved, decision, authorizeErr := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionWatch)
		if authorizeErr != nil {
			return nil, nil, authorizeErr
		}
		connection = resolved
		decisions[strings.ToLower(kind)] = decision
	}
	var source <-chan domainresource.ResourceStreamEvent
	var unsubscribe func()
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, clientErr := g.genericResourceAgentClient(connection)
		if clientErr != nil {
			return nil, nil, clientErr
		}
		source, unsubscribe, err = client.SubscribeResourceEvents(ctx, namespace, normalizedKinds)
	} else if g.resourceEvents == nil {
		return nil, nil, fmt.Errorf("%w: direct resource event stream is not configured", apperrors.ErrClusterUnready)
	} else {
		source, unsubscribe, err = g.resourceEvents.SubscribeResourceEvents(clusterID, namespace, normalizedKinds)
	}
	if err != nil {
		return nil, nil, err
	}
	out := make(chan domainresource.ResourceStreamEvent, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-source:
				if !ok {
					return
				}
				if !resourceEventAllowed(event, decisions) {
					continue
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ResourceStream", "", string(domainaccess.ActionWatch), "success", "subscribed to Kubernetes resource events")
	return out, unsubscribe, nil
}

func normalizeResourceEventKinds(kinds []string) ([]string, error) {
	if len(kinds) == 0 {
		return append([]string(nil), defaultResourceEventKinds...), nil
	}
	if len(kinds) > 16 {
		return nil, fmt.Errorf("%w: at most 16 resource kinds can be watched", apperrors.ErrInvalidArgument)
	}
	allowed := make(map[string]string, len(defaultResourceEventKinds))
	for _, kind := range defaultResourceEventKinds {
		allowed[strings.ToLower(kind)] = kind
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		key := strings.ToLower(strings.TrimSpace(kind))
		canonical, ok := allowed[key]
		if !ok {
			return nil, fmt.Errorf("%w: resource watch does not support kind %s", apperrors.ErrInvalidArgument, strings.TrimSpace(kind))
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, canonical)
	}
	return normalized, nil
}

func resourceEventAllowed(event domainresource.ResourceStreamEvent, decisions map[string]domainaccess.Decision) bool {
	if event.Resource == nil {
		return true
	}
	decision, ok := decisions[strings.ToLower(event.Resource.Kind)]
	if !ok {
		return false
	}
	items := filterScopedNamespaceItems([]domainresource.ResourceStreamEvent{event}, decision, func(item domainresource.ResourceStreamEvent) string {
		if item.Resource == nil {
			return ""
		}
		return item.Resource.Namespace
	})
	return len(items) == 1
}
