package resource

import (
	"context"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *Helm) authorizeHelmMutation(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, action domainaccess.Action) (domaincluster.Connection, domainaccess.Decision, error) {
	connection, decision, err := h.authorize(ctx, principal, clusterID, namespace, "HelmRelease", action)
	if err != nil || connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return connection, decision, err
	}
	return connection, decision, h.requireClusterWideHelmMutation(ctx, principal, connection, namespace, action)
}

// Helm can run hooks and write resources outside the release namespace with the Agent identity.
func (h *Helm) requireClusterWideHelmMutation(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace string, action domainaccess.Action) error {
	request := h.resourceAccessRequest(ctx, principal, connection, "", resourceGroupForKind("HelmRelease"), "HelmRelease", action)
	request.RequireClusterWideNamespace = true
	decision, err := h.authorizer.Authorize(ctx, request)
	if err != nil {
		return err
	}
	if clusterWideResourceScope(decision) {
		return nil
	}
	const reason = "Agent Helm writes require cluster-wide Helm release permission"
	_ = h.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", "", string(action), "deny", reason)
	return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, reason)
}
