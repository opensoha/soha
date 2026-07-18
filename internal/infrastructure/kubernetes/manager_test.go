package kubernetes

import (
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func TestBuildRESTConfigDoesNotSetGlobalRequestTimeout(t *testing.T) {
	config, err := buildRESTConfig(cfgpkg.ClusterConfig{
		ID: "cluster-1",
		KubeconfigData: `apiVersion: v1
kind: Config
clusters:
- name: cluster-1
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: cluster-1
  context:
    cluster: cluster-1
    user: user-1
current-context: cluster-1
users:
- name: user-1
  user:
    token: test-token
`,
	})
	if err != nil {
		t.Fatalf("buildRESTConfig() error = %v", err)
	}
	if config.Timeout != 0*time.Second {
		t.Fatalf("rest.Config.Timeout = %v, want no global timeout", config.Timeout)
	}
}

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
