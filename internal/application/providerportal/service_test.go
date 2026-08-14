package providerportal

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestIdentityRuntimeCapabilitiesReportOnlyInstalledRuntimes(t *testing.T) {
	capabilities := (&Service{}).IdentityRuntimeCapabilities()
	if !capabilities.SAMLLoginSource.Available || !capabilities.SAMLApplicationProvider.Available || !capabilities.TOTP.Available || !capabilities.WebAuthn.Available || !capabilities.RecoveryCodes.Available || !capabilities.StepUp.Available || capabilities.Outpost.AgentRuntime.Available || capabilities.Outpost.KubernetesArtifact.Available {
		t.Fatalf("unavailable runtimes reported as available: %#v", capabilities)
	}
	if !capabilities.Outpost.ControlPlane.Available || !capabilities.Outpost.EmbeddedRuntime.Available {
		t.Fatalf("installed outpost runtimes reported unavailable: %#v", capabilities.Outpost)
	}
}

func TestIdentityRuntimeCapabilitiesExposeConfiguredOutpostRuntime(t *testing.T) {
	service := &Service{}
	service.SetOutpostRuntimeCapability(true, "")
	capabilities := service.IdentityRuntimeCapabilities()
	if !capabilities.Outpost.AgentRuntime.Available || !capabilities.Outpost.KubernetesArtifact.Available || !capabilities.Outpost.ExternalProtocol.Available {
		t.Fatalf("configured outpost runtimes reported unavailable: %#v", capabilities.Outpost)
	}
}

func TestServiceLaunchAllowsProviderApplications(t *testing.T) {
	ctx := context.Background()
	for _, providerType := range []string{domainportal.ProviderTypeOIDC, domainportal.ProviderTypeProxy, domainportal.ProviderTypeSAML} {
		t.Run(providerType, func(t *testing.T) {
			repo := &memoryPortalRepo{
				applications: map[string]domainportal.Application{
					"app-1": {
						ID:            "app-1",
						Slug:          "grafana",
						Name:          "Grafana",
						LaunchURL:     "https://grafana.example.com",
						ProviderID:    "provider-1",
						ProviderType:  providerType,
						PortalVisible: true,
						Status:        domainportal.ApplicationStatusEnabled,
						Assignments: []domainportal.ApplicationAssignment{{
							SubjectType: domainportal.AssignmentSubjectRole,
							SubjectID:   "admin",
							Effect:      domainportal.AssignmentEffectAllow,
						}},
					},
				},
			}
			service := New(repo, nil, nil)
			decision, err := service.Launch(ctx, domainidentity.Principal{
				UserID:   "user-1",
				UserName: "Ada",
				Roles:    []string{"admin"},
			}, "app-1")
			if err != nil {
				t.Fatalf("Launch returned error: %v", err)
			}
			if decision.Decision != "allow" || decision.ProviderType != providerType || decision.LaunchURL != "https://grafana.example.com" {
				t.Fatalf("Launch decision = %#v", decision)
			}
			if len(repo.launches) != 1 || repo.launches[0].Result != "allow" {
				t.Fatalf("recorded launches = %#v", repo.launches)
			}
		})
	}
}

func TestServiceLaunchUsesConfiguredOIDCApplicationURL(t *testing.T) {
	ctx := context.Background()
	repo := &memoryPortalRepo{
		applications: map[string]domainportal.Application{
			"app-1": {
				ID:            "app-1",
				Slug:          "grafana",
				Name:          "Grafana",
				ProviderID:    "provider-1",
				ProviderType:  domainportal.ProviderTypeOIDC,
				PortalVisible: true,
				Status:        domainportal.ApplicationStatusEnabled,
				LaunchURL:     "https://grafana.example.com/login",
			},
		},
	}
	service := New(repo, nil, nil)

	decision, err := service.Launch(ctx, domainidentity.Principal{
		UserID:   "user-1",
		UserName: "Ada",
	}, "app-1")
	if err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	if decision.LaunchURL != "https://grafana.example.com/login" || decision.ProviderType != domainportal.ProviderTypeOIDC {
		t.Fatalf("Launch decision = %#v", decision)
	}
	if len(repo.launches) != 1 || repo.launches[0].LaunchURL != decision.LaunchURL {
		t.Fatalf("recorded launches = %#v", repo.launches)
	}
}

