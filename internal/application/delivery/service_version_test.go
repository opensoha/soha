package delivery

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type versionedDraftServices struct {
	stubApplicationReader
	input     *domainapp.ServiceInput
	listCalls int
}

func (s *versionedDraftServices) ListServices(_ context.Context, _ domainidentity.Principal, _ string) ([]domainapp.Service, error) {
	s.listCalls++
	return s.services, nil
}

func (s *versionedDraftServices) UpdateService(_ context.Context, _ domainidentity.Principal, _ string, id string, input domainapp.ServiceInput) (domainapp.Service, error) {
	item := s.services[0]
	if id != item.ID || input.ExpectedVersion == nil || *input.ExpectedVersion != item.Version {
		return domainapp.Service{}, apperrors.ErrConflict
	}
	s.input = &input
	item.DeploymentTemplate = input.DeploymentTemplate
	return item, nil
}

func TestDraftServiceTemplateUsesExpectedVersion(t *testing.T) {
	for _, id := range []string{"", "service-1"} {
		t.Run("id="+id, func(t *testing.T) {
			apps := &versionedDraftServices{stubApplicationReader: stubApplicationReader{
				app:      domainapp.App{ID: "app-1", Key: "api", Name: "API", Version: 1},
				services: []domainapp.Service{{ID: "service-1", Key: "api", Version: 4}},
			}}
			repo := &draftRepository{}
			service := New(apps, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil, deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate))
			version := int64(4)
			input := domaindelivery.DeliveryDraftInput{ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Name: "API", Key: "api"}, Services: []domaindelivery.DeliveryDraftService{{ID: id, Key: "api", Name: "API", ExpectedVersion: &version, DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: "http", Version: 2}}}}
			draft, err := service.CreateDeliveryDraft(context.Background(), deliveryActionPrincipal(), input)
			if err != nil {
				t.Fatal(err)
			}
			if apps.listCalls != 0 {
				t.Fatal("draft creation must not require service list permission")
			}
			if draft.Services[0].ExpectedVersion == nil || *draft.Services[0].ExpectedVersion != 4 {
				t.Fatalf("service version not preserved: %+v", draft.Services[0])
			}
			apps.services[0].Version = 5
			if _, err := service.upsertApplicationServices(context.Background(), deliveryActionPrincipal(), "app-1", draft.Services); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("stale service accepted: %v", err)
			}
			apps.services[0].Version = 4
			if _, err := service.ConfirmDeliveryDraft(context.Background(), deliveryActionPrincipal(), draft.ID); err != nil {
				t.Fatal(err)
			}
			if apps.input == nil || apps.input.DeploymentTemplate == nil || apps.input.DeploymentTemplate.TemplateID != "http" {
				t.Fatalf("template lost at confirmation: %+v", apps.input)
			}
		})
	}
}
