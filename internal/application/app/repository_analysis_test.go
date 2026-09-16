package app

import (
	"context"
	"errors"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type analysisRepositoryCatalog struct {
	RepositoryCatalog
	item domainapp.SourceRepository
}

func (r analysisRepositoryCatalog) GetRepository(context.Context, string) (domainapp.SourceRepository, error) {
	return r.item, nil
}

type captureRepositoryAnalyzer struct{ calls int }

type sourceMappingPermissions struct{}

func (sourceMappingPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"source-admin": {"settings.system-integrations.update"}}, nil
}

func (r *captureRepositoryAnalyzer) ValidateSourceRepositoryBinding(context.Context, domainapp.SourceRepositoryInput) error {
	r.calls++
	return nil
}
func (r *captureRepositoryAnalyzer) AnalyzeSourceRepository(context.Context, domainapp.SourceRepository, sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error) {
	r.calls++
	return sohaapi.RepositoryAnalysis{Status: sohaapi.AnalysisIdentified}, nil
}

func TestRepositoryAnalysisRequiresExplicitGrantWithoutIntegrationManagement(t *testing.T) {
	ctx := context.Background()
	principal := domainidentity.Principal{UserID: "developer"}
	analyzer := &captureRepositoryAnalyzer{}
	catalog := analysisRepositoryCatalog{item: domainapp.SourceRepository{ID: "repo", SourceConnectionID: "private", ProviderRepositoryID: "42", ApplicationIDs: []string{"app"}}}
	s := New(&captureAppRepository{items: map[string]domainapp.App{"app": {ID: "app"}}}, nil, &captureAppAuthorizer{allowed: true}, nil, nil)
	s.repositories, s.analyzer = catalog, analyzer
	input := sohaapi.RepositoryAnalysisInput{RepositoryID: "repo"}
	result, err := s.AnalyzeRepository(ctx, principal, "app", input)
	if err != nil || result.ApplicationID != "app" || analyzer.calls != 1 {
		t.Fatalf("authorized analysis: %+v %v", result, err)
	}
	// No integration permission resolver exists here. Reading an existing grant
	// is allowed; creating a grant or changing its connection must fail closed.
	if err := s.authorizeRepositoryAssociations(ctx, principal, []string{"repo"}, nil); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("self-grant: %v", err)
	}
	if err := s.authorizeRepositoryAssociations(ctx, principal, []string{"repo"}, []string{"repo"}); err != nil {
		t.Fatalf("unchanged association: %v", err)
	}
	change := domainapp.SourceRepositoryInput{SourceConnectionID: "other", ProviderRepositoryID: "42", ApplicationIDs: []string{"app"}}
	if err := s.validateRepositoryBindingChange(ctx, principal, change, catalog.item); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("connection replacement: %v", err)
	}
	catalog.item.ApplicationIDs = nil
	s.repositories = catalog
	if _, err := s.AnalyzeRepository(ctx, principal, "app", input); !errors.Is(err, apperrors.ErrAccessDenied) || analyzer.calls != 1 {
		t.Fatalf("unassociated read: %v calls=%d", err, analyzer.calls)
	}
	if err := validateSourceRepositoryInput(domainapp.SourceRepositoryInput{SourceConnectionID: "private"}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("partial mapping: %v", err)
	}
	s.SetPermissionResolver(appaccess.NewPermissionResolver(sourceMappingPermissions{}))
	principal.Roles = []string{"source-admin"}
	if err := s.authorizeRepositoryAssociations(ctx, principal, []string{"repo"}, nil); err != nil {
		t.Fatalf("source administrator could not grant an association: %v", err)
	}
	if err := s.validateRepositoryBindingChange(ctx, principal, change, catalog.item); err != nil || analyzer.calls != 2 {
		t.Fatalf("source administrator could not validate a mapping: %v calls=%d", err, analyzer.calls)
	}
}
