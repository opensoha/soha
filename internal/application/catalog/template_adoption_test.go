package catalog

import (
	"context"
	"errors"
	"slices"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestTemplateSelectionCannotBeForgedOnCreateOrUpdate(t *testing.T) {
	input := domaincatalog.ApplicationEnvironmentInput{ApplicationID: "app", Targets: []domaincatalog.ReleaseTargetInput{{ID: "helm", Metadata: map[string]any{"templateSelection": map[string]any{"version": 4}}}}}
	current := domaincatalog.ApplicationEnvironment{ApplicationID: "app"}
	if err := validateTemplateAdoptionUpdate(context.Background(), current, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("forged creation: %v", err)
	}
	current.Targets = []domaincatalog.ReleaseTarget{{ID: input.Targets[0].ID, Metadata: input.Targets[0].Metadata}}
	if err := validateTemplateAdoptionUpdate(context.Background(), current, input); err != nil {
		t.Fatal(err)
	}
	input.Targets = []domaincatalog.ReleaseTargetInput{{ID: "helm", Metadata: map[string]any{"templateSelection": map[string]any{"version": 5}}}}
	if err := validateTemplateAdoptionUpdate(context.Background(), current, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("forged update: %v", err)
	}
	if err := validateTemplateAdoptionUpdate(domainupdate.WithAdoption(context.Background(), "deployment", "app", "plan"), current, input); err != nil {
		t.Fatal(err)
	}
}

type adoptionScopedRepository struct {
	stubCatalogRepository
	appID string
}

func (r *adoptionScopedRepository) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return nil, errors.New("unexpected unscoped lookup")
}

func (r *adoptionScopedRepository) ListApplicationEnvironmentsByApplication(_ context.Context, appID string) ([]domaincatalog.ApplicationEnvironment, error) {
	r.appID = appID
	return []domaincatalog.ApplicationEnvironment{
		{ID: "dev", ApplicationID: appID},
		{ID: "prod", ApplicationID: appID},
	}, nil
}

type templateAdoptionAuthorizer struct {
	deny bool
	seen []string
}

func (a *templateAdoptionAuthorizer) Authorize(_ context.Context, req domainaccess.Request) (domainaccess.Decision, error) {
	a.seen = append(a.seen, req.Resource.Name)
	return domainaccess.Decision{Allowed: !a.deny || req.Resource.Name != "prod"}, nil
}

func TestListTemplateAdoptionEnvironmentsAuthorizesWholeApplication(t *testing.T) {
	repo := &adoptionScopedRepository{}
	auth := &templateAdoptionAuthorizer{deny: true}
	service := New(repo, auth, nil, nil, nil, nil)
	p := domainidentity.Principal{UserID: "user"}
	items, err := service.ListTemplateAdoptionEnvironments(t.Context(), p, "app")
	if !errors.Is(err, apperrors.ErrAccessDenied) || items != nil {
		t.Fatalf("partial adoption scope was returned: items=%+v err=%v", items, err)
	}
	if repo.appID != "app" || !slices.Equal(auth.seen, []string{"dev", "prod"}) {
		t.Fatalf("adoption scope app=%q checked=%v", repo.appID, auth.seen)
	}
	auth.deny = false
	items, err = service.ListTemplateAdoptionEnvironments(t.Context(), p, "app")
	if err != nil || len(items) != 2 || items[0].ID != "dev" || items[1].ID != "prod" {
		t.Fatalf("authorized adoption scope = %+v, %v", items, err)
	}
}
