package apphosting

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) StartBrowserAuth(ctx context.Context, principal domainidentity.Principal, input domainapphosting.BrowserAuthStartInput) (domainapphosting.BrowserAuthStartResult, error) {
	returnPath, gatewayState, settings, err := s.validateBrowserAuthInputs(ctx, input)
	if err != nil {
		return domainapphosting.BrowserAuthStartResult{}, err
	}
	dnsLabel, normalizedHost, err := domainapphosting.ParseHostedAppHostForScheme(input.Host, settings.AppsDomain, settings.PublicScheme)
	if err != nil {
		return domainapphosting.BrowserAuthStartResult{}, err
	}
	if strings.TrimSpace(principal.UserID) == "" {
		return domainapphosting.BrowserAuthStartResult{LoginRequired: true}, nil
	}
	sourceSessionID := strings.TrimSpace(input.SourceSessionID)
	if sourceSessionID == "" {
		return domainapphosting.BrowserAuthStartResult{}, apperrors.NewBusiness(
			apperrors.ErrAccessDenied,
			"browser_session_required",
			"interactive browser session is required for hosted application SSO",
			"访问托管应用需要使用交互式浏览器登录会话",
		)
	}
	if active, err := s.isBrowserSessionActive(ctx, sourceSessionID, principal.UserID); err != nil {
		return domainapphosting.BrowserAuthStartResult{}, err
	} else if !active {
		return domainapphosting.BrowserAuthStartResult{LoginRequired: true}, nil
	}
	if !settings.Enabled {
		return domainapphosting.BrowserAuthStartResult{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "app_hosting_disabled", "workspace App Hosting is currently disabled", "工作区应用托管当前已关闭")
	}

	app, err := s.repo.GetAppByDNSLabel(ctx, dnsLabel)
	if err != nil {
		return domainapphosting.BrowserAuthStartResult{}, err
	}
	canonicalHost := canonicalRegisteredHost(app, settings, normalizedHost)
	if err := checkAppOnlineForBrowserAuth(app); err != nil {
		if errors.Is(err, apperrors.ErrAccessDenied) {
			s.recordAudit(ctx, principal, "auth.start", "denied", app.ID, "Denied SSO start for administratively suspended hosted app", map[string]any{"appId": app.ID, "host": canonicalHost})
		}
		return domainapphosting.BrowserAuthStartResult{}, err
	}

	effectivePrincipal, visitorProfile, err := s.resolveEffectiveVisitor(ctx, principal)
	if err != nil || !domainapphosting.CanAccess(effectivePrincipal, app, settings) {
		s.recordAudit(ctx, principal, "auth.start", "denied", app.ID, "Denied hosted application browser SSO handoff", map[string]any{
			"appId": app.ID,
			"host":  canonicalHost,
		})
		return domainapphosting.BrowserAuthStartResult{}, apperrors.ErrAccessDenied
	}

	rawTicket, err := s.issueBrowserAuthTicket(ctx, app, canonicalHost, returnPath, sourceSessionID, gatewayState, visitorProfile)
	if err != nil {
		return domainapphosting.BrowserAuthStartResult{}, err
	}
	return domainapphosting.BrowserAuthStartResult{
		RedirectURL: buildGatewayCallbackURL(settings.PublicScheme, canonicalHost, rawTicket, gatewayState),
	}, nil
}

