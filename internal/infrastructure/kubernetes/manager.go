package kubernetes

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Bundle struct {
	Typed      kubernetes.Interface
	Dynamic    dynamic.Interface
	Discovery  discovery.DiscoveryInterface
	RESTConfig *rest.Config
}

type Manager struct {
	clusters map[string]cfgpkg.ClusterConfig
	bundles  map[string]*Bundle
	mu       sync.RWMutex
}

func NewManager(clusters []cfgpkg.ClusterConfig) *Manager {
	items := make(map[string]cfgpkg.ClusterConfig, len(clusters))
	for _, cluster := range clusters {
		items[cluster.ID] = cluster
	}
	return &Manager{clusters: items, bundles: map[string]*Bundle{}}
}

func (m *Manager) RegisterCluster(cfg cfgpkg.ClusterConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.clusters[cfg.ID]; ok && reflect.DeepEqual(current, cfg) {
		return
	}
	m.clusters[cfg.ID] = cfg
	delete(m.bundles, cfg.ID)
}

func (m *Manager) UnregisterCluster(clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clusters, clusterID)
	delete(m.bundles, clusterID)
}

func (m *Manager) ValidateCluster(cfg cfgpkg.ClusterConfig) error {
	_, err := createBundle(cfg)
	return err
}

func (m *Manager) ClusterIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.clusters))
	for id := range m.clusters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m *Manager) HasCluster(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.clusters[id]
	return ok
}

func (m *Manager) ListClusters(ctx context.Context) ([]domaincluster.Summary, error) {
	ids := m.ClusterIDs()
	clusters := make([]domaincluster.Summary, 0, len(ids))
	for _, id := range ids {
		summary, err := m.GetCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, summary)
	}
	return clusters, nil
}

func (m *Manager) GetCluster(ctx context.Context, id string) (domaincluster.Summary, error) {
	summary, err := m.Metadata(id)
	if err != nil {
		return domaincluster.Summary{}, err
	}

	inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	bundle, err := m.Bundle(inspectCtx, id)
	if err != nil {
		summary.Health.Message = err.Error()
		return summary, nil
	}

	serverVersion, err := bundle.Discovery.ServerVersion()
	if err != nil {
		summary.Health.Message = err.Error()
		return summary, nil
	}
	groups, err := bundle.Discovery.ServerGroups()
	if err != nil {
		summary.Health.Message = err.Error()
		return summary, nil
	}

	capabilities := make([]string, 0, len(groups.Groups))
	for _, group := range groups.Groups {
		capabilities = append(capabilities, group.Name)
		if len(capabilities) == 8 {
			break
		}
	}

	summary.Version = serverVersion.GitVersion
	summary.Capabilities = capabilities
	summary.Health = domaincluster.Health{Status: "healthy", LastChecked: time.Now()}
	return summary, nil
}

func (m *Manager) Metadata(id string) (domaincluster.Summary, error) {
	m.mu.RLock()
	cfg, ok := m.clusters[id]
	m.mu.RUnlock()
	if !ok {
		return domaincluster.Summary{}, fmt.Errorf("%w: cluster not found: %s", apperrors.ErrNotFound, strings.TrimSpace(id))
	}
	return domaincluster.Summary{
		ID:             cfg.ID,
		Name:           cfg.Name,
		Region:         cfg.Region,
		Environment:    cfg.Environment,
		Labels:         cfg.Labels,
		ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		Health: domaincluster.Health{
			Status:      "unknown",
			LastChecked: time.Now(),
		},
	}, nil
}

func (m *Manager) Bundle(_ context.Context, id string) (*Bundle, error) {
	m.mu.RLock()
	bundle, ok := m.bundles[id]
	m.mu.RUnlock()
	if ok {
		return bundle, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if bundle, ok = m.bundles[id]; ok {
		return bundle, nil
	}
	cfg, ok := m.clusters[id]
	if !ok {
		return nil, fmt.Errorf("%w: cluster not found: %s", apperrors.ErrNotFound, strings.TrimSpace(id))
	}
	created, err := createBundle(cfg)
	if err != nil {
		return nil, err
	}
	m.bundles[id] = created
	return created, nil
}

func createBundle(cfg cfgpkg.ClusterConfig) (*Bundle, error) {
	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig for cluster %s: %w", cfg.ID, err)
	}
	typedClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build typed client for cluster %s: %w", cfg.ID, err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client for cluster %s: %w", cfg.ID, err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build discovery client for cluster %s: %w", cfg.ID, err)
	}
	return &Bundle{Typed: typedClient, Dynamic: dynamicClient, Discovery: discoveryClient, RESTConfig: restConfig}, nil
}

func buildRESTConfig(cfg cfgpkg.ClusterConfig) (*rest.Config, error) {
	if cfg.KubeconfigData != "" {
		clientConfig, err := clientcmd.NewClientConfigFromBytes([]byte(cfg.KubeconfigData))
		if err != nil {
			return nil, err
		}
		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			return nil, err
		}
		restConfig.QPS = 50
		restConfig.Burst = 100
		restConfig.Timeout = 5 * time.Second
		return restConfig, nil
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.Context != "" {
		overrides.CurrentContext = cfg.Context
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	restConfig.QPS = 50
	restConfig.Burst = 100
	restConfig.Timeout = 5 * time.Second
	return restConfig, nil
}
