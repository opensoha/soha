package app

import (
	"context"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

// ListAccessible applies the requested limit after scope authorization.
func (s *Service) ListAccessible(ctx context.Context, principal domainidentity.Principal, filter domainapp.Filter) ([]domainapp.App, error) {
	reader := *s
	reader.authorizer = appaccess.SnapshotForRead(ctx, s.authorizer)
	if err := reader.authorize(ctx, principal, domainaccess.ActionList, "Application", "", "", "", "", ""); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	filter.Limit = min(limit, 100)
	allowed := make([]domainapp.App, 0, filter.Limit)
	for len(allowed) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, err := s.repo.List(ctx, filter)
		if err != nil {
			return nil, normalizeRepoError(err)
		}
		for _, item := range items {
			if err := reader.authorize(ctx, principal, domainaccess.ActionList, "Application", item.Name, item.Key, item.BusinessLineID, item.Group, item.ID); err != nil {
				continue
			}
			allowed = append(allowed, item)
			if len(allowed) == limit {
				break
			}
		}
		if len(items) < filter.Limit {
			break
		}
		last := items[len(items)-1]
		filter.After = &domainapp.ListCursor{Group: last.Group, Name: last.Name, ID: last.ID}
	}
	_ = s.recordAudit(ctx, principal, "", "Application", "", string(domainaccess.ActionList), "success", "listed applications")
	return allowed, nil
}
