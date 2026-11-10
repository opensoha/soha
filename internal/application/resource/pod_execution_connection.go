package resource

import (
	"context"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func bindPodExecutionConnection(ctx context.Context, connection domaincluster.Connection) (context.Context, string, error) {
	if domaincluster.VerifyExecutionConnection(ctx, connection) != nil {
		return nil, "", apperrors.ErrConflict
	}
	fingerprint, err := domaincluster.RuntimeConnectionFingerprint(connection)
	if err != nil {
		return nil, "", apperrors.ErrConflict
	}
	return domaincluster.WithExpectedExecutionConnection(ctx, connection.Summary.ID, fingerprint), fingerprint, nil
}

func (w *Workloads) authorizePodExecutionConnection(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, fingerprint string) (domaincluster.Connection, error) {
	connection, _, err := w.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return domaincluster.Connection{}, err
	}
	// Empty fingerprints from older pending leases fail closed. Reading and
	// explicitly closing those original records remain available.
	expected := domaincluster.WithExpectedExecutionConnection(ctx, clusterID, fingerprint)
	if domaincluster.VerifyExecutionConnection(expected, connection) != nil {
		return domaincluster.Connection{}, apperrors.ErrConflict
	}
	return connection, nil
}
