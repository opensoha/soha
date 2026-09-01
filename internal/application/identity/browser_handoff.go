package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	browserHandoffKind  = "browser_session_handoff"
	browserHandoffTTL   = 60 * time.Second
	browserHandoffMaxID = 128
)

type browserHandoffPayload struct {
	ApplicationID      string `json:"applicationId"`
	ApplicationName    string `json:"applicationName"`
	ApplicationIconURL string `json:"applicationIconUrl,omitempty"`
	UserID             string `json:"userId"`
	AccountName        string `json:"accountName"`
	SourceSessionID    string `json:"sourceSessionId"`
	Audience           string `json:"audience"`
	DestinationURL     string `json:"destinationUrl"`
}

func (s *Service) CreateBrowserHandoff(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, application domainportal.Application, audience string) (domainportal.BrowserHandoff, error) {
	if principal.UserID == "" || access.TokenKind != "session_access" || access.SessionID == "" {
		s.auditBrowserHandoff(ctx, principal, application.ID, "deny", "session_required")
		return domainportal.BrowserHandoff{}, browserHandoffError(apperrors.ErrUnauthorized, "browser_handoff_session_required", "desktop session is required", "需要桌面登录会话")
	}
	audience, err := normalizeBrowserHandoffOrigin(audience)
	if err != nil {
		s.auditBrowserHandoff(ctx, principal, application.ID, "deny", "audience_invalid")
		return domainportal.BrowserHandoff{}, err
	}
	if err := s.validateBrowserHandoffSource(ctx, principal.UserID, access.SessionID); err != nil {
		s.auditBrowserHandoff(ctx, principal, application.ID, "deny", "source_session_inactive")
		return domainportal.BrowserHandoff{}, err
	}
	destination := strings.TrimSpace(application.LaunchURL)
	if !validBrowserHandoffDestination(destination) {
		s.auditBrowserHandoff(ctx, principal, application.ID, "deny", "destination_invalid")
		return domainportal.BrowserHandoff{}, browserHandoffError(apperrors.ErrInvalidArgument, "browser_handoff_invalid_destination", "application destination is invalid", "应用目标地址无效")
	}
	accountName := strings.TrimSpace(principal.UserName)
	if accountName == "" {
		accountName = "Soha user"
	}

	id := uuid.NewString()
	expiresAt := time.Now().UTC().Add(browserHandoffTTL)
	payload := browserHandoffPayload{
		ApplicationID: application.ID, ApplicationName: application.Name, ApplicationIconURL: application.IconURL,
		UserID: principal.UserID, AccountName: accountName, SourceSessionID: access.SessionID,
		Audience: audience, DestinationURL: destination,
	}
	payloadMap, err := browserHandoffPayloadMap(payload)
	if err != nil {
		return domainportal.BrowserHandoff{}, err
	}
	if err := s.ephemeralTokens.CreateEphemeralToken(ctx, domainidentity.EphemeralToken{
		Token: id, Kind: browserHandoffKind, Payload: payloadMap, ExpiresAt: expiresAt,
	}); err != nil {
		return domainportal.BrowserHandoff{}, fmt.Errorf("store browser handoff: %w", err)
	}
	s.auditBrowserHandoff(ctx, principal, application.ID, "success", "created")
	return domainportal.BrowserHandoff{
		ID: id, Application: browserHandoffApplication(payload), AccountName: accountName, Status: "pending", ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) GetBrowserHandoff(ctx context.Context, handoffID string) (domainportal.BrowserHandoff, error) {
	handoffID, token, payload, err := s.loadBrowserHandoff(ctx, handoffID)
	if err != nil {
		s.auditBrowserHandoff(ctx, domainidentity.Principal{}, "", "deny", "expired_or_consumed")
		return domainportal.BrowserHandoff{}, err
	}
	return domainportal.BrowserHandoff{
		ID: handoffID, Application: browserHandoffApplication(payload), AccountName: payload.AccountName,
		Status: "pending", ExpiresAt: token.ExpiresAt,
	}, nil
}

func (s *Service) CompleteBrowserHandoff(ctx context.Context, handoffID, origin, browserAccessToken, browserRefreshToken string) (domainportal.BrowserHandoffCompletion, domainidentity.AuthResult, error) {
	handoffID, _, payload, err := s.loadBrowserHandoff(ctx, handoffID)
	if err != nil {
		s.auditBrowserHandoff(ctx, domainidentity.Principal{}, "", "deny", "expired_or_consumed")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, err
	}
	actor := domainidentity.Principal{UserID: payload.UserID, UserName: payload.AccountName}
	origin, err = normalizeBrowserHandoffOrigin(origin)
	if err != nil || origin != payload.Audience {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "origin_mismatch")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, browserHandoffError(
			apperrors.ErrInvalidArgument, "browser_handoff_origin_invalid", "browser handoff origin is invalid", "浏览器授权来源无效",
		)
	}
	currentUserID, err := s.currentBrowserUserID(ctx, browserAccessToken, browserRefreshToken)
	if err != nil {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "account_conflict")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, err
	}
	if currentUserID != "" && currentUserID != payload.UserID {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "account_conflict")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, browserHandoffError(
			apperrors.ErrConflict, "browser_handoff_account_conflict", "browser is signed in as a different account", "浏览器已登录其他账号",
		)
	}

	token, err := s.ephemeralTokens.ConsumeEphemeralToken(ctx, handoffID, browserHandoffKind)
	if err != nil {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "replayed_or_expired")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, browserHandoffExpired()
	}
	payload, err = decodeBrowserHandoffPayload(token)
	if err != nil {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "ticket_invalid")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, err
	}
	if payload.Audience != origin {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "audience_mismatch")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, browserHandoffExpired()
	}
	if err := s.validateBrowserHandoffSource(ctx, payload.UserID, payload.SourceSessionID); err != nil {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "source_session_inactive")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, err
	}
	principal, err := s.loadPrincipal(ctx, payload.UserID)
	if err != nil {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "user_inactive")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, err
	}
	result, err := s.issueAuthResult(ctx, principal, "desktop-handoff")
	if err != nil {
		s.auditBrowserHandoff(ctx, actor, payload.ApplicationID, "deny", "session_issue_failed")
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, err
	}
	s.auditBrowserHandoff(ctx, principal, payload.ApplicationID, "success", "completed")
	return domainportal.BrowserHandoffCompletion{Status: "completed", DestinationURL: payload.DestinationURL}, result, nil
}