func TestServicePortalApplicationsRedactAssignments(t *testing.T) {
	ctx := context.Background()
	repo := &memoryPortalRepo{
		applications: map[string]domainportal.Application{
			"app-1": {
				ID:            "app-1",
				Name:          "Grafana",
				ProviderType:  domainportal.ProviderTypeLink,
				PortalVisible: true,
				Status:        domainportal.ApplicationStatusEnabled,
				Assignments: []domainportal.ApplicationAssignment{{
					SubjectType: domainportal.AssignmentSubjectRole,
					SubjectID:   "admin",
					Effect:      domainportal.AssignmentEffectAllow,
				}},
			},
		},
	}
	service := New(repo, nil, nil)
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}}

	items, err := service.ListPortalApplications(ctx, principal)
	if err != nil {
		t.Fatalf("ListPortalApplications returned error: %v", err)
	}
	item, err := service.GetPortalApplication(ctx, principal, "app-1")
	if err != nil {
		t.Fatalf("GetPortalApplication returned error: %v", err)
	}
	if len(items) != 1 || len(items[0].Assignments) != 0 || len(item.Assignments) != 0 {
		t.Fatalf("portal assignments were exposed: list=%#v detail=%#v", items, item)
	}
	if len(repo.applications["app-1"].Assignments) != 1 {
		t.Fatal("portal redaction mutated the stored application")
	}
}

func TestServicePortalApplicationsUseAccessPolicyContext(t *testing.T) {
	repo := &memoryPortalRepo{applications: map[string]domainportal.Application{
		"app-1": {
			ID: "app-1", Name: "Grafana", ProviderType: domainportal.ProviderTypeLink,
			PortalVisible: true, Status: domainportal.ApplicationStatusEnabled,
			Metadata: map[string]any{"accessPolicy": map[string]any{
				"requireMfa": true, "allowedCidrs": []string{"10.0.0.0/8"},
				"startTimeUtc": "22:00", "endTimeUtc": "02:00",
			}},
		},
	}}
	service := New(repo, nil, nil)
	principal := domainidentity.Principal{UserID: "user-1"}
	ctx := WithAccessPolicyContext(context.Background(), domainportal.AccessPolicyContext{
		SourceIP: "10.1.2.3", MFAAuthenticated: true,
		Now: time.Date(2026, 7, 25, 23, 0, 0, 0, time.UTC),
	})

	items, err := service.ListPortalApplications(ctx, principal)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListPortalApplications = %#v, %v", items, err)
	}
	if _, err := service.GetPortalApplication(ctx, principal, "app-1"); err != nil {
		t.Fatalf("GetPortalApplication returned error: %v", err)
	}
	if items, err := service.ListPortalApplications(context.Background(), principal); err != nil || len(items) != 0 {
		t.Fatalf("ListPortalApplications without context = %#v, %v", items, err)
	}
}

