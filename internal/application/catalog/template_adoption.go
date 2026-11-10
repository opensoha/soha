package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Application-wide build bindings affect every environment, including those a
// filtered list would hide. Fail closed unless the whole scope is accessible.
func (s *Service) ListTemplateAdoptionEnvironments(ctx context.Context, p domainidentity.Principal, appID string) ([]domaincatalog.ApplicationEnvironment, error) {
	items, err := s.repo.ListApplicationEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	result := []domaincatalog.ApplicationEnvironment{}
	for _, item := range items {
		if item.ApplicationID != appID {
			continue
		}
		if err := s.authorizeApplicationEnvironment(ctx, p, domainaccess.ActionView, item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func validateTemplateAdoptionUpdate(ctx context.Context, current domaincatalog.ApplicationEnvironment, input domaincatalog.ApplicationEnvironmentInput) error {
	if current.WorkflowTemplateID != "" && current.WorkflowTemplateVersion > 0 && input.WorkflowTemplateID != "" && (current.WorkflowTemplateID != input.WorkflowTemplateID || input.WorkflowTemplateVersion > 0 && current.WorkflowTemplateVersion != input.WorkflowTemplateVersion) && !domainupdate.IsAdoption(ctx, "workflow", current.ApplicationID) {
		return fmt.Errorf("%w: adopt a published workflow version through a template comparison and configuration Plan", apperrors.ErrConflict)
	}
	for _, target := range input.Targets {
		var previous any
		for _, old := range current.Targets {
			if old.ID == target.ID {
				previous = old.Metadata["templateSelection"]
				break
			}
		}
		left, _ := json.Marshal(previous)
		right, _ := json.Marshal(target.Metadata["templateSelection"])
		if string(left) != string(right) && !domainupdate.IsAdoption(ctx, "deployment", current.ApplicationID) {
			return fmt.Errorf("%w: template selection is owned by its approved configuration Plan", apperrors.ErrConflict)
		}
	}
	return nil
}
