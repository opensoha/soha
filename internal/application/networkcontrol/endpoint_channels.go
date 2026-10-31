package networkcontrol

import (
	"context"
	"slices"

	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type EndpointChannels interface {
	Desired(context.Context, runtime.Credential) (ec.Desired, error)
	Report(context.Context, runtime.Credential, []byte) error
}

func (s *Service) channelCredential(ctx context.Context, i networkidentity.Identity) (runtime.Credential, error) {
	c, err := s.authenticate(ctx, i)
	if err != nil {
		return c, err
	}
	if c.RuntimeKind != "endpoint" || !slices.Contains(c.Capabilities, ec.Capability) {
		return c, apperrors.ErrAccessDenied
	}
	if s.options.EndpointChannels == nil {
		return c, apperrors.ErrServiceUnavailable
	}
	return c, nil
}
func (s *Service) ChannelControls(ctx context.Context, i networkidentity.Identity) (ec.Desired, error) {
	c, err := s.channelCredential(ctx, i)
	if err != nil {
		return ec.Desired{}, err
	}
	return s.options.EndpointChannels.Desired(ctx, c)
}
func (s *Service) ChannelReport(ctx context.Context, i networkidentity.Identity, raw []byte) error {
	c, err := s.channelCredential(ctx, i)
	if err != nil {
		return err
	}
	return s.options.EndpointChannels.Report(ctx, c, raw)
}