func (s *Service) loadBrowserHandoff(ctx context.Context, handoffID string) (string, domainidentity.EphemeralToken, browserHandoffPayload, error) {
	handoffID, err := normalizeBrowserHandoffID(handoffID)
	if err != nil {
		return "", domainidentity.EphemeralToken{}, browserHandoffPayload{}, err
	}
	token, err := s.ephemeralTokens.GetEphemeralToken(ctx, handoffID, browserHandoffKind)
	if err != nil {
		return "", domainidentity.EphemeralToken{}, browserHandoffPayload{}, browserHandoffExpired()
	}
	payload, err := decodeBrowserHandoffPayload(token)
	if err != nil {
		return "", domainidentity.EphemeralToken{}, browserHandoffPayload{}, err
	}
	return handoffID, token, payload, nil
}

func (s *Service) currentBrowserUserID(ctx context.Context, accessToken, refreshToken string) (string, error) {
	var accessUserID string
	if principal, access, err := s.ParseAccessToken(ctx, strings.TrimSpace(accessToken)); err == nil && access.TokenKind == "session_access" {
		accessUserID = principal.UserID
	}
	var refreshUserID string
	if claims, err := s.parseToken(strings.TrimSpace(refreshToken), "refresh"); err == nil {
		if session, sessionErr := s.sessions.GetSessionByRefreshID(ctx, claims.ID); sessionErr == nil && session.Status == "active" && session.ExpiresAt.After(time.Now().UTC()) && session.UserID == claims.Subject {
			if _, stateErr := s.requireActiveAuthzState(ctx, claims.Subject); stateErr == nil {
				refreshUserID = claims.Subject
			}
		}
	}
	if accessUserID != "" && refreshUserID != "" && accessUserID != refreshUserID {
		return "", browserHandoffError(apperrors.ErrConflict, "browser_handoff_account_conflict", "browser credentials belong to different accounts", "浏览器凭据属于不同账号")
	}
	if refreshUserID != "" {
		return refreshUserID, nil
	}
	return accessUserID, nil
}

