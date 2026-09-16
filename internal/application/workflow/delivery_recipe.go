package workflow

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryRecipeReader interface {
	GetWorkflowTemplate(context.Context, string) (domaincatalog.WorkflowTemplate, error)
	GetWorkflowTemplateVersion(context.Context, string, int64) (domaincatalog.WorkflowTemplate, error)
}

func (s *Service) resolveDeliveryRecipe(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryWorkflowDefinition) (domainworkflow.DeliveryWorkflowDefinition, string, error) {
	if input.WorkflowTemplateID == "" && input.WorkflowTemplateVersion == 0 {
		return input, "", nil
	}
	if input.WorkflowTemplateID == "" || input.WorkflowTemplateVersion < 1 {
		return input, "", fmt.Errorf("%w: workflow template requires a published version", apperrors.ErrInvalidArgument)
	}
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesView); err != nil {
		return input, "", err
	}
	reader, ok := s.catalog.(deliveryRecipeReader)
	if !ok {
		return input, "", fmt.Errorf("%w: delivery recipe reader is unavailable", apperrors.ErrInvalidArgument)
	}
	current, err := reader.GetWorkflowTemplate(ctx, input.WorkflowTemplateID)
	if err != nil {
		return input, "", err
	}
	if !current.Enabled || current.PublicationState == "deprecated" {
		return input, "", fmt.Errorf("%w: workflow template is disabled or deprecated", apperrors.ErrInvalidArgument)
	}
	item, err := reader.GetWorkflowTemplateVersion(ctx, input.WorkflowTemplateID, int64(input.WorkflowTemplateVersion))
	if err != nil {
		return input, "", err
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(item.ContentDigest, "sha256:"))
	if err != nil || len(digest) != 32 || !strings.HasPrefix(item.ContentDigest, "sha256:") || item.ID != input.WorkflowTemplateID || item.PublishedVersion != int64(input.WorkflowTemplateVersion) {
		return input, "", fmt.Errorf("%w: workflow template version is not a verified publication", apperrors.ErrInvalidArgument)
	}
	recipe, err := domaincatalog.ParseDeliveryRecipe(item.Definition)
	if err != nil {
		return input, "", fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	if input.Mode == "" {
		input.Mode = recipe.ExecutionMode
	}
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = recipe.MaxConcurrency
	}
	if input.StopOnFailure == nil {
		input.StopOnFailure = &recipe.StopOnFailure
	}
	return input, item.ContentDigest, nil
}
