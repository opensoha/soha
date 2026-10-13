package copilot

import (
	"context"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *PersonalGuestService) ConfigureTools(grant func(context.Context, guest.Binding, string) (guest.ToolsPlan, error)) {
	s.toolsGrant = grant
}

func (s *PersonalGuestService) ToolsOperation(ctx context.Context, identity networkidentity.Identity, id string) (guest.ToolsPlan, error) {
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.ToolsPlan{}, err
	}
	if item.Status != "registered" || s.toolsGrant == nil {
		return guest.ToolsPlan{}, apperrors.ErrAccessDenied
	}
	return s.toolsGrant(ctx, item.Binding, id)
}

func (s *PersonalGuestService) ConfigureRuntime(grant func(context.Context, guest.Binding, string) (guest.RuntimeOperation, error)) {
	s.runtimeGrant = grant
}

func (s *PersonalGuestService) RuntimeOperation(ctx context.Context, identity networkidentity.Identity, id string) (guest.RuntimeOperation, error) {
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.RuntimeOperation{}, err
	}
	if item.Status != "registered" || s.runtimeGrant == nil {
		return guest.RuntimeOperation{}, apperrors.ErrAccessDenied
	}
	return s.runtimeGrant(ctx, item.Binding, id)
}