func (s *Service) validateBrowserAuthInputs(ctx context.Context, input domainapphosting.BrowserAuthStartInput) (string, string, domainapphosting.Settings, error) {
	returnPath, err := domainapphosting.ValidateReturnPath(input.ReturnPath)
	if err != nil {
		return "", "", domainapphosting.Settings{}, err
	}
	gatewayState, err := domainapphosting.ValidateGatewayState(input.GatewayState)
	if err != nil {
		return "", "", domainapphosting.Settings{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return "", "", domainapphosting.Settings{}, err
	}
	return returnPath, gatewayState, settings.WithDefaults(), nil
}

func canonicalRegisteredHost(app domainapphosting.App, settings domainapphosting.Settings, validatedAuthority string) string {
	registered := app.Host(settings.AppsDomain)
	if _, port, err := net.SplitHostPort(validatedAuthority); err == nil && port != "" {
		return net.JoinHostPort(registered, port)
	}
	return registered
}

func checkAppOnlineForBrowserAuth(app domainapphosting.App) error {
	if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "app_suspended_by_admin", "hosted application is suspended by administrator", "该托管应用已被管理员强制封停")
	}
	if app.Status != sohaapi.HostedAppStatusOnline || strings.TrimSpace(app.ActiveRevisionID) == "" {
		return apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "app_offline", "hosted application is currently offline", "该托管应用当前已下线")
	}
	return nil
}

func (s *Service) resolveEffectiveVisitor(ctx context.Context, principal domainidentity.Principal) (domainidentity.Principal, domainapphosting.VisitorProfile, error) {
	if s.audience != nil {
		live, err := s.audience.ResolveVisitor(ctx, principal.UserID)
		if errors.Is(err, domainapphosting.ErrVisitorDisabled) {
			return domainidentity.Principal{}, domainapphosting.VisitorProfile{}, apperrors.ErrAccessDenied
		}
		if err == nil {
			updated := principal
			updated.Teams = slices.Clone(live.TeamIDs)
			return updated, live, nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return domainidentity.Principal{}, domainapphosting.VisitorProfile{}, err
		}
	}
	fallback := domainapphosting.VisitorProfile{
		ID:          principal.UserID,
		Username:    firstNonEmpty(principal.UserName, principal.UserID),
		DisplayName: firstNonEmpty(principal.UserName, principal.UserID),
		Email:       principal.Email,
		TeamIDs:     slices.Clone(principal.Teams),
	}
	return principal, fallback, nil
}

func (s *Service) issueBrowserAuthTicket(
	ctx context.Context,
	app domainapphosting.App,
	host, returnPath, sourceSessionID, gatewayState string,
	visitor domainapphosting.VisitorProfile,
) (string, error) {
	rawTicket, err := generateOpaqueToken("aht_", 32)
	if err != nil {
		return "", err
	}
	stateHash := ""
	if strings.TrimSpace(gatewayState) != "" {
		stateHash = sha256HexString(strings.TrimSpace(gatewayState))
	}
	now := s.now()
	record := domainapphosting.AuthTicket{
		TicketSHA256:       sha256HexString(rawTicket),
		AppID:              app.ID,
		DNSLabel:           app.DNSLabel,
		Host:               host,
		RouteGeneration:    app.RouteGeneration,
		ReturnPath:         returnPath,
		SourceSessionID:    sourceSessionID,
		GatewayStateSHA256: stateHash,
		UserID:             visitor.ID,
		Username:           visitor.Username,
		DisplayName:        visitor.DisplayName,
		Email:              visitor.Email,
		Teams:              slices.Clone(visitor.TeamIDs),
		ExpiresAt:          now.Add(domainapphosting.DefaultAuthTicketTTL),
		CreatedAt:          now,
	}
	if err := s.repo.CreateAuthTicket(ctx, record); err != nil {
		return "", err
	}
	return rawTicket, nil
}

func buildGatewayCallbackURL(publicScheme, host, rawTicket, gatewayState string) string {
	scheme := strings.ToLower(strings.TrimSpace(publicScheme))
	if scheme != "http" && scheme != "https" {
		scheme = domainapphosting.DefaultPublicScheme
	}
	query := "ticket=" + url.QueryEscape(rawTicket)
	if gatewayState != "" {
		query += "&state=" + url.QueryEscape(gatewayState)
	}
	return scheme + "://" + host + "/_soha/auth/callback?" + query
}

