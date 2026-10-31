package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) CLIAuthOptions(ctx context.Context) domainidentity.CLIAuthOptions {
	methods := []string{"key"}
	if s.cfg.CLI.Enabled {
		methods = append(methods, "device_code")
		if s.cfg.CLI.AllowPassword && !s.cfg.LoginVerification.SliderEnabled {
			for _, p := range s.ListProviders(ctx) {
				if p.Type == "password" && p.Enabled {
					methods = append(methods, "password")
					break
				}
			}
		}
	}
	return domainidentity.CLIAuthOptions{Enabled: s.cfg.CLI.Enabled, SupportsGovernedAI: true, ProtocolVersion: 2, SupportedMethods: methods, VerificationOrigin: s.cfg.CLI.PublicURL, DeviceCodeExpiresIn: int(s.cfg.CLI.DeviceTTL / time.Second), Interval: int(s.cfg.CLI.PollInterval / time.Second), SessionIdleExpiresIn: int(s.cfg.CLI.IdleTTL / time.Second), SessionAbsoluteExpiresIn: int(s.cfg.CLI.AbsoluteTTL / time.Second)}
}

func (s *Service) issueCLISession(ctx context.Context, userID string, ceiling []string, deviceName, clientVersion string) (domainidentity.AuthResult, error) {
	if err := s.cliAvailable(true); err != nil {
		return domainidentity.AuthResult{}, err
	}
	principal, err := s.CurrentPrincipal(ctx, userID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	principal, err = appaccess.RuntimePrincipalWithin(ctx, s.permissions, principal, ceiling)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	state, err := s.requireActiveAuthzState(ctx, userID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	now := s.cliNow().UTC()
	absolute := now.Add(s.cfg.CLI.AbsoluteTTL)
	idle := minCLITime(now.Add(s.cfg.CLI.IdleTTL), absolute)
	id := uuid.NewString()
	secret, err := cliRandomSecret()
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	session := domainidentity.Session{ID: id, UserID: userID, RefreshTokenID: uuid.NewString(), ProviderType: "cli_v2", Status: "active", ExpiresAt: idle, LastSeenAt: now, AuthzVersion: state.AuthzVersion, Metadata: map[string]any{"cliProtocolVersion": 2, "cliAbsoluteExpiresAt": absolute.Format(time.RFC3339Nano), "cliPermissionKeys": slices.Clone(principal.PermissionKeys), "deviceName": deviceName, "clientVersion": clientVersion}}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return domainidentity.AuthResult{}, err
	}
	refresh := cliRefreshPrefix + secret
	if err := s.cliState.SaveCLIRefresh(ctx, domainidentity.CLIRefreshCredential{Hash: cliHash(refresh), SessionID: id, Generation: 1, IssuedAt: now, ExpiresAt: absolute}); err != nil {
		return domainidentity.AuthResult{}, err
	}
	return s.cliAuthResult(principal, session, state.AuthzVersion, refresh, absolute)
}

func (s *Service) cliAuthResult(principal domainidentity.Principal, session domainidentity.Session, version int64, refresh string, absolute time.Time) (domainidentity.AuthResult, error) {
	ttl := min(s.cfg.CLI.AccessTTL, time.Until(session.ExpiresAt))
	if ttl <= 0 {
		return domainidentity.AuthResult{}, apperrors.ErrUnauthorized
	}
	access, claims, err := s.signSessionAccessToken(principal, session.ID, version, ttl, "cli_access")
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	principal.AccessTokenID = cliSessionTokenPrefix + session.ID
	return domainidentity.AuthResult{User: principal, Tokens: domainidentity.TokenSet{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresAt: claims.ExpiresAt.Time, ExpiresIn: int64(time.Until(claims.ExpiresAt.Time) / time.Second), CredentialKind: "session", ProtocolVersion: 2, SessionID: session.ID, RefreshExpiresAt: &session.ExpiresAt, SessionAbsoluteExpiresAt: &absolute}}, nil
}

func (s *Service) LoginCLIWithPassword(ctx context.Context, login, password, source string) (domainidentity.AuthResult, error) {
	if err := s.cliAvailable(true); err != nil {
		return domainidentity.AuthResult{}, err
	}
	if !s.cfg.CLI.AllowPassword || s.cfg.LoginVerification.SliderEnabled {
		return domainidentity.AuthResult{}, cliError(apperrors.ErrMFARequired, "interactive_auth_required", "Use browser device login to complete the configured authentication checks")
	}
	if err := s.cliPasswordBudget(ctx, login, source); err != nil {
		return domainidentity.AuthResult{}, err
	}
	principal, err := s.authenticatePassword(ctx, login, password)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	if err := s.requireCLIMFA(ctx, principal.UserID, ""); err != nil {
		return domainidentity.AuthResult{}, err
	}
	permissions, err := s.cliPermissions(ctx, principal, nil)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	var result domainidentity.AuthResult
	err = s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		var e error
		result, e = s.issueCLISession(ctx, principal.UserID, permissions, "Password login", "unknown")
		return e
	})
	if err == nil {
		_ = s.recordAudit(ctx, principal, "cli.login", "success", "CLI password session issued", map[string]any{"sessionId": result.Tokens.SessionID})
	}
	return result, err
}

