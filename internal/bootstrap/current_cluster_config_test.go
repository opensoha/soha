package bootstrap

import (
	"context"
	"errors"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type fencedConnectionFixture struct{ connection domaincluster.Connection }

func (f *fencedConnectionFixture) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return f.connection, nil
}

func TestCurrentClusterConfigChecksExecutionDestinationAtActualResolution(t *testing.T) {
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "owned"}, SourceType: "api", Metadata: map[string]any{
		"kubeconfig_data": "apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: https://127.0.0.1:41001\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n"}}
	fingerprint, err := domaincluster.RuntimeConnectionFingerprint(connection)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fencedConnectionFixture{connection}
	resolve := currentClusterConfigResolver(repo)
	ctx := domaincluster.WithExpectedExecutionConnection(t.Context(), "owned", fingerprint)
	if config, err := resolve(ctx, "owned"); err != nil || config.ID != "owned" {
		t.Fatal("matching configuration was rejected", err)
	}
	repo.connection.Metadata = map[string]any{"kubeconfig_data": "replacement"}
	if _, err := resolve(ctx, "owned"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("connection changed at actual transport selection was not fenced", err)
	}
}
