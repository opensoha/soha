package networkcontrol

import (
	"context"
	"errors"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) applyEndpointAdmission(ctx context.Context, authorization *runtime.NASAuthorization, subject domain.Subject, device domain.Device, binding domain.NASBinding) error {
	policies, err := s.store.PublishedEndpointResponsePolicies(ctx)
	if err != nil {
		return err
	}
	authorization.ResponsePolicyHash = domain.ResponsePolicyHash(policies)
	presence, err := s.store.EndpointPresence(ctx, device.ID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	authorization.PresenceHash = presence.PayloadHash
	floor, err := s.store.EndpointRestriction(ctx, device.ID, binding.SiteID)
	if err != nil {
		return err
	}
	authorization.RestrictionEpoch = floor.Epoch
	profile, reason := app.EndpointAdmissionProfile(authorization.AccessProfile, subject, device, binding, policies, presence, floor, authorization.PolicyVersion, authorization.CreatedAt)
	authorization.AccessProfile = profile
	if reason != "" {
		authorization.ReasonCode = reason
	}
	if authorization.AccessProfile == domain.ProfileDeny {
		authorization.Decision = domain.DecisionDeny
	}
	return nil
}