func (s *Service) validateBrowserHandoffSource(ctx context.Context, userID, sessionID string) error {
	session, err := s.sessions.GetAuthSessionByID(ctx, sessionID)
	if err != nil || session.UserID != userID || session.Status != "active" || session.ExpiresAt.Before(time.Now().UTC()) {
		return browserHandoffExpired()
	}
	return nil
}

func decodeBrowserHandoffPayload(token domainidentity.EphemeralToken) (browserHandoffPayload, error) {
	raw, err := json.Marshal(token.Payload)
	if err != nil {
		return browserHandoffPayload{}, fmt.Errorf("marshal browser handoff payload: %w", err)
	}
	var payload browserHandoffPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return browserHandoffPayload{}, browserHandoffExpired()
	}
	canonicalAudience, audienceErr := normalizeBrowserHandoffOrigin(payload.Audience)
	if audienceErr != nil || canonicalAudience != payload.Audience || payload.ApplicationID == "" || payload.ApplicationName == "" || payload.UserID == "" || payload.AccountName == "" || payload.SourceSessionID == "" || !validBrowserHandoffDestination(payload.DestinationURL) {
		return browserHandoffPayload{}, browserHandoffExpired()
	}
	return payload, nil
}

func browserHandoffPayloadMap(payload browserHandoffPayload) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal browser handoff: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode browser handoff: %w", err)
	}
	return result, nil
}

func normalizeBrowserHandoffID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > browserHandoffMaxID {
		return "", browserHandoffExpired()
	}
	return value, nil
}

func validBrowserHandoffDestination(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Opaque != "" || strings.Contains(value, "\\") {
		return false
	}
	if parsed.IsAbs() {
		return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
	}
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
}

func normalizeBrowserHandoffOrigin(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", browserHandoffError(apperrors.ErrInvalidArgument, "browser_handoff_origin_invalid", "browser handoff origin is invalid", "浏览器授权来源无效")
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}

func browserHandoffApplication(payload browserHandoffPayload) domainportal.BrowserHandoffApplication {
	return domainportal.BrowserHandoffApplication{ID: payload.ApplicationID, Name: payload.ApplicationName, IconURL: payload.ApplicationIconURL}
}

func (s *Service) AuditBrowserHandoffFailure(ctx context.Context, principal domainidentity.Principal, applicationID, reason string) {
	s.auditBrowserHandoff(ctx, principal, applicationID, "deny", reason)
}

func (s *Service) auditBrowserHandoff(ctx context.Context, principal domainidentity.Principal, applicationID, result, reason string) {
	metadata := map[string]any{"reason": reason}
	if applicationID != "" {
		metadata["applicationId"] = applicationID
	}
	requestMetadata := requestctx.FromContext(ctx)
	requestMetadata.Path = ""
	_ = s.recordAudit(requestctx.WithMetadata(ctx, requestMetadata), principal, "browser_handoff", result, "desktop browser handoff", metadata)
}

func browserHandoffExpired() error {
	return browserHandoffError(apperrors.ErrGone, "browser_handoff_expired", "browser handoff is unknown, expired, or consumed", "浏览器授权已失效或已使用")
}

func browserHandoffError(kind error, code, english, chinese string) error {
	return apperrors.NewBusiness(kind, code, english, chinese)
}
