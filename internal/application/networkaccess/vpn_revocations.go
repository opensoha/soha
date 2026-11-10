package networkaccess

import (
	"context"
	"errors"
	"fmt"

	audit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func (s *VPNService) revocationSession(ctx context.Context, p identity.Principal, id, key string) (domain.Session, error) {
	if !runtimeIdentifierPattern.MatchString(id) {
		return domain.Session{}, invalid("session ID is invalid")
	}
	if err := s.base.authorize(ctx, p, key); err != nil {
		return domain.Session{}, err
	}
	session, err := s.base.store.GetSession(ctx, id)
	if err != nil {
		return session, err
	}
	if session.NASID != "" || session.Mode == domain.ModeInternalDirect {
		return domain.Session{}, apperrors.ErrNotFound
	}
	for _, target := range []resourceScope{
		sessionScope(session),
		{kind: "EndpointDevice", id: session.DeviceID, siteID: session.SiteID, deviceID: session.DeviceID, ownerUserID: session.SubjectID},
		{kind: "NetworkSite", id: session.SiteID, siteID: session.SiteID, deviceID: session.DeviceID, ownerUserID: session.SubjectID},
	} {
		if err := s.base.authorizeScope(ctx, p, key, target); err != nil {
			return session, err
		}
	}
	return session, nil
}

func (s *VPNService) GetSessionRevocation(ctx context.Context, p identity.Principal, id string) (*domain.VPNRevocation, error) {
	session, err := s.revocationSession(ctx, p, id, "network_access.vpn_sessions.view")
	if err != nil {
		return nil, err
	}
	item, err := s.connections.GetVPNRevocation(ctx, session)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *VPNService) RevokeSession(ctx context.Context, p identity.Principal, id string) (domain.VPNRevocation, error) {
	session, err := s.revocationSession(ctx, p, id, "network_access.vpn_sessions.revoke")
	if err != nil {
		return domain.VPNRevocation{}, err
	}
	if err := s.auditRevocation(ctx, p, session, "revoke"); err != nil {
		return domain.VPNRevocation{}, err
	}
	item, err := s.connections.RevokeVPNSession(ctx, session, p.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, p, "network_access.vpn.session.revoke", "NetworkSession", id, id)
	}
	return item, err
}

func (s *VPNService) ReleaseSessionRevocation(ctx context.Context, p identity.Principal, id string, expected int64) (domain.VPNRevocation, error) {
	if expected < 1 {
		return domain.VPNRevocation{}, invalid("expectedEpoch must be positive")
	}
	session, err := s.revocationSession(ctx, p, id, "network_access.vpn_sessions.release")
	if err != nil {
		return domain.VPNRevocation{}, err
	}
	if err := s.auditRevocation(ctx, p, session, "release"); err != nil {
		return domain.VPNRevocation{}, err
	}
	item, err := s.connections.ReleaseVPNRevocation(ctx, session, expected, p.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, p, "network_access.vpn.session.release", "NetworkSession", id, id)
	}
	return item, err
}

func (s *VPNService) auditRevocation(ctx context.Context, p identity.Principal, session domain.Session, action string) error {
	meta := requestctx.FromContext(ctx)
	err := s.base.audit.Record(ctx, audit.Entry{ActorID: p.UserID, ActorName: p.UserName, Roles: p.Roles, Teams: p.Teams,
		ResourceKind: "NetworkSession", ResourceName: session.ID, Action: "network_access.vpn.session." + action,
		Result: "requested", Summary: "VPN restriction change requested", RequestPath: meta.Path, RequestMethod: meta.Method,
		RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: map[string]any{"sessionId": session.ID, "subjectId": session.SubjectID, "deviceId": session.DeviceID, "siteId": session.SiteID}})
	if err != nil {
		return fmt.Errorf("audit VPN restriction change: %w", err)
	}
	return nil
}
