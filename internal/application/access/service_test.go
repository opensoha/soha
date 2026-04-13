package access

import (
	"context"
	"testing"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainscopegrant "github.com/kubecrux/kubecrux/internal/domain/scopegrant"
	"github.com/kubecrux/kubecrux/internal/policy"
)

type stubScopeGrantReader struct {
	items []domainscopegrant.Record
}

func (s stubScopeGrantReader) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.items, nil
}

type stubCatalogReader struct {
	environments            []domaincatalog.Environment
	applicationEnvironments []domaincatalog.ApplicationEnvironment
}

func (s stubCatalogReader) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return s.environments, nil
}

func (s stubCatalogReader) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.applicationEnvironments, nil
}

func TestAuthorizeAppliesScopeGrantToPlatformNamespaces(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "team",
				SubjectID:      "team-a",
				BusinessLineID: "bl-retail",
				EnvironmentIDs: []string{"env-dev"},
				Role:           "developer",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			environments: []domaincatalog.Environment{
				{ID: "env-dev", Key: "dev"},
			},
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					EnvironmentID:  "env-dev",
					EnvironmentKey: "dev",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
						{ClusterID: "cluster-a", Namespace: "erp-api", WorkloadKind: "Deployment", WorkloadName: "erp-api-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
			Teams:  []string{"team-a"},
		},
		Action: domainaccess.ActionList,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
			Teams:  []string{"team-a"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   "cluster-a",
			Environment: "production",
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Namespace"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
	if decision.ResourceScope == nil {
		t.Fatalf("decision.ResourceScope = nil, want namespace scope")
	}
	if len(decision.ResourceScope.Namespaces) != 2 {
		t.Fatalf("decision.ResourceScope.Namespaces = %v, want 2 namespaces", decision.ResourceScope.Namespaces)
	}
}

func TestAuthorizeDeniesPlatformClusterOutsideScopeGrant(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "bl-retail",
				Role:           "readonly",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Action: domainaccess.ActionView,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID: "cluster-b",
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Cluster", Name: "cluster-b"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision.Allowed = true, want false")
	}
}

func TestAuthorizeFiltersPlatformActionsByScopeGrantRole(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "bl-retail",
				Role:           "readonly",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Action: domainaccess.ActionDelete,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID: "cluster-a",
		},
		Namespace: domainaccess.NamespaceAttributes{Namespace: "erp-front"},
		Resource:  domainaccess.ResourceAttributes{Kind: "Namespace"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision.Allowed = true, want false for readonly platform scope")
	}
}