func (s *Service) ExchangeGatewayTicket(ctx context.Context, gatewayBearerToken string, req sohaapi.HostedAppTicketExchangeRequest) (sohaapi.HostedAppTicketExchangeResult, error) {
	settings, normalizedHost, err := s.validateTicketExchangeRequest(ctx, gatewayBearerToken, req)
	if err != nil {
		return sohaapi.HostedAppTicketExchangeResult{}, err
	}
	now := s.now()
	consumed, err := s.repo.ConsumeAuthTicket(ctx, sha256HexString(strings.TrimSpace(req.Ticket)), normalizedHost, now)
	if err != nil {
		return sohaapi.HostedAppTicketExchangeResult{}, err
	}
	if !verifyTicketGatewayState(consumed.GatewayStateSHA256, req.GatewayState) {
		return sohaapi.HostedAppTicketExchangeResult{}, apperrors.ErrAccessDenied
	}
	sessionID, sessionExpiry, err := s.verifyConsumedTicketSession(ctx, consumed, now)
	if err != nil {
		return sohaapi.HostedAppTicketExchangeResult{}, err
	}

	app, err := s.repo.GetAppByID(ctx, consumed.AppID)
	if err != nil {
		return sohaapi.HostedAppTicketExchangeResult{}, err
	}
	if err := checkAppOnlineForBrowserAuth(app); err != nil {
		return sohaapi.HostedAppTicketExchangeResult{}, err
	}
	liveProfile, err := s.revalidateExchangedVisitor(ctx, consumed, app, settings)
	if err != nil {
		return sohaapi.HostedAppTicketExchangeResult{}, err
	}
	return sohaapi.HostedAppTicketExchangeResult{
		SessionID:       sessionID,
		PrincipalUserID: liveProfile.ID,
		AppID:           app.ID,
		DNSLabel:        app.DNSLabel,
		Host:            normalizedHost,
		RouteGeneration: app.RouteGeneration,
		ReturnPath:      consumed.ReturnPath,
		ExpiresAt:       sessionExpiry,
		User:            liveProfile.ToPairwiseVisitorIdentity(s.visitorIDSecret, app.ID),
	}, nil
}

func (s *Service) validateTicketExchangeRequest(ctx context.Context, gatewayBearerToken string, req sohaapi.HostedAppTicketExchangeRequest) (domainapphosting.Settings, string, error) {
	settings, err := s.verifyGatewayToken(ctx, gatewayBearerToken)
	if err != nil {
		return domainapphosting.Settings{}, "", err
	}
	if !settings.Enabled {
		return domainapphosting.Settings{}, "", apperrors.ErrServiceUnavailable
	}
	rawTicket := strings.TrimSpace(req.Ticket)
	if rawTicket == "" || len(rawTicket) > 512 {
		return domainapphosting.Settings{}, "", apperrors.ErrInvalidArgument
	}
	if _, err := domainapphosting.ValidateGatewayState(req.GatewayState); err != nil {
		return domainapphosting.Settings{}, "", apperrors.ErrInvalidArgument
	}
	_, normalizedHost, err := domainapphosting.ParseHostedAppHostForScheme(req.Host, settings.AppsDomain, settings.PublicScheme)
	if err != nil {
		return domainapphosting.Settings{}, "", err
	}
	return settings, normalizedHost, nil
}

func verifyTicketGatewayState(expectedHash, providedState string) bool {
	trimmed := strings.TrimSpace(providedState)
	if expectedHash == "" && trimmed == "" {
		return true
	}
	if expectedHash == "" || trimmed == "" {
		return false
	}
	actualHash := sha256HexString(trimmed)
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actualHash)) == 1
}

