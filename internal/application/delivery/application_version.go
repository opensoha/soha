package delivery

import (
	"context"
	"fmt"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) captureApplicationVersion(ctx context.Context, principal domainidentity.Principal, draft *domaindelivery.BlueprintApplicationDraft) error {
	if draft.ExpectedVersion != nil && *draft.ExpectedVersion < 1 {
		return fmt.Errorf("%w: expectedVersion must be positive", apperrors.ErrInvalidArgument)
	}
	var existing domainapp.App
	if id := strings.TrimSpace(draft.ID); id != "" {
		item, err := s.applications.Get(ctx, principal, id)
		if err != nil {
			return err
		}
		existing = item
	} else {
		items, err := s.applications.List(ctx, principal, domainapp.Filter{Search: strings.TrimSpace(draft.Key), Limit: 200})
		if err != nil {
			return err
		}
		for _, item := range items {
			if strings.TrimSpace(item.Key) == strings.TrimSpace(draft.Key) {
				existing = item
				break
			}
		}
	}
	if existing.ID != "" {
		draft.ID = existing.ID
		if draft.ExpectedVersion == nil {
			draft.ExpectedVersion = &existing.Version
		}
	}
	return nil
}
