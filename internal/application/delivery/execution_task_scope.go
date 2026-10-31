package delivery

import (
	"context"
	"fmt"
	"strings"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ApplicationPermissionAuthorizer supplies the action ceiling for tasks without an environment.
type ApplicationPermissionAuthorizer interface {
	AuthorizeApplicationPermission(context.Context, domainidentity.Principal, string, string) error
}

func (s *Service) authorizeExecutionTaskScope(ctx context.Context, principal domainidentity.Principal, task domaindelivery.ExecutionTask, permission string) error {
	appID, bindingID := strings.TrimSpace(task.ApplicationID), strings.TrimSpace(task.ApplicationEnvironmentID)
	if appID == "" {
		return fmt.Errorf("%w: task application scope unavailable", apperrors.ErrAccessDenied)
	}
	if bindingID == "" {
		authorizer, ok := s.applications.(ApplicationPermissionAuthorizer)
		if !ok {
			return fmt.Errorf("%w: task application action authorization unavailable", apperrors.ErrAccessDenied)
		}
		return authorizer.AuthorizeApplicationPermission(ctx, principal, appID, permission)
	}
	if s.catalog == nil {
		return fmt.Errorf("%w: task application environment authorization unavailable", apperrors.ErrAccessDenied)
	}
	binding, err := s.catalog.AuthorizeApplicationEnvironmentPermission(ctx, principal, bindingID, permission)
	if err != nil {
		return err
	}
	if binding.ApplicationID != appID || binding.ID != bindingID {
		return fmt.Errorf("%w: task application environment does not match its target", apperrors.ErrAccessDenied)
	}
	return nil
}
