package app

import (
	"context"
	"errors"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type selectionAppRepository struct {
	domainapp.Repository
	service domainapp.Service
}

func (r selectionAppRepository) GetService(context.Context, string, string) (domainapp.Service, error) {
	return r.service, nil
}

type selectionPackageReader struct {
	bindings []domainmanifest.EnvironmentBinding
}

func (r selectionPackageReader) Get(context.Context, string) (domainmanifest.Package, error) {
	return domainmanifest.Package{}, nil
}
func (r selectionPackageReader) ListBindings(context.Context, string) ([]domainmanifest.EnvironmentBinding, error) {
	return r.bindings, nil
}

func TestEnvironmentSelectionsCannotBeLostByDetachingService(t *testing.T) {
	ref := &domaincatalog.DeploymentTemplateBinding{TemplateID: "template", Version: 1, ManifestPackageID: "package"}
	s := &Service{repo: selectionAppRepository{service: domainapp.Service{DeploymentTemplate: ref}}, deploymentPackages: selectionPackageReader{bindings: []domainmanifest.EnvironmentBinding{{TemplateSelection: &domainmanifest.TemplateSelection{TemplateID: "template", Version: 4}}}}}
	ctx := context.Background()
	for _, next := range []*domaincatalog.DeploymentTemplateBinding{nil, {TemplateID: "template", Version: 1, ManifestPackageID: "package", Detached: true}, {TemplateID: "other", Version: 1, ManifestPackageID: "other"}} {
		if err := s.preserveEnvironmentTemplateSelections(ctx, "app", "service", next); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("environment version could be discarded: %v", err)
		}
	}
	if err := s.preserveEnvironmentTemplateSelections(ctx, "app", "service", ref); err != nil {
		t.Fatal(err)
	}
	s.deploymentPackages = selectionPackageReader{}
	if err := s.preserveEnvironmentTemplateSelections(ctx, "app", "service", &domaincatalog.DeploymentTemplateBinding{TemplateID: "template", Version: 1, ManifestPackageID: "package", Detached: true}); err != nil {
		t.Fatal("legacy independent-copy workflow changed:", err)
	}
}
