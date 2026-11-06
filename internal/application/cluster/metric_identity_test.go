package cluster

import (
	"context"
	"errors"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
)

func TestClusterMetricAliasPersistsAndPartialUpdatePreservesIt(t *testing.T) {
	repo := &stubRepository{}
	service := newTestService(t, repo)
	service.authorizer = stubAuthorizer{}
	_, err := service.Register(context.Background(), domainidentity.Principal{}, domaincluster.RegisterInput{
		ID: "cluster-a", Name: "Example", ConnectionMode: domaincluster.ConnectionModeAgent,
		AgentEndpoint: "http://agent.invalid", AgentToken: "fixture", PrometheusBaseURL: "http://metrics.invalid",
		PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: "middleware-prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertValue := func(want string) {
		t.Helper()
		if got := metadataString(repo.connection.Metadata, "prometheus_cluster_label_value"); got != want {
			t.Fatalf("saved alias %q, want %q", got, want)
		}
		detail, err := service.Describe(context.Background(), domainidentity.Principal{}, "cluster-a")
		if err != nil {
			t.Fatal(err)
		}
		if detail.Monitoring.Prometheus.ClusterLabelValue != want {
			t.Fatal("detail lost saved alias")
		}
	}
	assertValue("middleware-prod")
	if _, err = service.Update(context.Background(), domainidentity.Principal{}, "cluster-a", domaincluster.UpdateInput{Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	assertValue("middleware-prod")
	empty := ""
	if _, err = service.Update(context.Background(), domainidentity.Principal{}, "cluster-a", domaincluster.UpdateInput{Name: "Renamed", PrometheusClusterLabelValue: &empty}); err != nil {
		t.Fatal(err)
	}
	assertValue("")
}

func TestMetricIdentityValidatesLabelAndValue(t *testing.T) {
	for _, input := range []domaincluster.RegisterInput{
		{PrometheusClusterLabelValue: "alias"},
		{PrometheusClusterLabel: "cluster=bad", PrometheusClusterLabelValue: "alias"},
		{PrometheusClusterLabel: "cluster=bad"},
		{PrometheusClusterLabel: "namespace", PrometheusClusterLabelValue: "team"},
		{PrometheusClusterLabel: "__name__"},
		{PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: strings.Repeat("x", 1025)},
	} {
		if err := normalizeClusterMetricIdentity(&input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("accepted invalid identity: %v", err)
		}
	}
	input := domaincluster.RegisterInput{PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: `alias"with\quoted`}
	if err := normalizeClusterMetricIdentity(&input); err != nil {
		t.Fatal(err)
	}
}

func TestDirectMetricAliasSurvivesRuntimeConfig(t *testing.T) {
	input := domaincluster.RegisterInput{ID: "real-id", Name: "Example", Kubeconfig: "fixture", PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: "alias"}
	service := &Service{}
	connection, cfg, err := service.buildConnection(input)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrometheusClusterLabelValue != "alias" {
		t.Fatal("new registration config lost alias")
	}
	restored, err := runtimeClusterConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	if restored.PrometheusClusterLabelValue != "alias" || restored.ID != "real-id" {
		t.Fatal("runtime restore changed identity")
	}
}

type metricIdentityRegistry struct{ cfg appconfig.Cluster }

func (r *metricIdentityRegistry) ValidateCluster(cfg appconfig.Cluster) error {
	r.cfg = cfg
	return nil
}
func (r *metricIdentityRegistry) RegisterCluster(cfg appconfig.Cluster) { r.cfg = cfg }
func (*metricIdentityRegistry) UnregisterCluster(string)                {}

func TestMonitoringEditPreservesFileBackedCredential(t *testing.T) {
	repo := &stubRepository{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "file-cluster", Name: "File", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}, SourceType: "config", SourceRef: "config.yaml", Metadata: map[string]any{"kubeconfig": "/fixture/kubeconfig"}}}
	service := newTestService(t, repo)
	registry := &metricIdentityRegistry{}
	service.registry = registry
	alias := "middleware-prod"
	_, err := service.Update(context.Background(), domainidentity.Principal{}, "file-cluster", domaincluster.UpdateInput{Name: "File", PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: &alias})
	if err != nil {
		t.Fatal(err)
	}
	if registry.cfg.Kubeconfig != "/fixture/kubeconfig" || registry.cfg.KubeconfigData != "" || repo.connection.SourceType != "config" {
		t.Fatal("monitoring edit changed file credential into inline data")
	}
}

type metricIdentityAuthorizer struct {
	global   bool
	requests []domainaccess.Request
}

func (a *metricIdentityAuthorizer) Authorize(_ context.Context, req domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, req)
	return domainaccess.Decision{
		Allowed:       req.Cluster.ClusterID != "" || a.global,
		ResourceScope: &domainaccess.ResourceScope{},
	}, nil
}

