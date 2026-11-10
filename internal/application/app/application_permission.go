package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

// AuthorizeApplicationPermission retains the action-specific scope ceiling for
// operations whose durable task belongs to an application without an environment.
func (s *Service) AuthorizeApplicationPermission(ctx context.Context, principal domainidentity.Principal, id, permission string) error {
	id, permission = strings.TrimSpace(id), strings.TrimSpace(permission)
	if s == nil || s.authorizer == nil || s.repo == nil || id == "" || permission == "" {
		return fmt.Errorf("%w: application action authorization unavailable", apperrors.ErrAccessDenied)
	}
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal, PermissionKey: permission,
		Subject:  domainaccess.SubjectAttributes{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects, Tags: principal.Tags},
		Resource: domainaccess.ResourceAttributes{Kind: "Application", Name: item.Name, Owner: item.Key},
		Delivery: domainaccess.DeliveryAttributes{BusinessLineID: item.BusinessLineID, ApplicationGroup: item.Group, ApplicationID: item.ID},
		Context:  domainaccess.ContextAttributes{Source: requestctx.FromContext(ctx).Source, OccurredAt: time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: application action is outside its authorized scope", apperrors.ErrAccessDenied)
	}
	return nil
}
