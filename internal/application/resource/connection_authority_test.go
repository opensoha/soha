package resource

import (
	"context"
	"errors"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type unavailableConnections struct{ err error }

func (r unavailableConnections) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return domaincluster.Connection{}, r.err
}

type retainedClusterMetadata struct{ calls int }

func (m *retainedClusterMetadata) Metadata(id string) (domaincluster.Summary, error) {
	m.calls++
	return domaincluster.Summary{ID: id, ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}, nil
}

func TestConnectionReadFailureNeverFallsBackToRetainedRuntime(t *testing.T) {
	for _, cause := range []error{apperrors.ErrNotFound, apperrors.ErrAccessDenied, context.Canceled, errors.New("database unavailable")} {
		metadata := &retainedClusterMetadata{}
		access := &resourceAccess{resolver: unavailableConnections{cause}, directClusters: metadata}
		if _, err := access.loadConnection(t.Context(), "removed"); !errors.Is(err, cause) || metadata.calls != 0 {
			t.Fatal("retained runtime/informer data bypassed the current connection", err, metadata.calls)
		}
	}
	metadata := &retainedClusterMetadata{}
	legacy := &resourceAccess{directClusters: metadata}
	if connection, err := legacy.loadConnection(t.Context(), "configured"); err != nil || connection.Summary.ID != "configured" || metadata.calls != 1 {
		t.Fatal("explicit config-only adapter compatibility changed", err)
	}
}
