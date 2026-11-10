package manifest

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
	"testing"
)

type manifestReaderRole struct{}

func (manifestReaderRole) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"custom-reader": {appaccess.PermDeliveryApplicationsView}}, nil
}

type manifestPublisherRoles struct{}

func (manifestPublisherRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"publisher": {"delivery.manifest-deployments.trigger"}, "reader": {appaccess.PermDeliveryApplicationsView}, "preflight": {"delivery.manifest-deployments.preflight"}}, nil
}

type preflightOnlyAuthorizer struct{ requests []domainaccess.Request }

func (a *preflightOnlyAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	return domainaccess.Decision{Allowed: request.PermissionKey == "delivery.manifest-deployments.preflight"}, nil
}

func TestPreflightOnlyPlanDoesNotRequireDeployPermission(t *testing.T) {
	s, _, target, selectVersion := newMigrationSnapshotFixture()
	selectVersion(2)
	auth := &preflightOnlyAuthorizer{}
	s.base.authorizer = auth
	s.base.permissions = appaccess.NewPermissionResolver(manifestPublisherRoles{})
	p := domainidentity.Principal{UserID: "publisher", Roles: []string{"preflight"}}
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), p, "payments", "payments-dev", target, 1, "preview-only", domainmanifest.DeliveryArtifacts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(auth.requests) != 2 {
		t.Fatalf("expected package and target preflight checks, got %d", len(auth.requests))
	}
	if err := s.ValidateDeliverySnapshot(t.Context(), p, snapshot); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("preflight-only identity must not confirm deployment: %v", err)
	}
}

type manifestPublisherGrants struct{}

type manifestPublisherPolicy struct{}

func (manifestPublisherPolicy) ListRoleCapabilities(context.Context) (map[string][]domainaccess.Action, error) {
	return map[string][]domainaccess.Action{"publisher": {}}, nil
}

func (manifestPublisherPolicy) ListPolicies(context.Context) ([]domainaccess.Policy, error) {
	return []domainaccess.Policy{{ID: "publisher", Effect: domainaccess.EffectAllow, Subjects: domainaccess.Matcher{Users: []string{"publisher"}}, Clusters: domainaccess.ClusterMatcher{IDs: []string{"cluster"}}, Namespaces: domainaccess.NamespaceMatcher{Names: []string{"payments"}}, Actions: []domainaccess.Action{domainaccess.ActionTrigger}, Reason: "test publisher ABAC"}}, nil
}

func (manifestPublisherGrants) List(context.Context) ([]domainscopegrant.Record, error) {
	return []domainscopegrant.Record{{SubjectType: "user", SubjectID: "publisher", ScopeType: "delivery", BusinessLineID: "finance", ApplicationIDs: []string{"payments"}, EnvironmentIDs: []string{"dev"}, ClusterIDs: []string{"cluster"}, Namespaces: []string{"payments"}, Role: "publisher", Enabled: true}}, nil
}

func TestPermissionOnlyManifestTriggerRetainsTargetScope(t *testing.T) {
	for _, tc := range []struct {
		name, role, application, environment, cluster, namespace string
		allowed                                                  bool
	}{
		{"publisher", "publisher", "payments", "dev", "cluster", "payments", true},
		{"reader", "reader", "payments", "dev", "cluster", "payments", false},
		{"other application", "publisher", "outside", "dev", "cluster", "payments", false},
		{"other environment", "publisher", "payments", "prod", "cluster", "payments", false},
		{"other cluster", "publisher", "payments", "dev", "outside", "payments", false},
		{"other namespace", "publisher", "payments", "dev", "cluster", "outside", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := appaccess.NewPermissionResolver(manifestPublisherRoles{})
			auth := appaccess.New(policy.NewEngine(), manifestPublisherPolicy{}, manifestPublisherGrants{}, nil)
			auth.SetPermissionResolver(resolver)
			s := &Service{authorizer: auth}
			connection := &domaincluster.Connection{Summary: domaincluster.Summary{ID: tc.cluster}}
			err := s.authorizeManifest(context.Background(), domainidentity.Principal{UserID: "publisher", Roles: []string{tc.role}}, domainaccess.ActionTrigger, domainmanifest.Package{}, domainapp.App{ID: tc.application, BusinessLineID: "finance"}, connection, tc.environment, tc.namespace, "")
			if tc.allowed && err != nil {
				t.Fatal(err)
			}
			if !tc.allowed && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("expected denied, got %v", err)
			}
		})
	}
}

