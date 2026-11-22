package apphosting

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func prepareRevisionVisibility(app domain.App, actorID string, input domain.UploadRevisionInput) (*domain.RevisionVisibility, error) {
	req := input.PublishVisibility
	if req == nil {
		return nil, nil
	}
	if !app.IsOwner(actorID) {
		return nil, apperrors.ErrAccessDenied
	}
	reason := strings.TrimSpace(req.Reason)
	if input.IdempotencyKey == "" || utf8.RuneCountInString(reason) > 500 {
		return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_publish_visibility",
			"publishing with a visibility change requires idempotencyKey and a reason no longer than 500 characters",
			"发布时修改分享范围须提供幂等键，原因最长 500 个字符")
	}
	scope, err := domain.NormalizeVisibilityScope(&req.Visibility)
	if err != nil {
		return nil, err
	}
	return &domain.RevisionVisibility{Scope: scope, Reason: reason, BaseRouteGeneration: app.RouteGeneration}, nil
}

func sameRevisionVisibility(a, b *domain.RevisionVisibility) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Reason == b.Reason && domain.ScopesEqual(a.Scope, b.Scope)
}

func (s *Service) checkCurrentPublisher(ctx context.Context, publisherID string) error {
	if s.audience == nil {
		return apperrors.ErrServiceUnavailable
	}
	principal, active, err := s.audience.ResolveUserPrincipal(ctx, publisherID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return apperrors.ErrAccessDenied
	}
	if err != nil {
		return err
	}
	if !active || principal.UserID != publisherID {
		return apperrors.ErrAccessDenied
	}
	err = s.authorize(ctx, principal, access.PermAppHostingAppsPublish)
	if err != nil {
		return err
	}
	return ctx.Err()
}
