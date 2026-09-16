package catalog

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type catalogProjectionStub struct {
	stubCatalogRepository
	items []domaincatalog.WorkflowCatalogCandidate
}

func (r catalogProjectionStub) ListWorkflowCatalogCandidates(context.Context) ([]domaincatalog.WorkflowCatalogCandidate, error) {
	return r.items, nil
}

type catalogScopeAuthorizer struct{}

func (catalogScopeAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: request.Delivery.ApplicationID == "app-1" && request.Delivery.EnvironmentKey != "prod"}, nil
}

func TestWorkflowCatalogAuthorizationPrecedesPaginationAndFacets(t *testing.T) {
	scope := domaincatalog.WorkflowCatalogAuthorizationScope{WorkflowCatalogScope: domaincatalog.WorkflowCatalogScope{ApplicationID: "app-1", ApplicationName: "API"}, Exists: true}
	other := scope
	other.ApplicationID, other.ApplicationName = "secret-app", "Hidden application"
	dev := scope
	dev.EnvironmentID, dev.EnvironmentKey, dev.EnvironmentName = "env-dev", "dev", "Development"
	prod := dev
	prod.EnvironmentID, prod.EnvironmentKey, prod.EnvironmentName = "env-prod", "prod", "Secret production"
	makeItem := func(id, kind string, scopes ...domaincatalog.WorkflowCatalogAuthorizationScope) domaincatalog.WorkflowCatalogCandidate {
		return domaincatalog.WorkflowCatalogCandidate{WorkflowCatalogEntry: domaincatalog.WorkflowCatalogEntry{ID: id, SourceID: id, SourceKind: kind, Name: id, Enabled: true}, AuthorizationScopes: scopes}
	}
	repo := catalogProjectionStub{items: []domaincatalog.WorkflowCatalogCandidate{
		makeItem("hidden", "build_source", other),
		makeItem("build", "build_source", scope),
		makeItem("dev", "application_workflow", dev),
		makeItem("partial", "delivery_workflow", dev, prod),
		makeItem("batch", "delivery_workflow", dev),
	}}
	principal := domainidentity.Principal{Roles: []string{"admin"}}
	service := New(&repo, catalogScopeAuthorizer{}, nil, catalogPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryWorkflowsView), nil, nil)
	page, err := service.ListWorkflowCatalog(context.Background(), principal, domaincatalog.WorkflowCatalogFilter{Offset: 1, Limit: 1})
	if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].ID != "dev" {
		t.Fatalf("page = %#v, error = %v", page, err)
	}
	if len(page.Applications) != 1 || page.Applications[0].Value != "app-1" || len(page.Environments) != 1 || page.Environments[0].Value != "env-dev" {
		t.Fatalf("unauthorized facets: %#v", page)
	}
	page, err = service.ListWorkflowCatalog(context.Background(), principal, domaincatalog.WorkflowCatalogFilter{Kind: "workflow", EnvironmentID: "env-dev", Search: "batch"})
	if err != nil || page.Total != 1 || page.Items[0].ID != "batch" {
		t.Fatalf("filtered page = %#v, error = %v", page, err)
	}
	for _, permission := range []string{appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryWorkflowsView} {
		service.permissions = catalogPermissions(permission)
		page, err = service.ListWorkflowCatalog(context.Background(), principal, domaincatalog.WorkflowCatalogFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if (item.SourceKind == "build_source") != (permission == appaccess.PermDeliveryApplicationsView) {
				t.Fatalf("unexpected definition for %s: %#v", permission, item)
			}
		}
	}
	service.permissions = catalogPermissions()
	if _, err := service.ListWorkflowCatalog(context.Background(), principal, domaincatalog.WorkflowCatalogFilter{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("missing permission: %v", err)
	}
}

func TestWorkflowCatalogRejectsInvalidFiltersAndAuthorizationFailures(t *testing.T) {
	service := New(&catalogProjectionStub{}, nil, nil, catalogPermissions(appaccess.PermDeliveryApplicationsView), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}
	for _, filter := range []domaincatalog.WorkflowCatalogFilter{{Limit: 201}, {Offset: -1}, {Kind: "runtime"}} {
		if _, err := service.ListWorkflowCatalog(context.Background(), principal, filter); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("filter %#v: %v", filter, err)
		}
	}
	expected := errors.New("authorization unavailable")
	service.authorizer = errorCatalogAuthorizer{err: expected}
	_, err := service.authorizeWorkflowCatalogEntry(context.Background(), principal, domaincatalog.WorkflowCatalogCandidate{WorkflowCatalogEntry: domaincatalog.WorkflowCatalogEntry{SourceKind: "build_source"}, AuthorizationScopes: []domaincatalog.WorkflowCatalogAuthorizationScope{{Exists: true}}})
	if !errors.Is(err, expected) {
		t.Fatalf("authorization failure hidden: %v", err)
	}
}