func (s *Service) cliPasswordBudget(ctx context.Context, login, source string) error {
	sourceHash, err := s.cliSourceHash(source)
	if err != nil {
		return err
	}
	// Bind known aliases to the same account without storing login names.
	account := strings.ToLower(strings.TrimSpace(login))
	user, err := s.passwords.FindByLogin(ctx, strings.TrimSpace(login))
	if err == nil {
		account = user.ID
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	accountHash, err := s.cliSourceHash("password-account:" + account)
	if err != nil {
		return err
	}
	var policyErr error
	err = s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		if e := s.cliBudget(ctx, "password:source:"+sourceHash, 10); e != nil {
			return e
		}
		// Roll back new source rows if global admission is saturated.
		if e := s.cliBudget(ctx, "password:global", 1000); e != nil {
			return e
		}
		// An account rejection still consumes source/global admission. All
		// admitted attempts count, including failed authentication and unknown users.
		policyErr = s.cliBudget(ctx, "password:account:"+accountHash, 10)
		return nil
	})
	if err != nil {
		return err
	}
	return policyErr
}

func (s *Service) RefreshCLISession(ctx context.Context, req domainidentity.CLIRefreshRequest) (domainidentity.AuthResult, error) {
	if err := s.cliAvailable(false); err != nil {
		return domainidentity.AuthResult{}, err
	}
	if !validCLIRefresh(req.RefreshToken) {
		return domainidentity.AuthResult{}, apperrors.ErrUnauthorized
	}
	requestHash, err := cliRequestHash(req.RefreshRequestID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	var result domainidentity.AuthResult
	var businessErr error
	var auditAction string
	var auditPrincipal domainidentity.Principal
	var auditSessionID string
	err = s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		credential, e := s.cliState.GetCLIRefresh(ctx, cliHash(req.RefreshToken))
		if e != nil {
			return apperrors.ErrUnauthorized
		}
		session, e := s.sessions.GetAuthSessionByID(ctx, credential.SessionID)
		if e != nil {
			return apperrors.ErrUnauthorized
		}
		principal, e := s.cliSessionPrincipal(ctx, session)
		if e != nil {
			return e
		}
		auditPrincipal, auditSessionID = principal, session.ID
		if credential.RequestHash != "" {
			if !cliEqual(credential.RequestHash, requestHash) {
				if e := s.sessionAdmin.RevokeSessionByID(ctx, session.ID); e != nil {
					return e
				}
				businessErr = cliError(apperrors.ErrUnauthorized, "refresh_replay_detected", "Refresh credential reuse revoked this CLI session; log in again")
				auditAction = "cli.refresh.replay"
				return nil
			}
			result, e = s.openCLIReceipt(ctx, credential.Hash+":"+requestHash, credential.Receipt, credential.ReceiptExpiresAt)
			auditAction = "cli.refresh.recover"
			return e
		}
		if e := s.cliBudget(ctx, "refresh:"+session.ID, 60); e != nil {
			businessErr = e
			return nil
		}
		result, e = s.rotateCLIRefresh(ctx, principal, session, credential, requestHash)
		auditAction = "cli.refresh"
		return e
	})
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	s.auditCLIRefresh(ctx, auditPrincipal, auditAction, auditSessionID)
	if businessErr != nil {
		return domainidentity.AuthResult{}, businessErr
	}
	return result, nil
}

