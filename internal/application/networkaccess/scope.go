package networkaccess

import (
	"context"
	"errors"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ScopeAuthorizer interface {
	Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error)
}

type resourceScope struct {
	kind, id, siteID, spaceID, deviceID, ownerUserID string
	group                                            string
}

func (s *Service) authorizeScope(ctx context.Context, principal domainidentity.Principal, key string, resource resourceScope) error {
	if principal.UserID == "" {
		return apperrors.ErrUnauthorized
	}
	if isNilDependency(s.scope) {
		return apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "network_scope_authorizer_unavailable", "Network scope authorization is unavailable.", "网络对象范围鉴权不可用。")
	}
	group := resource.group
	if group == "" {
		group = "network_access"
	}
	decision, err := s.scope.Authorize(ctx, domainaccess.Request{
		Principal: principal, PermissionKey: key,
		Subject: domainaccess.SubjectAttributes{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects, Tags: principal.Tags},
		Resource: domainaccess.ResourceAttributes{Group: group, Kind: resource.kind, Name: resource.id, Labels: map[string]string{
			"network.siteId": resource.siteID, "network.spaceId": resource.spaceID,
			"network.deviceId": resource.deviceID, "network.ownerUserId": resource.ownerUserID,
		}},
		Context: domainaccess.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_scope_denied", "The network object is outside your permitted scope.", "该网络对象不在你的授权范围内。")
	}
	return nil
}

func (s *Service) authorizePolicyScope(ctx context.Context, principal domainidentity.Principal, key, id string, siteIDs []string) error {
	if len(siteIDs) == 0 {
		return s.authorizeScope(ctx, principal, key, resourceScope{kind: "NetworkAccessPolicy", id: id})
	}
	for _, siteID := range siteIDs {
		if err := s.authorizeScope(ctx, principal, key, resourceScope{kind: "NetworkAccessPolicy", id: id, siteID: siteID}); err != nil {
			return err
		}
	}
	return nil
}

func deviceScope(device domain.Device) resourceScope {
	return resourceScope{kind: "EndpointDevice", id: device.ID, siteID: device.SiteID, deviceID: device.ID, ownerUserID: device.OwnerUserID}
}

func sessionScope(session domain.Session) resourceScope {
	return resourceScope{kind: "NetworkSession", id: session.ID, siteID: session.SiteID, deviceID: session.DeviceID, ownerUserID: session.SubjectID}
}

// Collect authorized rows before applying the public limit, so hidden rows do not create false empty pages.
func collectVisible[T any](ctx context.Context, limit int, page func(int, int) ([]T, error), authorize func(T) error) ([]T, error) {
	const pageSize, maxRows = 200, 10000
	items := make([]T, 0, limit)
	for offset := 0; offset < maxRows; offset += pageSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := page(pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := authorize(row); errors.Is(err, apperrors.ErrAccessDenied) {
				continue
			} else if err != nil {
				return nil, err
			}
			items = append(items, row)
			if len(items) == limit {
				return items, nil
			}
		}
		if len(rows) < pageSize {
			return items, nil
		}
	}
	return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "network_scope_scan_limit", "Narrow the network query scope before retrying.", "请缩小网络查询范围后重试。")
}
