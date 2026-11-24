package app

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
)

type captureAppRepository struct {
	items                    map[string]domainapp.App
	serviceIdentityReadAppID string
}

func (r *captureAppRepository) List(context.Context, domainapp.Filter) ([]domainapp.App, error) {
	items := make([]domainapp.App, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	return items, nil
}

func (r *captureAppRepository) Get(_ context.Context, id string) (domainapp.App, error) {
	if r.items == nil {
		return domainapp.App{}, nil
	}
	return r.items[id], nil
}

func (r *captureAppRepository) Create(_ context.Context, input domainapp.UpsertInput) (domainapp.App, error) {
	if r.items == nil {
		r.items = map[string]domainapp.App{}
	}
	id := input.ID
	if id == "" {
		id = "app-created"
	}
	item := domainapp.App{
		ID:             id,
		Name:           input.Name,
		Key:            input.Key,
		Group:          input.Group,
		BusinessLineID: input.BusinessLineID,
		Language:       input.Language,
	}
	r.items[item.ID] = item
	return item, nil
}

func (r *captureAppRepository) Update(_ context.Context, id string, input domainapp.UpsertInput) (domainapp.App, error) {
	item := domainapp.App{
		ID:             id,
		Name:           input.Name,
		Key:            input.Key,
		Group:          input.Group,
		BusinessLineID: input.BusinessLineID,
		Language:       input.Language,
	}
	r.items[id] = item
	return item, nil
}

func (r *captureAppRepository) Delete(context.Context, string) error {
	return nil
}

func (r *captureAppRepository) ListServices(context.Context, string) ([]domainapp.Service, error) {
	return nil, nil
}

func (r *captureAppRepository) ListServiceIdentities(_ context.Context, applicationID string) ([]domainapp.ServiceIdentity, error) {
	r.serviceIdentityReadAppID = applicationID
	return []domainapp.ServiceIdentity{{ID: "service-1", ApplicationID: applicationID}}, nil
}

func (r *captureAppRepository) GetService(context.Context, string, string) (domainapp.Service, error) {
	return domainapp.Service{}, nil
}

func (r *captureAppRepository) CreateService(context.Context, string, domainapp.ServiceInput) (domainapp.Service, error) {
	return domainapp.Service{}, nil
}

func (r *captureAppRepository) UpdateService(context.Context, string, string, domainapp.ServiceInput) (domainapp.Service, error) {
	return domainapp.Service{}, nil
}

func (r *captureAppRepository) DeleteService(context.Context, string, string) error {
	return nil
}

type captureAppAuthorizer struct {
	requests []domainaccess.Request
	allowed  bool
}

type serviceListPermissionReader struct{}

func (serviceListPermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"app-reader":     {appaccess.PermDeliveryApplicationsView},
		"service-reader": {appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationServicesView},
	}, nil
}

func TestListServiceIdentitiesRequiresServicePermissionAndApplicationScope(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		inScope    bool
	}{
		{name: "missing service permission", role: "app-reader", inScope: true},
		{name: "outside application scope", role: "service-reader"},
		{name: "authorized", role: "service-reader", inScope: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &captureAppRepository{items: map[string]domainapp.App{
				"app-1": {ID: "app-1", Name: "Example", BusinessLineID: "retail"},
			}}
			auth := &captureAppAuthorizer{allowed: tc.inScope}
			service := New(repo, nil, auth, nil, nil)
			service.SetPermissionResolver(appaccess.NewPermissionResolver(serviceListPermissionReader{}))
			principal := domainidentity.Principal{UserID: "reader", Roles: []string{tc.role}}
			items, err := service.ListServiceIdentities(t.Context(), principal, " app-1 ")
			if tc.role != "service-reader" || !tc.inScope {
				if !errors.Is(err, apperrors.ErrAccessDenied) || repo.serviceIdentityReadAppID != "" {
					t.Fatalf("denied service identity read = %+v, %v, query scope %q", items, err, repo.serviceIdentityReadAppID)
				}
				return
			}
			if err != nil || len(items) != 1 || items[0].ID != "service-1" || repo.serviceIdentityReadAppID != "app-1" {
				t.Fatalf("authorized service identity read = %+v, %v, query scope %q", items, err, repo.serviceIdentityReadAppID)
			}
			if len(auth.requests) != 1 || auth.requests[0].Delivery.ApplicationID != "app-1" ||
				auth.requests[0].Delivery.BusinessLineID != "retail" || auth.requests[0].Action != domainaccess.ActionView {
				t.Fatalf("unexpected application scope check: %+v", auth.requests)
			}
		})
	}
}

type appScopeGrantReader struct {
	items []domainscopegrant.Record
}

func (s appScopeGrantReader) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.items, nil
}

func (a *captureAppAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	return domainaccess.Decision{Allowed: a.allowed, Reason: "test authorizer"}, nil
}