func TestServiceUpdatePolicyReplacesAssignments(t *testing.T) {
	ctx := context.Background()
	repo := &memoryPortalRepo{
		applications: map[string]domainportal.Application{
			"app-1": {
				ID:            "app-1",
				Slug:          "grafana",
				Name:          "Grafana",
				ProviderType:  domainportal.ProviderTypeLink,
				PortalVisible: true,
				Status:        domainportal.ApplicationStatusEnabled,
				Assignments: []domainportal.ApplicationAssignment{{
					ID:            "old",
					ApplicationID: "app-1",
					SubjectType:   domainportal.AssignmentSubjectRole,
					SubjectID:     "old-role",
					Effect:        domainportal.AssignmentEffectAllow,
				}},
			},
		},
	}
	service := New(repo, appaccess.NewPermissionResolver(staticRolePermissions{
		matrix: map[string][]string{
			"admin": {appaccess.PermIdentityPoliciesManage},
		},
	}), nil)

	policy, err := service.UpdatePolicy(ctx, domainidentity.Principal{
		UserID:   "admin-1",
		UserName: "Admin",
		Roles:    []string{"admin"},
	}, "app-1", domainportal.ApplicationPolicyInput{
		Assignments: []domainportal.ApplicationAssignmentInput{
			{
				SubjectType: domainportal.AssignmentSubjectTeam,
				SubjectID:   "platform",
				Effect:      domainportal.AssignmentEffectAllow,
			},
			{
				SubjectType: domainportal.AssignmentSubjectRole,
				SubjectID:   "ops",
				Effect:      domainportal.AssignmentEffectAllow,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdatePolicy returned error: %v", err)
	}
	if policy.ApplicationID != "app-1" || policy.ApplicationName != "Grafana" {
		t.Fatalf("policy identity = %#v", policy)
	}
	if len(policy.Assignments) != 2 {
		t.Fatalf("assignments len = %d, want 2: %#v", len(policy.Assignments), policy.Assignments)
	}
	if got := repo.applications["app-1"].Assignments[0].ApplicationID; got != "app-1" {
		t.Fatalf("assignment application id = %q, want app-1", got)
	}
}

func TestServiceSecuritySummaryUsesProfileReader(t *testing.T) {
	ctx := context.Background()
	lastLoginAt := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	service := New(&memoryPortalRepo{}, nil, nil)
	service.SetProfileReader(staticProfileReader{
		profile: domainidentity.UserProfile{
			UserID:      "user-1",
			Username:    "ada",
			DisplayName: "Ada Lovelace",
			Email:       "ada@example.com",
			Roles:       []string{"admin"},
			Teams:       []string{"platform"},
			Projects:    []string{"soha"},
			Tags:        []string{"staff"},
			Identities: []domainidentity.LinkedIdentity{
				{ProviderType: "oidc", ProviderID: "github"},
				{ProviderType: "password"},
			},
			Sessions: []domainidentity.SessionRecord{
				{ID: "session-1"},
				{ID: "session-2"},
			},
			LastLoginAt: &lastLoginAt,
		},
	})

	summary := service.SecuritySummary(ctx, domainidentity.Principal{UserID: "user-1", UserName: "stale"})
	if summary.Principal.UserName != "Ada Lovelace" || summary.Principal.Email != "ada@example.com" {
		t.Fatalf("summary principal = %#v", summary.Principal)
	}
	if summary.ActiveSession != 2 {
		t.Fatalf("active sessions = %d, want 2", summary.ActiveSession)
	}
	if summary.RecentLoginAt == nil || !summary.RecentLoginAt.Equal(lastLoginAt) {
		t.Fatalf("recent login = %v, want %v", summary.RecentLoginAt, lastLoginAt)
	}
	if got, want := fmt.Sprint(summary.LinkedSources), "[oidc:github password]"; got != want {
		t.Fatalf("linked sources = %s, want %s", got, want)
	}
}

func TestServiceSecuritySummaryFallsBackWhenProfileReaderFails(t *testing.T) {
	service := New(&memoryPortalRepo{}, nil, nil)
	service.SetProfileReader(staticProfileReader{err: errors.New("profile unavailable")})

	summary := service.SecuritySummary(context.Background(), domainidentity.Principal{
		UserID:   "user-1",
		UserName: "Ada",
	})
	if summary.Principal.UserName != "Ada" || summary.ActiveSession != 1 || len(summary.LinkedSources) != 0 {
		t.Fatalf("fallback summary = %#v", summary)
	}
}

func TestServiceCreateApplicationRejectsLinkProviderBinding(t *testing.T) {
	service := New(&memoryPortalRepo{}, portalApplicationManagePermissions(), nil)

	_, err := service.CreateApplication(context.Background(), domainidentity.Principal{
		UserID: "admin-1",
		Roles:  []string{"admin"},
	}, domainportal.ApplicationInput{
		Name:         "Docs",
		LaunchURL:    "/docs",
		ProviderID:   "provider-1",
		ProviderType: domainportal.ProviderTypeLink,
		Status:       domainportal.ApplicationStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateApplication error = %v, want invalid argument", err)
	}
}

func TestServiceCreateApplicationRejectsProviderBindingBeforeApplicationExists(t *testing.T) {
	service := New(&memoryPortalRepo{
		providerBindings: map[string]providerBinding{
			"provider-1": {applicationID: "app-1", providerType: domainportal.ProviderTypeOIDC},
		},
	}, portalApplicationManagePermissions(), nil)

	_, err := service.CreateApplication(context.Background(), domainidentity.Principal{
		UserID: "admin-1",
		Roles:  []string{"admin"},
	}, domainportal.ApplicationInput{
		Name:         "Grafana",
		ProviderID:   "provider-1",
		ProviderType: domainportal.ProviderTypeOIDC,
		Status:       domainportal.ApplicationStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateApplication error = %v, want invalid argument", err)
	}
}

func TestServiceUpdateApplicationRejectsMismatchedProviderBinding(t *testing.T) {
	repo := &memoryPortalRepo{
		applications: map[string]domainportal.Application{
			"app-1": {
				ID:           "app-1",
				Slug:         "grafana",
				Name:         "Grafana",
				ProviderType: domainportal.ProviderTypeOIDC,
				Status:       domainportal.ApplicationStatusDraft,
			},
		},
		providerBindings: map[string]providerBinding{
			"provider-1": {applicationID: "other-app", providerType: domainportal.ProviderTypeOIDC},
			"provider-2": {applicationID: "app-1", providerType: domainportal.ProviderTypeProxy},
		},
	}
	service := New(repo, portalApplicationManagePermissions(), nil)
	principal := domainidentity.Principal{UserID: "admin-1", Roles: []string{"admin"}}

	for _, providerID := range []string{"provider-1", "provider-2"} {
		t.Run(providerID, func(t *testing.T) {
			_, err := service.UpdateApplication(context.Background(), principal, "app-1", domainportal.ApplicationInput{
				Name:         "Grafana",
				ProviderID:   providerID,
				ProviderType: domainportal.ProviderTypeOIDC,
				Status:       domainportal.ApplicationStatusEnabled,
			})
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("UpdateApplication error = %v, want invalid argument", err)
			}
		})
	}
}

func TestServiceUpdateApplicationAllowsMatchingProviderBinding(t *testing.T) {
	repo := &memoryPortalRepo{
		applications: map[string]domainportal.Application{
			"app-1": {
				ID:           "app-1",
				Slug:         "grafana",
				Name:         "Grafana",
				ProviderType: domainportal.ProviderTypeOIDC,
				Status:       domainportal.ApplicationStatusDraft,
			},
		},
		providerBindings: map[string]providerBinding{
			"provider-1": {applicationID: "app-1", providerType: domainportal.ProviderTypeOIDC},
		},
	}
	service := New(repo, portalApplicationManagePermissions(), nil)

	item, err := service.UpdateApplication(context.Background(), domainidentity.Principal{
		UserID: "admin-1",
		Roles:  []string{"admin"},
	}, "app-1", domainportal.ApplicationInput{
		Name:         "Grafana",
		ProviderID:   "provider-1",
		ProviderType: domainportal.ProviderTypeOIDC,
		Status:       domainportal.ApplicationStatusEnabled,
	})
	if err != nil {
		t.Fatalf("UpdateApplication returned error: %v", err)
	}
	if item.ProviderID != "provider-1" || item.ProviderType != domainportal.ProviderTypeOIDC {
		t.Fatalf("updated application = %#v", item)
	}
}

type memoryPortalRepo struct {
	applications     map[string]domainportal.Application
	providerBindings map[string]providerBinding
	launches         []domainportal.ApplicationLaunch
}

type providerBinding struct {
	applicationID string
	providerType  string
}

func (r *memoryPortalRepo) ListApplications(context.Context, domainportal.ApplicationFilter) ([]domainportal.Application, error) {
	return r.allApplications(), nil
}

func (r *memoryPortalRepo) ListPortalApplications(context.Context) ([]domainportal.Application, error) {
	items := make([]domainportal.Application, 0)
	for _, item := range r.allApplications() {
		if item.PortalVisible && item.Status == domainportal.ApplicationStatusEnabled {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *memoryPortalRepo) GetApplication(_ context.Context, applicationID string) (domainportal.Application, error) {
	for _, item := range r.applications {
		if item.ID == applicationID || item.Slug == applicationID {
			return item, nil
		}
	}
	return domainportal.Application{}, fmt.Errorf("%w: application not found", apperrors.ErrNotFound)
}

func (r *memoryPortalRepo) CreateApplication(_ context.Context, item domainportal.Application) (domainportal.Application, error) {
	if r.applications == nil {
		r.applications = map[string]domainportal.Application{}
	}
	r.applications[item.ID] = item
	return item, nil
}

func (r *memoryPortalRepo) CreateApplicationWithAssignments(ctx context.Context, item domainportal.Application, assignments []domainportal.ApplicationAssignment) (domainportal.Application, error) {
	if r.applications == nil {
		r.applications = map[string]domainportal.Application{}
	}
	if _, ok := r.applications[item.ID]; ok {
		return domainportal.Application{}, fmt.Errorf("%w: application already exists", apperrors.ErrInvalidArgument)
	}
	r.applications[item.ID] = item
	if err := r.ReplaceAssignments(ctx, item.ID, assignments); err != nil {
		delete(r.applications, item.ID)
		return domainportal.Application{}, err
	}
	return r.GetApplication(ctx, item.ID)
}

func (r *memoryPortalRepo) UpdateApplication(_ context.Context, item domainportal.Application) (domainportal.Application, error) {
	if _, ok := r.applications[item.ID]; !ok {
		return domainportal.Application{}, fmt.Errorf("%w: application not found", apperrors.ErrNotFound)
	}
	r.applications[item.ID] = item
	return item, nil
}

func (r *memoryPortalRepo) UpdateApplicationWithAssignments(ctx context.Context, item domainportal.Application, assignments []domainportal.ApplicationAssignment) (domainportal.Application, error) {
	current, ok := r.applications[item.ID]
	if !ok {
		return domainportal.Application{}, fmt.Errorf("%w: application not found", apperrors.ErrNotFound)
	}
	r.applications[item.ID] = item
	if err := r.ReplaceAssignments(ctx, item.ID, assignments); err != nil {
		r.applications[item.ID] = current
		return domainportal.Application{}, err
	}
	return r.GetApplication(ctx, item.ID)
}

func (r *memoryPortalRepo) DeleteApplication(context.Context, string) error {
	return nil
}

func (r *memoryPortalRepo) ValidateProviderBinding(_ context.Context, providerID, applicationID, providerType string) error {
	binding, ok := r.providerBindings[providerID]
	if !ok || binding.applicationID != applicationID || binding.providerType != providerType {
		return fmt.Errorf("%w: identity provider is not bound to application", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (r *memoryPortalRepo) ReplaceAssignments(_ context.Context, applicationID string, assignments []domainportal.ApplicationAssignment) error {
	item, ok := r.applications[applicationID]
	if !ok {
		return fmt.Errorf("%w: application not found", apperrors.ErrNotFound)
	}
	item.Assignments = append([]domainportal.ApplicationAssignment(nil), assignments...)
	r.applications[applicationID] = item
	return nil
}

func (r *memoryPortalRepo) ListAssignments(context.Context, []string) (map[string][]domainportal.ApplicationAssignment, error) {
	return map[string][]domainportal.ApplicationAssignment{}, nil
}

func (r *memoryPortalRepo) ListFavoriteApplicationIDs(context.Context, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (r *memoryPortalRepo) SetFavorite(context.Context, string, string) error {
	return nil
}

func (r *memoryPortalRepo) DeleteFavorite(context.Context, string, string) error {
	return nil
}

func (r *memoryPortalRepo) ListRecentLaunches(context.Context, string, int) ([]domainportal.ApplicationLaunch, error) {
	return nil, nil
}

func (r *memoryPortalRepo) GetLastLaunches(context.Context, string) (map[string]time.Time, error) {
	return map[string]time.Time{}, nil
}

func (r *memoryPortalRepo) RecordLaunch(_ context.Context, launch domainportal.ApplicationLaunch) error {
	r.launches = append(r.launches, launch)
	return nil
}

func (r *memoryPortalRepo) allApplications() []domainportal.Application {
	items := make([]domainportal.Application, 0, len(r.applications))
	for _, item := range r.applications {
		items = append(items, item)
	}
	return items
}

type staticRolePermissions struct {
	matrix map[string][]string
}

func (s staticRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func portalApplicationManagePermissions() *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(staticRolePermissions{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermIdentityApplicationsManage,
			},
		},
	})
}

type staticProfileReader struct {
	profile domainidentity.UserProfile
	err     error
}

func (s staticProfileReader) CurrentProfile(context.Context, domainidentity.Principal) (domainidentity.UserProfile, error) {
	return s.profile, s.err
}
