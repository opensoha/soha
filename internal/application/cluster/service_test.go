package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
)

var errStubNotFound = errors.New("not found")

type stubRepository struct {
	connection domaincluster.Connection
}

type stubAuthorizer struct{}

func (stubAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	if request.Cluster.ClusterID == "cluster-2" {
		return domainaccess.Decision{Allowed: false, Reason: "denied"}, nil
	}
	return domainaccess.Decision{Allowed: true, Reason: "allowed", AllowedActions: []domainaccess.Action{request.Action}}, nil
}

func (r *stubRepository) List(context.Context) ([]domaincluster.Summary, error) {
	return []domaincluster.Summary{r.connection.Summary}, nil
}

func (r *stubRepository) Get(_ context.Context, clusterID string) (domaincluster.Summary, error) {
	if clusterID != r.connection.Summary.ID {
		return domaincluster.Summary{}, errStubNotFound
	}
	return r.connection.Summary, nil
}

func (r *stubRepository) ListConnections(context.Context) ([]domaincluster.Connection, error) {
	return []domaincluster.Connection{r.connection}, nil
}

func (r *stubRepository) GetConnection(_ context.Context, clusterID string) (domaincluster.Connection, error) {
	if clusterID != r.connection.Summary.ID {
		return domaincluster.Connection{}, errStubNotFound
	}
	return r.connection, nil
}

func (r *stubRepository) UpsertRegistration(_ context.Context, connection domaincluster.Connection) error {
	r.connection = connection
	return nil
}

func (r *stubRepository) UpdateRegistration(_ context.Context, connection domaincluster.Connection) error {
	r.connection = connection
	return nil
}

func (r *stubRepository) UpsertSnapshot(_ context.Context, summary domaincluster.Summary) error {
	r.connection.Summary = summary
	return nil
}

func (r *stubRepository) Delete(context.Context, string) error {
	return nil
}