func (s *Service) verifyConsumedTicketSession(ctx context.Context, consumed domainapphosting.AuthTicket, now time.Time) (string, time.Time, error) {
	sessionID := strings.TrimSpace(consumed.SourceSessionID)
	if sessionID == "" {
		return "", time.Time{}, apperrors.ErrAccessDenied
	}
	expiresAt := now.Add(domainapphosting.DefaultGatewaySessionTTL)
	if s.sessions != nil {
		state, err := s.sessions.ValidateBrowserSession(ctx, sessionID, consumed.UserID)
		if err != nil {
			return "", time.Time{}, err
		}
		if !state.Active {
			return "", time.Time{}, apperrors.ErrAccessDenied
		}
		if !state.ExpiresAt.IsZero() && state.ExpiresAt.Before(expiresAt) {
			expiresAt = state.ExpiresAt.UTC()
		}
	}
	if !expiresAt.After(now) {
		return "", time.Time{}, apperrors.ErrAccessDenied
	}
	return sessionID, expiresAt, nil
}

func (s *Service) isBrowserSessionActive(ctx context.Context, sessionID, userID string) (bool, error) {
	if s.sessions == nil {
		return true, nil
	}
	state, err := s.sessions.ValidateBrowserSession(ctx, sessionID, userID)
	if err != nil {
		return false, err
	}
	return state.Active, nil
}

func (s *Service) revalidateExchangedVisitor(ctx context.Context, consumed domainapphosting.AuthTicket, app domainapphosting.App, settings domainapphosting.Settings) (domainapphosting.VisitorProfile, error) {
	principal := domainidentity.Principal{
		UserID:   consumed.UserID,
		UserName: consumed.Username,
		Email:    consumed.Email,
		Teams:    consumed.Teams,
	}
	effectivePrincipal, liveVisitor, err := s.resolveEffectiveVisitor(ctx, principal)
	if err != nil || !domainapphosting.CanAccess(effectivePrincipal, app, settings) {
		return domainapphosting.VisitorProfile{}, apperrors.ErrAccessDenied
	}
	return liveVisitor, nil
}

func (s *Service) CheckGatewayAccess(ctx context.Context, gatewayBearerToken string, req sohaapi.HostedAppGatewayAccessCheckRequest) (sohaapi.HostedAppGatewayAccessDecision, error) {
	settings, err := s.verifyGatewayToken(ctx, gatewayBearerToken)
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	}
	rawHost := strings.TrimSpace(req.Host)
	if rawHost == "" || len(rawHost) > 253 {
		return sohaapi.HostedAppGatewayAccessDecision{}, apperrors.ErrInvalidArgument
	}
	if settings.AppsDomain == "" || !settings.Enabled {
		return sohaapi.HostedAppGatewayAccessDecision{
			Decision: sohaapi.HostedAppAccessDecisionKindOffline,
			Host:     rawHost,
			Reason:   "workspace App Hosting is currently disabled",
		}, nil
	}

	dnsLabel, normalizedHost, err := domainapphosting.ParseHostedAppHostForScheme(rawHost, settings.AppsDomain, settings.PublicScheme)
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{
			Decision: sohaapi.HostedAppAccessDecisionKindNotFound,
			Host:     rawHost,
			Reason:   "unknown or reserved application host",
		}, nil
	}

	app, err := s.repo.GetAppByDNSLabel(ctx, dnsLabel)
	if errors.Is(err, apperrors.ErrNotFound) {
		return sohaapi.HostedAppGatewayAccessDecision{
			Decision: sohaapi.HostedAppAccessDecisionKindNotFound,
			DNSLabel: dnsLabel,
			Host:     normalizedHost,
			Reason:   "hosted application not found",
		}, nil
	}
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	}

	return s.evaluateLiveAppAccess(ctx, app, settings, normalizedHost, strings.TrimSpace(req.UserID), strings.TrimSpace(req.SessionID))
}