func (s *Service) auditCLIRefresh(ctx context.Context, principal domainidentity.Principal, action, sessionID string) {
	if action == "" {
		return
	}
	result, summary := "success", "CLI session refresh completed"
	if action == "cli.refresh.replay" {
		result, summary = "deny", "CLI refresh replay revoked its session"
	}
	_ = s.recordAudit(ctx, principal, action, result, summary, map[string]any{"sessionId": sessionID})
}

func (s *Service) rotateCLIRefresh(ctx context.Context, principal domainidentity.Principal, session domainidentity.Session, used domainidentity.CLIRefreshCredential, requestHash string) (domainidentity.AuthResult, error) {
	absolute, err := cliAbsoluteExpiry(session)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	state, err := s.requireActiveAuthzState(ctx, session.UserID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	now := s.cliNow().UTC()
	session.ExpiresAt = minCLITime(now.Add(s.cfg.CLI.IdleTTL), absolute)
	if err := s.cliSessions.UpdateCLISession(ctx, session.ID, session.ExpiresAt, now, state.AuthzVersion); err != nil {
		return domainidentity.AuthResult{}, err
	}
	secret, err := cliRandomSecret()
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	refresh := cliRefreshPrefix + secret
	result, err := s.cliAuthResult(principal, session, state.AuthzVersion, refresh, absolute)
	if err != nil {
		return result, err
	}
	receipt, err := s.sealCLIReceipt(used.Hash+":"+requestHash, result)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	used.RequestHash = requestHash
	used.Receipt = receipt
	used.ReceiptExpiresAt = now.Add(cliReceiptWindow)
	used.ReplacementHash = cliHash(refresh)
	used.ConsumedAt = &now
	if err := s.cliState.SaveCLIRefresh(ctx, used); err != nil {
		return domainidentity.AuthResult{}, err
	}
	if err := s.cliState.SaveCLIRefresh(ctx, domainidentity.CLIRefreshCredential{Hash: used.ReplacementHash, SessionID: session.ID, Generation: used.Generation + 1, IssuedAt: now, ExpiresAt: absolute}); err != nil {
		return domainidentity.AuthResult{}, err
	}
	return result, nil
}

func (s *Service) LogoutCLISession(ctx context.Context, refresh string) error {
	if err := s.cliAvailable(false); err != nil {
		return err
	}
	if !validCLIRefresh(refresh) {
		return apperrors.ErrUnauthorized
	}
	var revoked domainidentity.Session
	err := s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		record, err := s.cliState.GetCLIRefresh(ctx, cliHash(refresh))
		if err != nil {
			return apperrors.ErrUnauthorized
		}
		session, err := s.sessions.GetAuthSessionByID(ctx, record.SessionID)
		if err != nil || session.ProviderType != "cli_v2" {
			return apperrors.ErrUnauthorized
		}
		revoked = session
		return s.sessionAdmin.RevokeSessionByID(ctx, session.ID)
	})
	if err == nil {
		_ = s.recordAudit(ctx, domainidentity.Principal{UserID: revoked.UserID}, "cli.logout", "success", "CLI session logged out", map[string]any{"sessionId": revoked.ID})
	}
	return err
}
func validCLIRefresh(value string) bool {
	return strings.HasPrefix(value, cliRefreshPrefix) && cliSecretPattern.MatchString(strings.TrimPrefix(value, cliRefreshPrefix))
}
func minCLITime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *Service) GetAuthContext(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext) (domainidentity.CLIAuthContext, error) {
	if access.SubjectID == "" || access.TokenKind == "" {
		return domainidentity.CLIAuthContext{}, apperrors.ErrUnauthorized
	}
	permissions, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return domainidentity.CLIAuthContext{}, err
	}
	kind := access.TokenKind
	if kind == "session_access" {
		kind = "session"
	}
	result := domainidentity.CLIAuthContext{CredentialKind: kind, SubjectType: access.SubjectType, SubjectID: access.SubjectID, DisplayName: principal.UserName, TokenID: access.TokenID, SessionID: access.SessionID, PermissionKeys: permissions, Scopes: slices.Clone(access.Scopes)}
	result.GovernedAI = principal.GovernedAI
	if result.DisplayName == "" {
		result.DisplayName = access.SubjectID
	}
	if result.PermissionKeys == nil {
		result.PermissionKeys = []string{}
	}
	if result.Scopes == nil {
		result.Scopes = []string{}
	}
	if !access.ExpiresAt.IsZero() {
		result.ExpiresAt = &access.ExpiresAt
	}
	if access.SessionID != "" {
		session, err := s.sessions.GetAuthSessionByID(ctx, access.SessionID)
		if err != nil {
			return domainidentity.CLIAuthContext{}, err
		}
		if session.ProviderType == "cli_v2" {
			absolute, err := cliAbsoluteExpiry(session)
			if err != nil {
				return domainidentity.CLIAuthContext{}, err
			}
			result.RefreshExpiresAt = &session.ExpiresAt
			result.SessionAbsoluteExpiresAt = &absolute
		}
	}
	return result, nil
}

