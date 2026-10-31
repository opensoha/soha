package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"testing"

	appcluster "github.com/opensoha/soha/internal/application/cluster"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type currentConnectionFixture struct {
	connection domaincluster.Connection
	err        error
}

func (f *currentConnectionFixture) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return f.connection, f.err
}

func TestDirectClientUsesCurrentConnectionAcrossReplicas(t *testing.T) {
	config := func(host string) string {
		return fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", host)
	}
	repo := &currentConnectionFixture{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "owned", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}, SourceType: "api", Metadata: map[string]any{"kubeconfig_data": config("https://127.0.0.1:41001")}}}
	manager := func() *k8sinfra.Manager {
		m := k8sinfra.NewManager(nil)
		m.SetConfigResolver(func(ctx context.Context, id string) (cfgpkg.ClusterConfig, error) {
			connection, err := repo.GetConnection(ctx, id)
			if err != nil {
				return cfgpkg.ClusterConfig{}, err
			}
			config, err := appcluster.RuntimeClusterConfig(connection)
			if err != nil {
				return cfgpkg.ClusterConfig{}, err
			}
			return *config, nil
		})
		return m
	}
	first, second := manager(), manager()
	for _, replica := range []*k8sinfra.Manager{first, second} {
		bundle, err := replica.Bundle(t.Context(), "owned")
		if err != nil || bundle.RESTConfig.Host != "https://127.0.0.1:41001" {
			t.Fatal("registration on another replica was not visible", err)
		}
	}
	repo.connection.Metadata["kubeconfig_data"] = config("https://127.0.0.1:41002")
	updated, err := second.Bundle(t.Context(), "owned")
	if err != nil || updated.RESTConfig.Host != "https://127.0.0.1:41002" {
		t.Fatal("old connection configuration was reused", err)
	}
	for _, scenario := range []string{"invalid_config", "agent_mode", "deleted", "unavailable", "wrong_identity"} {
		t.Run(scenario, func(t *testing.T) {
			previous := repo.connection
			defer func() { repo.connection, repo.err = previous, nil }()
			switch scenario {
			case "invalid_config":
				repo.connection.Metadata = map[string]any{}
			case "agent_mode":
				repo.connection.Summary.ConnectionMode = domaincluster.ConnectionModeAgent
			case "deleted":
				repo.err = apperrors.ErrNotFound
			case "unavailable":
				repo.err = errors.New("database unavailable")
			case "wrong_identity":
				repo.connection.Summary.ID = "other"
			}
			if bundle, err := second.Bundle(t.Context(), "owned"); err == nil || bundle != nil {
				t.Fatal("stale direct bundle survived a changed connection", err)
			}
		})
	}
}
