package networkcontrol

import (
	"context"
	"time"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func radiusAttachment(request networkprotocol.NASAuthorizationRequest) (access.NASAttachment, error) {
	called, calledValid := networkprotocol.DecodeRadiusAttachment(request.CalledStationIDBase64, 253)
	port, portValid := networkprotocol.DecodeRadiusAttachment(request.NASPortIDBase64, 128)
	if !calledValid || !portValid {
		return access.NASAttachment{}, invalidRuntimeMessage()
	}
	return access.NASAttachment{CalledStationID: called, NASPortID: port, NASPort: request.NASPort}, nil
}

func (s *Service) applyNASAttributes(ctx context.Context, authorization *runtime.NASAuthorization, decision app.AccessNetworkDecision, siteID string) (*access.SiteProfileBinding, error) {
	if authorization.Decision != access.DecisionAllow {
		return nil, nil
	}
	authorization.AccessNetworkRadius = app.NASNetworkAttributes(decision)
	profile, err := app.ResolveNASRadiusProfile(ctx, decision, siteID, authorization.AccessProfile, s.store.SiteProfileBinding)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		authorization.Decision, authorization.AccessProfile, authorization.ReasonCode = access.DecisionDeny, access.ProfileDeny, "site_profile_binding_unavailable"
		return nil, nil
	}
	authorization.RadiusAttributes = &networkprotocol.RadiusAttributes{VLANID: profile.VLANID, FilterID: profile.FilterID, SessionTimeoutSeconds: profile.SessionTimeoutSeconds}
	authorization.ValidUntil = authorization.CreatedAt.Add(time.Duration(profile.SessionTimeoutSeconds) * time.Second)
	return profile, nil
}
