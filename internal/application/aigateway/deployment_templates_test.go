package aigateway

import (
	"context"
	"errors"
	"testing"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type templatePreviewCatalog struct {
	CatalogService
	applicationID string
	input         domaincatalog.DeploymentTemplatePreviewInput
	err           error
}

func (c *templatePreviewCatalog) ListDeploymentTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.DeploymentTemplate, error) {
	return nil, c.err
}

func (c *templatePreviewCatalog) GetDeploymentTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error) {
	return domaincatalog.DeploymentTemplate{}, c.err
}

func (c *templatePreviewCatalog) PreviewDeploymentTemplate(_ context.Context, _ domainidentity.Principal, appID string, input domaincatalog.DeploymentTemplatePreviewInput) (domaincatalog.DeploymentTemplatePreview, error) {
	c.applicationID, c.input = appID, input
	return domaincatalog.DeploymentTemplatePreview{TemplateID: input.TemplateID, Version: input.Version, ConfigurationOnly: true}, c.err
}

func TestDeploymentTemplateToolPreservesTypedPreviewAndAuthorization(t *testing.T) {
	catalog := &templatePreviewCatalog{}
	service := &Service{catalog: catalog}
	tool := domainaigateway.ToolCapability{Name: "delivery.deployment_templates.preview"}
	input := map[string]any{"applicationId": "app", "templateId": "http", "version": 2,
		"serviceKey": "api", "applicationEnvironmentId": "dev-binding",
		"parameters": map[string]any{"enabled": false, "count": 0}, "overrides": map[string]any{"replicas": 3},
	}
	result, metadata, err := service.invokeDeliveryTool(context.Background(), domainidentity.Principal{}, tool, input)
	if err != nil {
		t.Fatal(err)
	}
	preview, ok := result.(domaincatalog.DeploymentTemplatePreview)
	if !ok || !preview.ConfigurationOnly || metadata["applicationId"] != "app" || catalog.applicationID != "app" || catalog.input.Version != 2 || catalog.input.ApplicationEnvironmentID != "dev-binding" || catalog.input.Parameters["enabled"] != false || catalog.input.Parameters["count"] != float64(0) || catalog.input.Overrides["replicas"] != float64(3) {
		t.Fatalf("preview lost typed inputs or scope: %+v %+v %+v", result, metadata, catalog)
	}
	catalog.err = apperrors.ErrAccessDenied
	if _, _, err := service.invokeDeliveryTool(context.Background(), domainidentity.Principal{}, tool, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("catalog authorization error was not preserved: %v", err)
	}
}