func TestCreateApplicationUsesCreateActionForScopeAuthorization(t *testing.T) {
	repo := &captureAppRepository{}
	authorizer := &captureAppAuthorizer{allowed: true}
	service := New(repo, nil, authorizer, nil, nil)

	item, err := service.Create(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"ops"}}, validAppInput("payments"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if item.ID == "" {
		t.Fatalf("expected created app, got %#v", item)
	}
	if len(authorizer.requests) != 1 {
		t.Fatalf("expected one authorization request, got %#v", authorizer.requests)
	}
	request := authorizer.requests[0]
	if request.Action != domainaccess.ActionCreate {
		t.Fatalf("authorization action = %q, want %q", request.Action, domainaccess.ActionCreate)
	}
	if request.Delivery.BusinessLineID != "retail" || request.Delivery.ApplicationGroup != "core, retail" || request.Resource.Kind != "Application" {
		t.Fatalf("authorization scope = %#v resource=%#v", request.Delivery, request.Resource)
	}
}

func TestCreateApplicationAllowsBoundaryOnlyInput(t *testing.T) {
	repo := &captureAppRepository{}
	service := New(repo, nil, &captureAppAuthorizer{allowed: true}, nil, nil)

	item, err := service.Create(context.Background(), domainidentity.Principal{UserID: "user-1"}, domainapp.UpsertInput{
		Name:    "Payments",
		Key:     "payments",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if item.ID == "" || item.Group != "" || item.Language != "" {
		t.Fatalf("created application = %#v, want boundary-only application", item)
	}
}

func TestUpdateApplicationUsesUpdateActionForScopeAuthorization(t *testing.T) {
	repo := &captureAppRepository{items: map[string]domainapp.App{
		"app-1": {ID: "app-1", Name: "Payments", Key: "payments", Group: "core, retail", BusinessLineID: "retail", Language: "go"},
	}}
	authorizer := &captureAppAuthorizer{allowed: true}
	service := New(repo, nil, authorizer, nil, nil)

	_, err := service.Update(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"ops"}}, "app-1", validAppInput("payments"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if len(authorizer.requests) != 1 {
		t.Fatalf("expected one authorization request, got %#v", authorizer.requests)
	}
	if authorizer.requests[0].Action != domainaccess.ActionUpdate {
		t.Fatalf("authorization action = %q, want %q", authorizer.requests[0].Action, domainaccess.ActionUpdate)
	}
}

func TestUpdateApplicationCannotMoveAnUnauthorizedOriginalScope(t *testing.T) {
	repo := &captureAppRepository{items: map[string]domainapp.App{"app-1": {ID: "app-1", Name: "Payments", Key: "payments", BusinessLineID: "private", Version: 1}}}
	authorizer := appaccess.New(policy.NewEngine(), nil, appScopeGrantReader{items: []domainscopegrant.Record{{ID: "grant", SubjectType: "user", SubjectID: "user-1", BusinessLineID: "retail", Role: "ops", Effect: "allow", Enabled: true}}}, nil)
	authorizer.SetPermissionResolver(appaccess.NewPermissionResolver(permissionOnlyRoleReader{}))
	service := New(repo, nil, authorizer, nil, nil)
	_, err := service.Update(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"ops"}}, "app-1", validAppInput("payments"))
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("moving a resource from an unauthorized scope: %v", err)
	}
	if repo.items["app-1"].BusinessLineID != "private" {
		t.Fatal("denied scope update was persisted")
	}
}

func TestCreateApplicationAllowsMatchingCreateScopeGrant(t *testing.T) {
	repo := &captureAppRepository{}
	authorizer := appaccess.New(policy.NewEngine(), nil, appScopeGrantReader{items: []domainscopegrant.Record{
		{
			ID:             "grant-1",
			SubjectType:    "user",
			SubjectID:      "user-1",
			BusinessLineID: "retail",
			Role:           "ops",
			Effect:         "allow",
			Enabled:        true,
		},
	}}, nil)
	authorizer.SetPermissionResolver(appaccess.NewPermissionResolver(permissionOnlyRoleReader{}))
	service := New(repo, nil, authorizer, nil, nil)

	item, err := service.Create(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"ops"}}, validAppInput("payments"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if item.ID == "" {
		t.Fatalf("expected created app, got %#v", item)
	}
}

func TestCreateApplicationDeniesScopeGrantRoleWithoutCreateAction(t *testing.T) {
	repo := &captureAppRepository{}
	authorizer := appaccess.New(policy.NewEngine(), nil, appScopeGrantReader{items: []domainscopegrant.Record{
		{
			ID:             "grant-1",
			SubjectType:    "user",
			SubjectID:      "user-1",
			BusinessLineID: "retail",
			Role:           "readonly",
			Effect:         "allow",
			Enabled:        true,
		},
	}}, nil)
	authorizer.SetPermissionResolver(appaccess.NewPermissionResolver(permissionOnlyRoleReader{}))
	service := New(repo, nil, authorizer, nil, nil)

	_, err := service.Create(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}}, validAppInput("payments"))
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Create error = %v, want access denied", err)
	}
	if len(repo.items) != 0 {
		t.Fatalf("application should not be created when scope grant denies create, got %#v", repo.items)
	}
}

func validAppInput(key string) domainapp.UpsertInput {
	return domainapp.UpsertInput{
		Name:           "Payments",
		Key:            key,
		Group:          "core, retail",
		BusinessLineID: "retail",
		Language:       "go",
		Enabled:        true,
	}
}
