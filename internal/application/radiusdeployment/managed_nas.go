package radiusdeployment

import (
	"context"
	"slices"
	"strings"

	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domainruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ManagedNAS(ctx context.Context, id networkidentity.Identity, nas string, proof *networkprotocol.ManagedNASProof, required bool) (access.NASBinding, bool, string, error) {
	managed := strings.HasPrefix(id.ID, domain.PoolPrefix) || strings.HasPrefix(id.ID, domain.MemberPrefix)
	if !managed {
		if proof != nil {
			return access.NASBinding{}, false, "", apperrors.ErrUnauthorized
		}
		return access.NASBinding{}, false, "", nil
	}
	state, err := s.stateForMember(ctx, id.ID)
	if err != nil {
		return access.NASBinding{}, true, "", apperrors.ErrUnauthorized
	}
	if err := s.pinned(state); err != nil {
		return access.NASBinding{}, true, "", err
	}
	binding, err := s.repo.ResolveManagedNAS(ctx, id, nas, proof, state.SlotHash, required)
	return binding, true, state.SlotHash, err
}
func (s *Service) ManagedEnrollment(ctx context.Context, id networkidentity.Identity, request networkprotocol.EnrollmentRequest) (*domainruntime.ManagedNASMemberScope, error) {
	if strings.HasPrefix(id.ID, domain.PoolPrefix) {
		return nil, apperrors.ErrUnauthorized
	}
	if !strings.HasPrefix(id.ID, domain.MemberPrefix) {
		for _, capability := range request.Capabilities {
			if capability == domain.Capability {
				return nil, apperrors.ErrUnauthorized
			}
		}
		return nil, nil
	}
	if id.Kind != "nas" || request.DeviceID != id.ID || !slices.Contains(request.Capabilities, domain.Capability) {
		return nil, apperrors.ErrUnauthorized
	}
	state, err := s.stateForMember(ctx, id.ID)
	if err != nil {
		return nil, apperrors.ErrUnauthorized
	}
	if err := s.pinned(state); err != nil {
		return nil, err
	}
	for _, member := range state.Slot.Members {
		if member.RuntimeID == id.ID {
			return &domainruntime.ManagedNASMemberScope{PoolID: state.Pool.PoolID, MemberID: member.MemberID, SlotHash: state.SlotHash}, nil
		}
	}
	return nil, apperrors.ErrUnauthorized
}
func (s *Service) ManagedConfiguration(ctx context.Context, id networkidentity.Identity) error {
	if strings.HasPrefix(id.ID, domain.PoolPrefix) || strings.HasPrefix(id.ID, domain.MemberPrefix) {
		return apperrors.NewBusiness(apperrors.ErrUnsupportedOperation, "dedicated_radius_control_required", "Use the dedicated member RADIUS control endpoint.", "请使用受管成员专用 RADIUS 控制入口。")
	}
	return nil
}
