package resource

import (
	"context"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *Helm) ListHelmReleases(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	s := h
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.HelmReleaseView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.helmAgentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHelmReleases(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if s.direct == nil {
			return nil, fmt.Errorf("%w: direct helm release reader is not configured", apperrors.ErrClusterUnready)
		}
		items, err = s.direct.ListHelmReleases(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.HelmReleaseView) string { return item.Namespace })
	populateAllowedActionsHelmReleases(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed helm releases via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func populateAllowedActionsHelmReleases(items []domainresource.HelmReleaseView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
