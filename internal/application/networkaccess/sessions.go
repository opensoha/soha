package networkaccess

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListSessions(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.SessionFilter) ([]domainnetworkaccess.Session, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return nil, err
	}
	filter.SiteID, filter.RuntimeID, filter.SubjectID, filter.DeviceID, filter.Status = strings.TrimSpace(filter.SiteID), strings.TrimSpace(filter.RuntimeID), strings.TrimSpace(filter.SubjectID), strings.TrimSpace(filter.DeviceID), strings.TrimSpace(filter.Status)
	if err := validateSessionFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListSessions(ctx, filter)
}

func (s *Service) GetSession(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Session, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return domainnetworkaccess.Session{}, err
	}
	return s.store.GetSession(ctx, strings.TrimSpace(id))
}

func (s *Service) PlanSessionAction(ctx context.Context, principal domainidentity.Principal, sessionID string, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionActionPlan, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.SessionActionPlan{}, err
	}
	plan, err := s.planSessionAction(ctx, sessionID, input)
	if err == nil {
		s.recordAnalysis(ctx, principal, "network_access.session.coa.plan", "NetworkSessionActionPlan", true, map[string]any{"sessionId": plan.SessionID, "requestedAction": plan.RequestedAction, "effectiveAction": plan.EffectiveAction, "targetAccessProfile": plan.TargetAccessProfile, "willDisconnect": plan.WillDisconnect})
	}
	return plan, err
}

func (s *Service) ExecuteSessionAction(ctx context.Context, principal domainidentity.Principal, sessionID string, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionCommand, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	input = normalizeSessionActionInput(input)
	if err := validateSessionActionInput(input, true); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	plan, err := s.planSessionAction(ctx, sessionID, input)
	if err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	if subtle.ConstantTimeCompare([]byte(plan.PlanHash), []byte(input.PlanHash)) != 1 {
		return domainnetworkaccess.SessionCommand{}, apperrors.NewBusiness(apperrors.ErrConflict, "network_session_plan_stale", "The network session action plan is stale.", "网络会话动作计划已过期。")
	}
	now := time.Now().UTC()
	session, err := s.store.GetSession(ctx, plan.SessionID)
	if err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	command, err := s.store.CreateSessionCommand(ctx, domainnetworkaccess.SessionCommand{
		ID: uuid.NewString(), SessionID: plan.SessionID, RuntimeID: plan.RuntimeID, NASID: plan.NASID,
		SubjectID: session.SubjectID, DeviceID: session.DeviceID,
		Action: plan.EffectiveAction, TargetAccessProfile: plan.TargetAccessProfile, PolicyVersion: session.PolicyVersion,
		Status: domainnetworkaccess.SessionCommandPending, ReasonCode: plan.ReasonCode, PlanHash: plan.PlanHash, RadiusAttributes: plan.RadiusAttributes,
		EffectiveAt: now, ExpiresAt: plan.CommandExpiresAt, CreatedAt: now,
	})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.session.coa.execute", "NetworkSessionCommand", command.ID, command.SessionID)
	}
	return command, err
}

func (s *Service) planSessionAction(ctx context.Context, sessionID string, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionActionPlan, error) {
	input = normalizeSessionActionInput(input)
	if err := validateSessionActionInput(input, false); err != nil {
		return domainnetworkaccess.SessionActionPlan{}, err
	}
	session, err := s.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return domainnetworkaccess.SessionActionPlan{}, err
	}
	binding, err := s.store.FindActiveNASBinding(ctx, session.RuntimeID, session.NASID)
	if err != nil {
		return domainnetworkaccess.SessionActionPlan{}, err
	}
	var profile *domainnetworkaccess.SiteProfileBinding
	if input.TargetAccessProfile != domainnetworkaccess.ProfileDeny && input.Action == domainnetworkaccess.SessionActionCoA && binding.CoASupported {
		value, err := s.store.FindSiteProfileBinding(ctx, session.SiteID, input.TargetAccessProfile)
		if err != nil {
			return domainnetworkaccess.SessionActionPlan{}, err
		}
		profile = &value
	}
	return buildSessionActionPlan(time.Now().UTC(), session, binding, profile, input)
}

func normalizeSessionActionInput(input domainnetworkaccess.SessionActionInput) domainnetworkaccess.SessionActionInput {
	input.Action, input.TargetAccessProfile, input.ReasonCode, input.PlanHash = strings.TrimSpace(input.Action), strings.TrimSpace(input.TargetAccessProfile), strings.TrimSpace(input.ReasonCode), strings.TrimSpace(input.PlanHash)
	return input
}