func (s *Service) ListCLISessions(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, limit int, cursor string) (domainidentity.CLISessionPage, error) {
	if access.TokenKind != "session_access" || access.SubjectType != "user" || access.SubjectID != principal.UserID {
		return domainidentity.CLISessionPage{}, apperrors.ErrAccessDenied
	}
	if limit < 1 || limit > 100 {
		return domainidentity.CLISessionPage{}, apperrors.ErrInvalidArgument
	}
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil || len(cursor) != 36 {
			return domainidentity.CLISessionPage{}, apperrors.ErrInvalidArgument
		}
	}
	if isNilDependency(s.cliSessions) {
		return domainidentity.CLISessionPage{}, apperrors.ErrServiceUnavailable
	}
	rows, err := s.cliSessions.ListCLISessionRecords(ctx, principal.UserID, cursor, limit+1)
	if err != nil {
		return domainidentity.CLISessionPage{}, err
	}
	result := domainidentity.CLISessionPage{Items: make([]domainidentity.CLISession, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		result.NextCursor = rows[limit-1].ID
		rows = rows[:limit]
	}
	for _, r := range rows {
		absolute, err := cliAbsoluteExpiry(domainidentity.Session{Metadata: r.Metadata})
		if err != nil {
			return domainidentity.CLISessionPage{}, err
		}
		status := r.Status
		if status == "active" && (!r.ExpiresAt.After(s.cliNow().UTC()) || !absolute.After(s.cliNow().UTC())) {
			status = "expired"
		}
		device, _ := r.Metadata["deviceName"].(string)
		version, _ := r.Metadata["clientVersion"].(string)
		result.Items = append(result.Items, domainidentity.CLISession{ID: r.ID, DeviceName: device, ClientVersion: version, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, ExpiresAt: r.ExpiresAt, AbsoluteExpiresAt: absolute, Status: status, Current: r.ID == access.SessionID})
	}
	return result, nil
}

func (s *Service) RevokeOwnCLISession(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, id string) error {
	if access.TokenKind != "session_access" || access.SubjectType != "user" || access.SubjectID != principal.UserID {
		return apperrors.ErrAccessDenied
	}
	session, err := s.sessions.GetAuthSessionByID(ctx, id)
	if err != nil || session.UserID != principal.UserID || session.ProviderType != "cli_v2" {
		return apperrors.ErrNotFound
	}
	if err := s.sessionAdmin.RevokeSessionByID(ctx, id); err != nil {
		return err
	}
	_ = s.recordAudit(ctx, principal, "cli.session.revoke", "success", "CLI session revoked", map[string]any{"sessionId": id})
	return nil
}

func (s *Service) CleanupCLIAuth(ctx context.Context) error {
	if isNilDependency(s.cliSessions) {
		return nil
	}
	if err := s.cliSessions.CleanupCLIAuth(ctx); err != nil {
		return fmt.Errorf("clean CLI authentication state: %w", err)
	}
	return nil
}