func (s *Service) evaluateLiveAppAccess(ctx context.Context, app domainapphosting.App, settings domainapphosting.Settings, normalizedHost, userID, sessionID string) (sohaapi.HostedAppGatewayAccessDecision, error) {
	base := sohaapi.HostedAppGatewayAccessDecision{
		AppID:           app.ID,
		DNSLabel:        app.DNSLabel,
		Host:            normalizedHost,
		AppStatus:       app.Status,
		RouteGeneration: app.RouteGeneration,
	}
	if app.Status != sohaapi.HostedAppStatusOnline || strings.TrimSpace(app.ActiveRevisionID) == "" {
		base.Decision = sohaapi.HostedAppAccessDecisionKindOffline
		base.Reason = offlineDecisionReason(app)
		return base, nil
	}
	if userID == "" || sessionID == "" {
		base.Decision = sohaapi.HostedAppAccessDecisionKindUnauthenticated
		return base, nil
	}
	if active, err := s.isBrowserSessionActive(ctx, sessionID, userID); err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	} else if !active {
		base.Decision = sohaapi.HostedAppAccessDecisionKindUnauthenticated
		base.Reason = "source browser session is inactive or revoked"
		return base, nil
	}
	visitor, allowed, err := s.checkLiveVisitorPermission(ctx, userID, app, settings)
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	}
	if !allowed {
		base.Decision = sohaapi.HostedAppAccessDecisionKindForbidden
		base.Reason = "access denied by application visibility policy"
		return base, nil
	}
	return s.buildAllowedAccessDecision(ctx, base, app, visitor)
}

func (s *Service) checkLiveVisitorPermission(ctx context.Context, userID string, app domainapphosting.App, settings domainapphosting.Settings) (domainapphosting.VisitorProfile, bool, error) {
	if s.audience == nil {
		return domainapphosting.VisitorProfile{}, false, apperrors.ErrServiceUnavailable
	}
	visitor, err := s.audience.ResolveVisitor(ctx, userID)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, domainapphosting.ErrVisitorDisabled) {
		return domainapphosting.VisitorProfile{}, false, nil
	}
	if err != nil {
		return domainapphosting.VisitorProfile{}, false, err
	}
	livePrincipal := domainidentity.Principal{
		UserID:   visitor.ID,
		UserName: visitor.Username,
		Email:    visitor.Email,
		Teams:    visitor.TeamIDs,
	}
	return visitor, domainapphosting.CanAccess(livePrincipal, app, settings), nil
}

func (s *Service) buildAllowedAccessDecision(ctx context.Context, base sohaapi.HostedAppGatewayAccessDecision, app domainapphosting.App, visitor domainapphosting.VisitorProfile) (sohaapi.HostedAppGatewayAccessDecision, error) {
	rev, err := s.repo.GetRevision(ctx, app.ID, app.ActiveRevisionID)
	if errors.Is(err, apperrors.ErrNotFound) || (err == nil && rev.Status != sohaapi.HostedAppRevisionStatusReady) {
		base.Decision = sohaapi.HostedAppAccessDecisionKindOffline
		base.Reason = "active revision is unavailable"
		return base, nil
	}
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	}
	identity := visitor.ToPairwiseVisitorIdentity(s.visitorIDSecret, app.ID)
	s.addOptionalVisitorIdentity(ctx, app, visitor, &identity)
	base.Decision = sohaapi.HostedAppAccessDecisionKindAllow
	base.ActiveRevisionID = rev.ID
	base.User = &identity
	if rev.Kind == sohaapi.HostedAppKindDynamic {
		if !s.DynamicEnabled() || rev.ImageRef == "" || !dynamicRevisionPattern.MatchString(rev.ID) {
			base.Decision = sohaapi.HostedAppAccessDecisionKindOffline
			base.Reason = "dynamic revision is unavailable"
			base.User = nil
			return base, nil
		}
		base.RuntimeService = "soha-app-" + strings.TrimPrefix(rev.ID, "rev_")
		return base, nil
	}
	base.SpaFallback = app.SpaFallback
	base.StorageIntegrationID = rev.StorageIntegrationID
	base.StoragePrefix = rev.StoragePrefix
	base.Files = slices.Clone(rev.Files)
	return base, nil
}

func offlineDecisionReason(app domainapphosting.App) string {
	if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		if app.SuspendedReason != "" {
			return "suspended by administrator: " + app.SuspendedReason
		}
		return "suspended by administrator"
	}
	return "application is offline"
}
