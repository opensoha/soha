package cluster

import (
	"context"
	"errors"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/appconfig"
)

type reconciliationRuntime struct {
	contractRuntime
	ids []string
}

func (r *reconciliationRuntime) ClusterIDs() []string { return r.ids }

type reconciliationRepo struct {
	contractRepository
	listErr error
}

func (r *reconciliationRepo) ListConnections(ctx context.Context) ([]domaincluster.Connection, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.contractRepository.ListConnections(ctx)
}

func TestSyncRetiresDirectWatchesWithoutAnyCacheRead(t *testing.T) {
	for _, name := range []string{"deleted", "agent", "database_unavailable"} {
		t.Run(name, func(t *testing.T) {
			runtime := &reconciliationRuntime{ids: []string{"old"}}
			registry, cache := &contractRegistry{}, &contractCache{}
			repo := &reconciliationRepo{}
			if name == "agent" {
				repo.listConnections = []domaincluster.Connection{{Summary: domaincluster.Summary{ID: "old", ConnectionMode: domaincluster.ConnectionModeAgent}}}
			}
			if name == "database_unavailable" {
				repo.listErr = errors.New("unavailable")
			}
			service, err := New(registry, runtime, cache, nil, repo, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.syncAll(t.Context())
			if name == "database_unavailable" && !errors.Is(err, repo.listErr) {
				t.Fatalf("missing repository failure: %v", err)
			}
			if len(cache.unregistered) == 0 || cache.unregistered[0] != "old" || len(registry.unregistered) == 0 || registry.unregistered[0] != "old" {
				t.Fatalf("retained old runtime: cache=%v registry=%v", cache.unregistered, registry.unregistered)
			}
		})
	}
}

func TestReconciliationPreservesPersistedFileConnection(t *testing.T) {
	registry, cache := &contractRegistry{}, &contractCache{}
	service, _ := New(registry, &reconciliationRuntime{ids: []string{"file"}}, cache, nil, nil, nil, nil, nil)
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "file", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}, SourceType: "file", SourceRef: "/owned/kubeconfig"}
	service.reconcileDirectRegistrations([]domaincluster.Connection{connection})
	if len(cache.unregistered) != 0 || len(registry.unregistered) != 0 {
		t.Fatal("persisted file connection was retired")
	}
	service.registry.RegisterCluster(appconfig.Cluster{ID: "file"})
}
