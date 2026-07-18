package resource

import (
	"context"
	"fmt"
	"sort"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (e *Events) ListClusterEvents(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	s := e
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Event", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ClusterEventView
		source string
	)
	backendNamespace := namespace
	backendLimit := limit
	if scopedNamespace, ok := singleScopedNamespace(namespace, decision); ok {
		backendNamespace = scopedNamespace
	} else if needsPostListScopeFilter(namespace, decision) {
		backendLimit = 0
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.eventAgentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListClusterEvents(ctx, backendNamespace, backendLimit)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if s.direct == nil {
			return nil, fmt.Errorf("%w: direct event reader is not configured", apperrors.ErrClusterUnready)
		}
		rawItems, rawSource, err := s.direct.ListClusterEvents(ctx, clusterID, backendNamespace, backendLimit)
		if err != nil {
			return nil, err
		}
		items = rawItems
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ClusterEventView) string { return item.Namespace })
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].LastTimestamp > items[j].LastTimestamp
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Event", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed cluster events via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
