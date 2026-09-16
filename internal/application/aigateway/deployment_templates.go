package aigateway

import (
	"context"
	"fmt"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) invokeDeploymentTemplateTool(ctx context.Context, principal domainidentity.Principal, name string, input map[string]any) (any, map[string]any, error) {
	catalog, ok := s.catalog.(interface {
		ListDeploymentTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.DeploymentTemplate, error)
		GetDeploymentTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error)
		PreviewDeploymentTemplate(context.Context, domainidentity.Principal, string, domaincatalog.DeploymentTemplatePreviewInput) (domaincatalog.DeploymentTemplatePreview, error)
	})
	if !ok {
		return nil, nil, fmt.Errorf("%w: deployment template catalog is unavailable", apperrors.ErrInvalidArgument)
	}
	if name == "delivery.deployment_templates.list" {
		items, err := catalog.ListDeploymentTemplates(ctx, principal)
		return items, map[string]any{"count": len(items)}, err
	}
	var req domaincatalog.DeploymentTemplatePreviewInput
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	if req.TemplateID == "" || req.Version < 1 {
		return nil, nil, fmt.Errorf("%w: templateId and a published version are required", apperrors.ErrInvalidArgument)
	}
	meta := map[string]any{"templateId": req.TemplateID, "version": req.Version}
	switch name {
	case "delivery.deployment_templates.version":
		item, err := catalog.GetDeploymentTemplateVersion(ctx, principal, req.TemplateID, req.Version)
		return item, meta, err
	case "delivery.deployment_templates.preview":
		applicationID := stringInput(input, "applicationId")
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		meta["applicationId"], meta["configurationOnly"] = applicationID, true
		item, err := catalog.PreviewDeploymentTemplate(ctx, principal, applicationID, req)
		return item, meta, err
	default:
		return nil, nil, fmt.Errorf("%w: unsupported deployment template tool", apperrors.ErrInvalidArgument)
	}
}
