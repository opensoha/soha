package resource

import (
	"context"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type resourcePresenceAgent interface {
	ObserveResourcePresence(context.Context, string, string, string) (contractresource.ResourcePresence, error)
}

type resourcePresenceDirect interface {
	ObserveResourcePresence(context.Context, string, string, string, string) (contractresource.ResourcePresence, error)
}

func (g *GenericResources) ObserveResourcePresence(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (contractresource.ResourcePresence, error) {
	kind, err := conditionalResourceKind(namespace, kind, name)
	if err != nil {
		return contractresource.ResourcePresence{}, err
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return contractresource.ResourcePresence{}, err
	}
	var result contractresource.ResourcePresence
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, failure := g.genericResourceAgentClient(connection)
		if failure != nil {
			return result, failure
		}
		reader, ok := client.(resourcePresenceAgent)
		if !ok {
			return result, apperrors.ErrUnsupportedOperation
		}
		result, err = reader.ObserveResourcePresence(ctx, namespace, kind, name)
	} else {
		reader, ok := g.direct.(resourcePresenceDirect)
		if !ok {
			return result, apperrors.ErrUnsupportedOperation
		}
		result, err = reader.ObserveResourcePresence(ctx, clusterID, namespace, kind, name)
	}
	if err != nil {
		return contractresource.ResourcePresence{}, wrapAgentResourceError(err)
	}
	if result.ValidateFor(kind, namespace, name) != nil {
		return contractresource.ResourcePresence{}, apperrors.ErrClusterUnready
	}
	return result, nil
}