type manifestScopeGrants struct{ applicationID string }

func (g manifestScopeGrants) List(context.Context) ([]domainscopegrant.Record, error) {
	return []domainscopegrant.Record{{SubjectType: "user", SubjectID: "reader", ScopeType: "delivery", BusinessLineID: "finance", ApplicationIDs: []string{g.applicationID}, Role: "custom-reader", Enabled: true}}, nil
}
func TestPermissionOnlyManifestReadRetainsApplicationScope(t *testing.T) {
	for _, appID := range []string{"payments", "outside"} {
		t.Run(appID, func(t *testing.T) {
			resolver := appaccess.NewPermissionResolver(manifestReaderRole{})
			auth := appaccess.New(policy.NewEngine(), nil, manifestScopeGrants{applicationID: appID}, nil)
			auth.SetPermissionResolver(resolver)
			service := New(&testRepository{item: domainmanifest.Package{ID: "config", ApplicationID: "payments"}}, testApplications{}, nil, nil, auth, resolver, nil, nil)
			_, err := service.Get(context.Background(), domainidentity.Principal{UserID: "reader", Roles: []string{"custom-reader"}}, "config")
			if appID == "payments" && err != nil {
				t.Fatal(err)
			}
			if appID != "payments" && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("outside scope: %v", err)
			}
		})
	}
}

type adoptionScopeRoles struct{}

func (adoptionScopeRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"adopter": {appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationServicesView, "delivery.manifest-deployments.update"}, "app-shell": {appaccess.PermDeliveryApplicationsView}}, nil
}

type adoptionScopeGrants struct{ environment string }

func (g adoptionScopeGrants) List(context.Context) ([]domainscopegrant.Record, error) {
	base := domainscopegrant.Record{SubjectType: "user", SubjectID: "adopter", ScopeType: "delivery", BusinessLineID: "finance", ApplicationIDs: []string{"payments"}, Role: "app-shell", Enabled: true}
	scoped := base
	scoped.Role = "adopter"
	scoped.EnvironmentIDs = []string{g.environment}
	return []domainscopegrant.Record{base, scoped}, nil
}

type authorizedSelectionRepository struct {
	*snapshotRepository
	writes int
}

func (r *authorizedSelectionRepository) SetTemplateSelection(_ context.Context, _ string, expected int64, selection domainmanifest.TemplateSelection) error {
	if r.binding.Version != expected {
		return apperrors.ErrConflict
	}
	r.writes++
	r.binding.TemplateSelection = &selection
	return nil
}
func TestPermissionOnlyAdoptionUsesDeploymentWriteAndEnvironmentScope(t *testing.T) {
	for _, environment := range []string{"dev", "prod"} {
		t.Run(environment, func(t *testing.T) {
			service, repo, _, _ := newMigrationSnapshotFixture()
			repo.binding.EnvironmentKey = "dev"
			writer := &authorizedSelectionRepository{snapshotRepository: repo}
			service.repository = writer
			resolver := appaccess.NewPermissionResolver(adoptionScopeRoles{})
			auth := appaccess.New(policy.NewEngine(), nil, adoptionScopeGrants{environment: environment}, nil)
			auth.SetPermissionResolver(resolver)
			service.base.authorizer, service.base.permissions = auth, resolver
			selection := domainmanifest.TemplateSelection{TemplateID: "template", Version: 2, Parameters: map[string]any{}, BaseFilesDigest: TemplateFilesDigest(repo.base.item.Files), AdoptionID: "reviewed-plan"}
			if allowed, err := resolver.HasPermission(context.Background(), domainidentity.Principal{UserID: "adopter", Roles: []string{"adopter"}}, appaccess.PermDeliveryApplicationsUpdate); err != nil || allowed {
				t.Fatalf("fixture must not grant application.update: %v", err)
			}
			err := service.AdoptTemplate(context.Background(), domainidentity.Principal{UserID: "adopter", Roles: []string{"adopter"}}, repo.binding.ID, repo.binding.Version, selection)
			if environment == "dev" && (err != nil || writer.writes != 1) {
				t.Fatalf("scoped adopter without application.update: %v, writes %d", err, writer.writes)
			}
			if environment == "prod" && (!errors.Is(err, apperrors.ErrAccessDenied) || writer.writes != 0) {
				t.Fatalf("outside environment: %v, writes %d", err, writer.writes)
			}
		})
	}
}
