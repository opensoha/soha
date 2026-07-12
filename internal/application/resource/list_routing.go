package resource

import (
	"context"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type resourceListRequest struct {
	clusterID string
	namespace string
	kind      string
	summary   func(string) string
}

type directDetailRequest struct {
	clusterID   string
	namespace   string
	kind        string
	name        string
	unsupported string
	summary     string
}

type resourceDetailRequest struct {
	clusterID string
	namespace string
	kind      string
	name      string
	summary   func(string) string
}

func getModeResource[T any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, request resourceDetailRequest, load func(domaincluster.Connection) (T, string, error), setActions func(*T, []string)) (T, error) {
	var zero T
	connection, decision, err := access.authorize(ctx, principal, request.clusterID, request.namespace, request.kind, domainaccess.ActionView)
	if err != nil {
		return zero, err
	}
	item, source, err := load(connection)
	if err != nil {
		return zero, err
	}
	setActions(&item, stringifyActions(decision.AllowedActions))
	_ = access.recordAudit(ctx, principal, connection.Summary.ID, request.namespace, request.kind, request.name, string(domainaccess.ActionView), "success", request.summary(source))
	return item, nil
}

func getDirectDetail[T any, D any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, request directDetailRequest, directFactory func() (D, error), load func(D) (T, error), setActions func(*T, []string)) (T, error) {
	var zero T
	connection, decision, err := access.authorize(ctx, principal, request.clusterID, request.namespace, request.kind, domainaccess.ActionView)
	if err != nil {
		return zero, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return zero, unsupportedAgentOperation(request.unsupported)
	}
	direct, err := directFactory()
	if err != nil {
		return zero, err
	}
	item, err := load(direct)
	if err != nil {
		return zero, err
	}
	setActions(&item, stringifyActions(decision.AllowedActions))
	_ = access.recordAudit(ctx, principal, connection.Summary.ID, request.namespace, request.kind, request.name, string(domainaccess.ActionView), "success", request.summary)
	return item, nil
}

func requireDirect[D any](direct D, configured bool, name string) (D, error) {
	if !configured {
		var zero D
		return zero, fmt.Errorf("%w: direct %s is not configured", apperrors.ErrClusterUnready, name)
	}
	return direct, nil
}

func listModeResources[T any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, request resourceListRequest, load func(domaincluster.Connection) ([]T, string, error), namespaceOf func(T) string, actionsOf func(T) []string, setActions func(*T, []string)) ([]T, error) {
	connection, decision, err := access.authorize(ctx, principal, request.clusterID, request.namespace, request.kind, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	items, source, err := load(connection)
	if err != nil {
		return nil, err
	}
	if namespaceOf != nil {
		items = filterScopedNamespaceItems(items, decision, namespaceOf)
	}
	populateAllowedActions(items, decision, actionsOf, setActions)
	_ = access.recordAudit(ctx, principal, connection.Summary.ID, request.namespace, request.kind, "", string(domainaccess.ActionList), "success", request.summary(source))
	return items, nil
}

func listRoutedModeResources[T any, A any, D any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, request resourceListRequest, agentFactory func(domaincluster.Connection) (A, error), directFactory func() (D, error), agentCall func(A) ([]T, error), directCall func(D) ([]T, error), namespaceOf func(T) string, actionsOf func(T) []string, setActions func(*T, []string)) ([]T, error) {
	return listModeResources(ctx, access, principal, request, func(connection domaincluster.Connection) ([]T, string, error) {
		return routeModeItems(connection, agentFactory, directFactory, agentCall, directCall)
	}, namespaceOf, actionsOf, setActions)
}

func listSourcedModeResources[T any, A any, D any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, request resourceListRequest, agentFactory func(domaincluster.Connection) (A, error), directFactory func() (D, error), agentCall func(A) ([]T, error), directCall func(D) ([]T, string, error), namespaceOf func(T) string, actionsOf func(T) []string, setActions func(*T, []string)) ([]T, error) {
	return listModeResources(ctx, access, principal, request, func(connection domaincluster.Connection) ([]T, string, error) {
		return routeModeSourcedItems(connection, agentFactory, directFactory, agentCall, directCall)
	}, namespaceOf, actionsOf, setActions)
}

func bindNamespacedAgentList[A any, T any](ctx context.Context, namespace string, list func(A, context.Context, string) ([]T, error)) func(A) ([]T, error) {
	return func(client A) ([]T, error) {
		return list(client, ctx, namespace)
	}
}

func bindClusterAgentList[A any, T any](ctx context.Context, list func(A, context.Context) ([]T, error)) func(A) ([]T, error) {
	return func(client A) ([]T, error) {
		return list(client, ctx)
	}
}

func bindNamespacedDirectList[D any, T any](ctx context.Context, clusterID, namespace string, list func(D, context.Context, string, string) ([]T, error)) func(D) ([]T, error) {
	return func(direct D) ([]T, error) {
		return list(direct, ctx, clusterID, namespace)
	}
}

func bindClusterDirectList[D any, T any](ctx context.Context, clusterID string, list func(D, context.Context, string) ([]T, error)) func(D) ([]T, error) {
	return func(direct D) ([]T, error) {
		return list(direct, ctx, clusterID)
	}
}

func bindNamespacedSourcedDirectList[D any, T any](ctx context.Context, clusterID, namespace string, list func(D, context.Context, string, string) ([]T, string, error)) func(D) ([]T, string, error) {
	return func(direct D) ([]T, string, error) {
		return list(direct, ctx, clusterID, namespace)
	}
}

func bindClusterSourcedDirectList[D any, T any](ctx context.Context, clusterID string, list func(D, context.Context, string) ([]T, string, error)) func(D) ([]T, string, error) {
	return func(direct D) ([]T, string, error) {
		return list(direct, ctx, clusterID)
	}
}

func bindClusterAgentValue[A any, T any](ctx context.Context, name string, get func(A, context.Context, string) (T, error)) func(A) (T, error) {
	return func(client A) (T, error) {
		return get(client, ctx, name)
	}
}

func bindClusterDirectValue[D any, T any](ctx context.Context, clusterID, name string, get func(D, context.Context, string, string) (T, error)) func(D) (T, error) {
	return func(direct D) (T, error) {
		return get(direct, ctx, clusterID, name)
	}
}

func routeModeItems[T any, A any, D any](connection domaincluster.Connection, agentFactory func(domaincluster.Connection) (A, error), directFactory func() (D, error), agentCall func(A) ([]T, error), directCall func(D) ([]T, error)) ([]T, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := agentFactory(connection)
		if err != nil {
			return nil, "agent", err
		}
		items, err := agentCall(client)
		return items, "agent", wrapAgentResourceError(err)
	}
	direct, err := directFactory()
	if err != nil {
		return nil, "live", err
	}
	items, err := directCall(direct)
	return items, "live", err
}

func routeModeSourcedItems[T any, A any, D any](connection domaincluster.Connection, agentFactory func(domaincluster.Connection) (A, error), directFactory func() (D, error), agentCall func(A) ([]T, error), directCall func(D) ([]T, string, error)) ([]T, string, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := agentFactory(connection)
		if err != nil {
			return nil, "agent", err
		}
		items, err := agentCall(client)
		return items, "agent", wrapAgentResourceError(err)
	}
	direct, err := directFactory()
	if err != nil {
		return nil, "live", err
	}
	return directCall(direct)
}

func routeModeValue[T any, A any, D any](connection domaincluster.Connection, agentFactory func(domaincluster.Connection) (A, error), directFactory func() (D, error), agentCall func(A) (T, error), directCall func(D) (T, error)) (T, string, error) {
	var zero T
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := agentFactory(connection)
		if err != nil {
			return zero, "agent", err
		}
		item, err := agentCall(client)
		return item, "agent", wrapAgentResourceError(err)
	}
	direct, err := directFactory()
	if err != nil {
		return zero, "live", err
	}
	item, err := directCall(direct)
	return item, "live", err
}
