package app

import (
	"context"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type pinnedTemplateReader struct{ head domaincatalog.BuildTemplate }

func (r pinnedTemplateReader) GetBuildTemplate(context.Context, string) (domaincatalog.BuildTemplate, error) {
	return r.head, nil
}
func (r pinnedTemplateReader) GetBuildTemplateVersion(_ context.Context, id string, version int64) (domaincatalog.BuildTemplate, error) {
	if version < 1 || version > 3 {
		return domaincatalog.BuildTemplate{}, apperrors.ErrNotFound
	}
	return domaincatalog.BuildTemplate{ID: id, PublishedVersion: version}, nil
}

func TestBuildTemplateSavePinsAndPreservesVersions(t *testing.T) {
	service := &Service{templates: pinnedTemplateReader{head: domaincatalog.BuildTemplate{ID: "template", PublishedVersion: 3, PublicationState: "published", Enabled: true}}}
	source := domainapp.BuildSourceInput{ID: "source", Type: domainapp.BuildSourceTypePlatformTemplate, Config: map[string]any{"buildTemplateId": "template"}}
	input := domainapp.UpsertInput{BuildSources: []domainapp.BuildSourceInput{source}}
	if err := service.pinBuildTemplates(context.Background(), &input, domainapp.App{}); err != nil {
		t.Fatal(err)
	}
	if input.BuildSources[0].Config["buildTemplateVersion"] != int64(3) {
		t.Fatal("new source did not pin latest published version")
	}
	if _, changed := source.Config["buildTemplateVersion"]; changed {
		t.Fatal("caller config was mutated")
	}
	current := domainapp.App{BuildSources: []domainapp.BuildSource{{ID: "source", Type: source.Type, Config: map[string]any{"buildTemplateId": "template", "buildTemplateVersion": 1}}}}
	input.BuildSources = []domainapp.BuildSourceInput{source}
	if err := service.pinBuildTemplates(context.Background(), &input, current); err != nil {
		t.Fatal(err)
	}
	if input.BuildSources[0].Config["buildTemplateVersion"] != int64(1) {
		t.Fatal("legacy save upgraded existing source")
	}
	input.BuildSources[0].Config["buildTemplateVersion"] = 2
	if err := service.pinBuildTemplates(context.Background(), &input, current); err != nil {
		t.Fatal(err)
	}
	if input.BuildSources[0].Config["buildTemplateVersion"] != int64(2) {
		t.Fatal("explicit upgrade lost")
	}
	service.templates = pinnedTemplateReader{head: domaincatalog.BuildTemplate{PublicationState: "deprecated"}}
	if err := service.pinBuildTemplates(context.Background(), &input, current); err == nil {
		t.Fatal("new binding to deprecated template allowed")
	}
	input.BuildSources[0].Config["buildTemplateVersion"] = 1
	if err := service.pinBuildTemplates(context.Background(), &input, current); err != nil {
		t.Fatalf("deprecated template broke existing source: %v", err)
	}
}