func TestMetricSourceRequiresUnscopedClusterPermission(t *testing.T) {
	repo := &stubRepository{}
	service := newTestService(t, repo)
	authorizer := &metricIdentityAuthorizer{}
	service.authorizer = authorizer
	input := domaincluster.RegisterInput{ID: "cluster-a", Name: "Example", ConnectionMode: domaincluster.ConnectionModeAgent,
		AgentEndpoint: "http://agent.invalid", AgentToken: "fixture", PrometheusBaseURL: "http://metrics.invalid"}
	if _, err := service.Register(t.Context(), domainidentity.Principal{}, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("scoped registration configured Prometheus: %v", err)
	}
	if len(authorizer.requests) != 2 || !authorizer.requests[1].RequireUnscopedPlatform || authorizer.requests[1].Cluster.ClusterID != "" || authorizer.requests[1].PermissionKey != "platform.clusters.update" {
		t.Fatalf("global source authorization = %+v", authorizer.requests)
	}
	authorizer.global = true
	if _, err := service.Register(t.Context(), domainidentity.Principal{}, input); err != nil {
		t.Fatal(err)
	}
	authorizer.global = false
	if _, err := service.Update(t.Context(), domainidentity.Principal{}, input.ID, domaincluster.UpdateInput{Name: "Renamed"}); err != nil {
		t.Fatalf("name-only edit should preserve the metric source: %v", err)
	}
	if got := metadataString(repo.connection.Metadata, "prometheus_url"); got != input.PrometheusBaseURL {
		t.Fatalf("name-only edit replaced the metric source with %q", got)
	}
	alias := "other-cluster"
	if _, err := service.Update(t.Context(), domainidentity.Principal{}, input.ID, domaincluster.UpdateInput{
		Name: "Example", PrometheusBaseURL: &input.PrometheusBaseURL, PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: &alias,
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("scoped update changed Prometheus alias: %v", err)
	}
	empty := ""
	if _, err := service.Update(t.Context(), domainidentity.Principal{}, input.ID, domaincluster.UpdateInput{
		Name: "Example", PrometheusBaseURL: &empty,
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("scoped update disabled Prometheus: %v", err)
	}
	authorizer.global = true
	if _, err := service.Update(t.Context(), domainidentity.Principal{}, input.ID, domaincluster.UpdateInput{
		Name: "Example", PrometheusBaseURL: &empty,
	}); err != nil {
		t.Fatalf("unscoped update could not explicitly disable Prometheus: %v", err)
	}
	if got := metadataString(repo.connection.Metadata, "prometheus_url"); got != "" {
		t.Fatalf("explicit disable retained URL %q", got)
	}
}

func TestMetricSourceRejectsDuplicateMatcherAndAllowsDistinctAlias(t *testing.T) {
	repo := &stubRepository{connection: domaincluster.Connection{
		Summary:  domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{"prometheus_url": "http://metrics.invalid/", "prometheus_cluster_label": "cluster"},
	}}
	service := newTestService(t, repo)
	service.authorizer = stubAuthorizer{}
	input := domaincluster.RegisterInput{ID: "cluster-b", Name: "Example", ConnectionMode: domaincluster.ConnectionModeAgent,
		AgentEndpoint: "http://agent.invalid", AgentToken: "fixture", PrometheusBaseURL: "http://metrics.invalid",
		PrometheusClusterLabel: "cluster", PrometheusClusterLabelValue: "cluster-a"}
	if _, err := service.Register(t.Context(), domainidentity.Principal{}, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("shared source accepted duplicate identity: %v", err)
	}
	input.PrometheusClusterLabelValue = "other-cluster"
	if _, err := service.Register(t.Context(), domainidentity.Principal{}, input); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterCannotReplaceExistingMetricSource(t *testing.T) {
	repo := &stubRepository{connection: domaincluster.Connection{
		Summary:  domaincluster.Summary{ID: "cluster-a"},
		Metadata: map[string]any{"prometheus_url": "http://metrics.invalid", "prometheus_cluster_label_value": "existing"},
	}}
	service := newTestService(t, repo)
	service.authorizer = &metricIdentityAuthorizer{}
	_, err := service.Register(t.Context(), domainidentity.Principal{}, domaincluster.RegisterInput{
		ID: "cluster-a", Name: "Overwrite", ConnectionMode: domaincluster.ConnectionModeAgent,
		AgentEndpoint: "http://agent.invalid", AgentToken: "fixture",
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("POST registration replaced existing ID: %v", err)
	}
	if got := metadataString(repo.connection.Metadata, "prometheus_url"); got != "http://metrics.invalid" {
		t.Fatalf("existing metric source changed: %q", got)
	}
}