func TestUpdatePersistsClusterMonitoringMetadata(t *testing.T) {
	repo := &stubRepository{
		connection: domaincluster.Connection{
			Summary: domaincluster.Summary{
				ID:             "cluster-1",
				Name:           "cluster-1",
				Region:         "local",
				Environment:    "dev",
				Labels:         map[string]string{"provider": "local"},
				ConnectionMode: domaincluster.ConnectionModeAgent,
			},
			CredentialType: "bearer",
			SourceType:     "agent",
			SourceRef:      "http://agent.internal",
			Metadata: map[string]any{
				"endpoint": "http://agent.internal",
				"token":    "agent-token",
			},
		},
	}

	service := &Service{manager: k8sinfra.NewManager(nil), repo: repo}

	_, err := service.Update(context.Background(), domainidentity.Principal{}, "cluster-1", domaincluster.UpdateInput{
		Name:                   "cluster-1",
		Region:                 "local",
		Environment:            "dev",
		Labels:                 map[string]string{"provider": "local"},
		ConnectionMode:         domaincluster.ConnectionModeAgent,
		AgentEndpoint:          "http://agent.internal",
		PrometheusBaseURL:      "http://prometheus.internal:9090",
		PrometheusBearerToken:  "prom-token",
		PrometheusClusterLabel: "k8s_cluster",
		GrafanaBaseURL:         "http://grafana.internal:3000",
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got := repo.connection.Metadata["prometheus_url"]; got != "http://prometheus.internal:9090" {
		t.Fatalf("prometheus_url = %v, want %q", got, "http://prometheus.internal:9090")
	}
	if got := repo.connection.Metadata["prometheus_bearer_token"]; got != "prom-token" {
		t.Fatalf("prometheus_bearer_token = %v, want %q", got, "prom-token")
	}
	if got := repo.connection.Metadata["prometheus_cluster_label"]; got != "k8s_cluster" {
		t.Fatalf("prometheus_cluster_label = %v, want %q", got, "k8s_cluster")
	}
	if got := repo.connection.Metadata["grafana_base_url"]; got != "http://grafana.internal:3000" {
		t.Fatalf("grafana_base_url = %v, want %q", got, "http://grafana.internal:3000")
	}
}

func TestRegisterGeneratesClusterID(t *testing.T) {
	repo := &stubRepository{}
	service := &Service{manager: k8sinfra.NewManager(nil), repo: repo}

	item, err := service.Register(context.Background(), domainidentity.Principal{}, domaincluster.RegisterInput{
		Name:           "demo-cluster",
		ConnectionMode: domaincluster.ConnectionModeAgent,
		AgentEndpoint:  "http://agent.internal",
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if strings.TrimSpace(item.ID) == "" {
		t.Fatalf("Register returned empty cluster ID")
	}
	if strings.TrimSpace(repo.connection.Summary.ID) == "" {
		t.Fatalf("registration persisted empty cluster ID")
	}
}

func TestUpdateRetainsExistingAgentEndpointWhenOmitted(t *testing.T) {
	repo := &stubRepository{
		connection: domaincluster.Connection{
			Summary: domaincluster.Summary{
				ID:             "cluster-1",
				Name:           "cluster-1",
				ConnectionMode: domaincluster.ConnectionModeAgent,
			},
			CredentialType: "bearer",
			SourceType:     "agent",
			SourceRef:      "http://agent.internal",
			Metadata: map[string]any{
				"endpoint": "http://agent.internal",
				"token":    "agent-token",
			},
		},
	}

	service := &Service{manager: k8sinfra.NewManager(nil), repo: repo}

	_, err := service.Update(context.Background(), domainidentity.Principal{}, "cluster-1", domaincluster.UpdateInput{
		Name:           "cluster-1",
		ConnectionMode: domaincluster.ConnectionModeAgent,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got := repo.connection.Metadata["endpoint"]; got != "http://agent.internal" {
		t.Fatalf("endpoint = %v, want %q", got, "http://agent.internal")
	}
}

func TestUpdateRetainsExistingKubeContextWhenOmitted(t *testing.T) {
	repo := &stubRepository{
		connection: domaincluster.Connection{
			Summary: domaincluster.Summary{
				ID:             "cluster-1",
				Name:           "cluster-1",
				ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
			},
			CredentialType: "kubeconfig",
			SourceType:     "api",
			SourceRef:      "cluster.register",
			Metadata: map[string]any{
				"kubeconfig": "apiVersion: v1\nkind: Config\nclusters:\n- name: prod\n  cluster:\n    server: https://127.0.0.1:6443\ncontexts:\n- name: prod\n  context:\n    cluster: prod\n    user: prod\ncurrent-context: prod\nusers:\n- name: prod\n  user:\n    token: demo\n",
				"context":    "prod",
			},
		},
	}

	service := &Service{manager: k8sinfra.NewManager(nil), repo: repo}

	_, err := service.Update(context.Background(), domainidentity.Principal{}, "cluster-1", domaincluster.UpdateInput{
		Name:           "cluster-1",
		ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got := repo.connection.Metadata["context"]; got != "prod" {
		t.Fatalf("context = %v, want %q", got, "prod")
	}
}

func TestListAccessibleFiltersUnauthorizedClusters(t *testing.T) {
	repo := &stubRepository{
		connection: domaincluster.Connection{
			Summary: domaincluster.Summary{
				ID:             "cluster-1",
				Name:           "cluster-1",
				Region:         "local",
				Environment:    "dev",
				ConnectionMode: domaincluster.ConnectionModeAgent,
			},
		},
	}
	service := &Service{manager: k8sinfra.NewManager(nil), repo: repo, authorizer: stubAuthorizer{}}

	items, err := service.ListAccessible(context.Background(), domainidentity.Principal{})
	if err != nil {
		t.Fatalf("ListAccessible returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "cluster-1" {
		t.Fatalf("ListAccessible = %v, want only cluster-1", items)
	}

	repo.connection.Summary.ID = "cluster-2"
	repo.connection.Summary.Name = "cluster-2"

	items, err = service.ListAccessible(context.Background(), domainidentity.Principal{})
	if err != nil {
		t.Fatalf("ListAccessible returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListAccessible = %v, want empty result", items)
	}
}

func TestSyncConnectionUsesFixedHealthMessageForInvalidClusterConfig(t *testing.T) {
	repo := &stubRepository{
		connection: domaincluster.Connection{
			Summary: domaincluster.Summary{
				ID:             "cluster-1",
				Name:           "cluster-1",
				ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
			},
		},
	}
	service := &Service{
		manager: k8sinfra.NewManager(nil),
		repo:    repo,
	}

	if err := service.syncConnection(context.Background(), repo.connection); err != nil {
		t.Fatalf("syncConnection returned error: %v", err)
	}
	if got := repo.connection.Summary.Health.Message; got != "cluster configuration unavailable" {
		t.Fatalf("health message = %q, want fixed fallback", got)
	}
	if strings.Contains(repo.connection.Summary.Health.Message, "has no kubeconfig metadata") {
		t.Fatalf("health message leaked underlying config error: %q", repo.connection.Summary.Health.Message)
	}
}
