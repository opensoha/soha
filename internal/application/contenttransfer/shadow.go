package contenttransfer

import (
	"context"
	"github.com/opensoha/soha/internal/application/contentdlp"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/domain/identity"
)

func (s *Service) PreviewDestination(ctx context.Context, p identity.Principal, id string, in dlp.ShadowInput) (dlp.ShadowResult, error) {
	defer func() {
		for _, sample := range in.Samples {
			clear(sample)
		}
	}()
	dest, err := s.destination(ctx, p, id, DestinationView)
	if err != nil {
		return dlp.ShadowResult{}, err
	}
	if err := s.global(ctx, p, DestinationUpdate); err != nil {
		return dlp.ShadowResult{}, err
	}
	if err := s.authorize(ctx, p, DestinationUpdate, dest); err != nil {
		return dlp.ShadowResult{}, err
	}
	ctx, finish, err := s.operationContext(ctx)
	if err != nil {
		return dlp.ShadowResult{}, err
	}
	defer finish()
	draft := dest.Draft.Policy.Scanner()
	draft.Enabled = dest.Draft.Enabled
	var published *dlp.Content
	if dest.Published != nil {
		x := dest.Published.Policy.Scanner()
		x.Enabled = dest.Published.Enabled
		published = &x
	}
	return contentdlp.Shadow(ctx, draft, published, dest.Revision, dest.PublishedVersion, in)
}
