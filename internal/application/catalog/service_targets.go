package catalog

import (
	"context"
	"fmt"
	"strings"

	access "github.com/opensoha/soha/internal/application/access"
	da "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type targetServiceReader interface {
	GetService(context.Context, string, string) (domainapp.Service, error)
}

func (s *Service) prepareTargetServiceMappings(ctx context.Context, principal di.Principal, input dc.ApplicationEnvironmentInput, current dc.ApplicationEnvironment) (dc.ApplicationEnvironmentInput, error) {
	if input.Targets == nil {
		return input, nil
	}
	old := make(map[string]dc.ReleaseTarget, len(current.Targets))
	for _, target := range current.Targets {
		old[target.ID] = target
	}
	next := make([]dc.ReleaseTargetInput, len(input.Targets))
	var changed []domainapp.ServiceInput
	for i, target := range input.Targets {
		previous := old[strings.TrimSpace(target.ID)]
		if target.ResolvedServiceID() == "" && previous.ResolvedServiceID() != "" {
			target.ServiceID = previous.ResolvedServiceID()
		}
		canonical, err := target.WithCanonicalServiceID()
		if err != nil {
			return input, fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, err)
		}
		next[i] = canonical
		if canonical.ServiceID != "" && (current.ApplicationID != input.ApplicationID || canonical.ServiceID != previous.ResolvedServiceID()) {
			changed = append(changed, domainapp.ServiceInput{ID: canonical.ServiceID})
		}
	}
	if err := s.authorizeTargetMappings(ctx, principal, input.ApplicationID, changed); err != nil {
		return input, err
	}

	input.Targets = next
	return input, nil
}

func validationTargets(input dc.ApplicationEnvironmentInput, current dc.ApplicationEnvironment) dc.ApplicationEnvironmentInput {
	if input.Targets != nil {
		return input
	}
	input.Targets = make([]dc.ReleaseTargetInput, 0, len(current.Targets))
	for _, target := range current.Targets {
		input.Targets = append(input.Targets, target.Input())
	}
	return input
}

func (s *Service) authorizeTargetMappings(ctx context.Context, principal di.Principal, applicationID string, targets []domainapp.ServiceInput) error {
	if len(targets) == 0 {
		return nil
	}
	reader, ok := s.apps.(targetServiceReader)
	if !ok {
		return fmt.Errorf("%w: service association authorization is unavailable", apperrors.ErrUnsupportedOperation)
	}
	permission := access.ManagedActionPermission(access.PermDeliveryApplicationServicesManage, "update")
	if err := s.authorize(ctx, principal, permission); err != nil {
		return err
	}
	app, err := s.apps.Get(ctx, applicationID)
	if err != nil {
		return normalizeRepoError(err)
	}
	for _, target := range targets {
		service, err := reader.GetService(ctx, applicationID, target.ID)
		if err != nil {
			return normalizeRepoError(err)
		}
		if service.ID != target.ID || service.ApplicationID != applicationID || !service.Enabled {
			return fmt.Errorf("%w: selected service is not available in this application", apperrors.ErrConflict)
		}
		if s.authorizer != nil {
			if err := s.authorizeDelivery(ctx, principal, permission, da.ActionUpdate, "ApplicationService", service.Name, app.BusinessLineID, app.Group, "", applicationID); err != nil {
				return err
			}
		}
	}
	return nil
}
