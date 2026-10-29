package networkproxy

import (
	"context"
	appaccess "github.com/opensoha/soha/internal/application/access"
	access "github.com/opensoha/soha/internal/domain/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"slices"
	"strings"
)

func (s *Management) authorizeBinding(ctx context.Context, principal identity.Principal, input *domain.Binding) (*domain.Binding, error) {
	if input == nil {
		return nil, nil
	}
	value := *input
	value.SiteID, value.DeviceID = strings.TrimSpace(value.SiteID), strings.TrimSpace(value.DeviceID)
	value.ResourceIDs = slices.Clone(value.ResourceIDs)
	if !identifier.MatchString(value.SiteID) || !identifier.MatchString(value.DeviceID) || len(value.ResourceIDs) > 128 {
		return nil, invalid("proxy binding is invalid")
	}
	for _, id := range value.ResourceIDs {
		if !identifier.MatchString(id) {
			return nil, invalid("proxy binding resource is invalid")
		}
	}
	slices.Sort(value.ResourceIDs)
	if len(slices.Compact(slices.Clone(value.ResourceIDs))) != len(value.ResourceIDs) {
		return nil, invalid("proxy binding resources must be unique")
	}
	if value.ResourceIDs == nil {
		value.ResourceIDs = []string{}
	}
	resolved, err := s.store.ResolveBinding(ctx, value)
	if err != nil {
		return nil, err
	}
	checks := []struct{ key, kind, id string }{
		{appaccess.PermNetworkAccessSitesView, "NetworkSite", resolved.SiteID},
		{appaccess.PermNetworkAccessEndpointDevicesView, "EndpointDevice", resolved.DeviceID},
	}
	for _, id := range resolved.ResourceIDs {
		checks = append(checks, struct{ key, kind, id string }{appaccess.PermNetworkAccessResourcesView, "NetworkResource", id})
	}
	for _, check := range checks {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, check.key); err != nil {
			return nil, err
		}
		labels := map[string]string{"network.siteId": resolved.SiteID}
		switch check.kind {
		case "EndpointDevice":
			labels["network.deviceId"], labels["network.ownerUserId"] = resolved.DeviceID, resolved.OwnerUserID
		case "NetworkResource":
			labels["network.spaceId"], labels["network.resourceId"] = resolved.ResourceSpaceIDs[check.id], check.id
		}
		decision, err := s.scope.Authorize(ctx, access.Request{
			Principal: principal, PermissionKey: check.key,
			Subject:  access.SubjectAttributes{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects, Tags: principal.Tags},
			Resource: access.ResourceAttributes{Group: "network_access", Kind: check.kind, Name: check.id, Labels: labels},
			Context:  access.ContextAttributes{Source: "api", OccurredAt: s.now().UTC()},
		})
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			return nil, apperrors.ErrAccessDenied
		}
	}
	return &resolved, nil
}
