package catalog

import (
	"context"
	"errors"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
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
