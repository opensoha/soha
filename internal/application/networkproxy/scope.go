package networkproxy

import (
	"context"

	access "github.com/opensoha/soha/internal/domain/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ScopeAuthorizer interface {
	Authorize(context.Context, access.Request) (access.Decision, error)
}

func (s *Management) authorizeScope(ctx context.Context, p identity.Principal, key string, item domain.Instance) error {
	if missing(s.scope) {
		return apperrors.ErrServiceUnavailable
	}
	labels := map[string]string{"network.proxyInstanceId": item.ID, "network.proxyEngine": item.Engine, "network.host": item.Host}
	if item.Binding != nil {
		labels["network.siteId"], labels["network.deviceId"] = item.Binding.SiteID, item.Binding.DeviceID
	}
	decision, err := s.scope.Authorize(ctx, access.Request{
		Principal: p, PermissionKey: key,
		Subject:  access.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags},
		Resource: access.ResourceAttributes{Group: "network_access", Kind: "NetworkProxyInstance", Name: item.ID, Labels: labels},
		Context:  access.ContextAttributes{Source: "api", OccurredAt: s.now().UTC()},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "proxy_scope_denied", "The proxy instance is outside your permitted scope.", "该代理实例不在你的授权范围内。")
	}
	return nil
}
