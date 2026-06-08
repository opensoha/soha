package kubernetes

import (
	"testing"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func TestRegisterClusterKeepsBundleWhenConfigUnchanged(t *testing.T) {
	manager := NewManager([]cfgpkg.ClusterConfig{{
		ID:         "cluster-1",
		Name:       "cluster-1",
		Kubeconfig: "/tmp/cluster-1",
	}})
	existing := &Bundle{}
	manager.bundles["cluster-1"] = existing

	manager.RegisterCluster(cfgpkg.ClusterConfig{
		ID:         "cluster-1",
		Name:       "cluster-1",
		Kubeconfig: "/tmp/cluster-1",
	})

	if manager.bundles["cluster-1"] != existing {
		t.Fatalf("expected bundle to be kept when cluster config is unchanged")
	}
}

func TestRegisterClusterDropsBundleWhenConfigChanges(t *testing.T) {
	manager := NewManager([]cfgpkg.ClusterConfig{{
		ID:         "cluster-1",
		Name:       "cluster-1",
		Kubeconfig: "/tmp/cluster-1",
	}})
	manager.bundles["cluster-1"] = &Bundle{}

	manager.RegisterCluster(cfgpkg.ClusterConfig{
		ID:         "cluster-1",
		Name:       "cluster-1",
		Kubeconfig: "/tmp/cluster-2",
	})

	if _, ok := manager.bundles["cluster-1"]; ok {
		t.Fatalf("expected bundle to be dropped when cluster config changes")
	}
}