func buildSessionActionPlan(now time.Time, session domainnetworkaccess.Session, binding domainnetworkaccess.NASBinding, profile *domainnetworkaccess.SiteProfileBinding, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionActionPlan, error) {
	if err := validateActionableSession(now, session, binding); err != nil {
		return domainnetworkaccess.SessionActionPlan{}, err
	}
	effectiveAction := input.Action
	if input.TargetAccessProfile == domainnetworkaccess.ProfileDeny || (input.Action == domainnetworkaccess.SessionActionCoA && !binding.CoASupported) {
		effectiveAction = domainnetworkaccess.SessionActionDisconnect
	}
	if effectiveAction == domainnetworkaccess.SessionActionDisconnect && !binding.DisconnectSupported {
		return domainnetworkaccess.SessionActionPlan{}, apperrors.NewBusiness(apperrors.ErrConflict, "nas_disconnect_unsupported", "The NAS cannot disconnect this session.", "NAS 不支持断开此会话。")
	}
	attributes, err := sessionActionRadiusAttributes(session, profile, input, effectiveAction)
	if err != nil {
		return domainnetworkaccess.SessionActionPlan{}, err
	}
	expiresAt := now.Add(5 * time.Minute)
	if session.ExpiresAt.Before(expiresAt) {
		expiresAt = session.ExpiresAt
	}
	fingerprint := struct {
		SessionID, RuntimeID, NASID, RequestedAction, EffectiveAction, CurrentProfile, TargetProfile, ReasonCode, Status string
		PolicyVersion                                                                                                    int
		SessionUpdatedAt                                                                                                 time.Time
		RadiusAttributes                                                                                                 *networkprotocol.RadiusAttributes
	}{session.ID, session.RuntimeID, session.NASID, input.Action, effectiveAction, session.AccessProfile, input.TargetAccessProfile, input.ReasonCode, session.Status, session.PolicyVersion, session.UpdatedAt.UTC(), attributes}
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return domainnetworkaccess.SessionActionPlan{}, fmt.Errorf("marshal network session action plan: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return domainnetworkaccess.SessionActionPlan{
		SessionID: session.ID, RuntimeID: session.RuntimeID, NASID: session.NASID,
		RequestedAction: input.Action, EffectiveAction: effectiveAction, CurrentAccessProfile: session.AccessProfile,
		TargetAccessProfile: input.TargetAccessProfile, ReasonCode: input.ReasonCode, WillDisconnect: effectiveAction == domainnetworkaccess.SessionActionDisconnect,
		CommandExpiresAt: expiresAt, PlanHash: fmt.Sprintf("sha256:%x", digest), RadiusAttributes: attributes,
	}, nil
}

func validateActionableSession(now time.Time, session domainnetworkaccess.Session, binding domainnetworkaccess.NASBinding) error {
	if session.ID == "" || session.RuntimeID == "" || session.NASID == "" || session.SiteID == "" || !session.ExpiresAt.After(now) || session.Status == "revoked" || session.Status == "expired" {
		return apperrors.NewBusiness(apperrors.ErrConflict, "network_session_not_actionable", "The network session cannot accept an enforcement action.", "网络会话无法执行准入动作。")
	}
	if binding.Status != domainnetworkaccess.StatusActive || binding.RuntimeID != session.RuntimeID || binding.NASID != session.NASID || binding.SiteID != session.SiteID {
		return apperrors.NewBusiness(apperrors.ErrConflict, "nas_binding_unavailable", "The session NAS binding is not active.", "会话的 NAS 绑定未激活。")
	}
	return nil
}

func sessionActionRadiusAttributes(session domainnetworkaccess.Session, profile *domainnetworkaccess.SiteProfileBinding, input domainnetworkaccess.SessionActionInput, effectiveAction string) (*networkprotocol.RadiusAttributes, error) {
	if effectiveAction != domainnetworkaccess.SessionActionCoA {
		return nil, nil
	}
	if profile == nil || profile.SiteID != session.SiteID || profile.AccessProfile != input.TargetAccessProfile {
		return nil, apperrors.NewBusiness(apperrors.ErrConflict, "site_profile_binding_unavailable", "The target site profile has no enforcement binding.", "目标站点访问等级没有执行绑定。")
	}
	return &networkprotocol.RadiusAttributes{VLANID: profile.VLANID, FilterID: profile.FilterID, SessionTimeoutSeconds: profile.SessionTimeoutSeconds}, nil
}
