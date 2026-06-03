package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	domainsettings "github.com/soha/soha/internal/domain/settings"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
	userrepo "github.com/soha/soha/internal/repository/user"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

var usernameSanitizer = regexp.MustCompile(`[^a-z0-9._-]+`)

type UserRepository interface {
	FindByLogin(context.Context, string) (userrepo.User, error)
	FindByEmail(context.Context, string) (userrepo.User, error)
	GetByID(context.Context, string) (userrepo.User, error)
	UpsertUser(context.Context, userrepo.User) error
	SetPasswordHash(context.Context, string, string) error
	GetPasswordHash(context.Context, string) (string, error)
	ListRoles(context.Context, string) ([]string, error)
	ReplaceRoleBindings(context.Context, string, []string) error
	ListTeams(context.Context, string) ([]string, error)
	ListProjects(context.Context, string) ([]string, error)
	FindIdentity(context.Context, string, string, string) (userrepo.OIDCIdentity, error)
	ListIdentitiesByUserID(context.Context, string) ([]userrepo.OIDCIdentity, error)
	UpsertOIDCIdentity(context.Context, userrepo.OIDCIdentity) error
	CreateSession(context.Context, userrepo.Session) error
	GetSessionByRefreshID(context.Context, string) (userrepo.Session, error)
	GetAuthSessionByID(context.Context, string) (userrepo.Session, error)
	GetSessionByID(context.Context, string) (domainidentity.SessionRecord, error)
	ListSessionRecords(context.Context, int) ([]domainidentity.SessionRecord, error)
	ListSessionRecordsByUserID(context.Context, string, int) ([]domainidentity.SessionRecord, error)
	RevokeSessionByID(context.Context, string) error
	TouchSession(context.Context, string, time.Time) error
	RevokeSession(context.Context, string) error
	CreateEphemeralToken(context.Context, userrepo.EphemeralToken) error
	ConsumeEphemeralToken(context.Context, string, string) (userrepo.EphemeralToken, error)
}

type GatewayTokenRepository interface {
	GetPersonalAccessTokenByHash(context.Context, string) (domainaigateway.PersonalAccessToken, error)
	TouchPersonalAccessToken(context.Context, string, time.Time) error
	GetServiceAccountTokenByHash(context.Context, string) (domainaigateway.ServiceAccountToken, error)
	TouchServiceAccountToken(context.Context, string, time.Time) error
	GetServiceAccount(context.Context, string) (domainaigateway.ServiceAccount, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type SettingsReader interface {
	ResolveOIDCSettings(context.Context) (cfgpkg.OIDCConfig, error)
	ResolveLoginProviders(context.Context) ([]domainsettings.LoginProviderSettings, string, error)
	ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error)
}

type Service struct {
	cfg         cfgpkg.AuthConfig
	users       UserRepository
	audit       AuditRecorder
	operations  OperationRecorder
	settings    SettingsReader
	permissions *appaccess.PermissionResolver
	gateway     GatewayTokenRepository
}

type tokenClaims struct {
	TokenType string   `json:"token_type"`
	SessionID string   `json:"sid,omitempty"`
	UserName  string   `json:"name,omitempty"`
	Email     string   `json:"email,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	Teams     []string `json:"teams,omitempty"`
	Projects  []string `json:"projects,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	jwt.RegisteredClaims
}

type oidcStatePayload struct {
	Nonce string `json:"nonce"`
}

type oidcExchangePayload struct {
	Result domainidentity.AuthResult `json:"result"`
}

type genericProfile struct {
	ID       string
	Email    string
	Name     string
	Raw      map[string]any
	Provider string
}

type accessTokenEnvelope struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	OpenID      string `json:"open_id"`
	UnionID     string `json:"union_id"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	Code        int    `json:"code"`
	Message     string `json:"message"`
	Data        struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"open_id"`
		UnionID     string `json:"union_id"`
	} `json:"data"`
}

type oidcProfile struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Nonce             string `json:"nonce"`
}

func New(_ context.Context, cfg cfgpkg.AuthConfig, users UserRepository, audit AuditRecorder, operations OperationRecorder, settings SettingsReader, permissions *appaccess.PermissionResolver, gateway GatewayTokenRepository) (*Service, error) {
	return &Service{cfg: cfg, users: users, audit: audit, operations: operations, settings: settings, permissions: permissions, gateway: gateway}, nil
}

func (s *Service) ListProviders(ctx context.Context) []domainidentity.Provider {
	providers := []domainidentity.Provider{{ID: "password", Type: "password", Name: "Password", Enabled: true}}
	loginProviders, _, err := s.loginProviders(ctx)
	if err == nil {
		for _, item := range loginProviders {
			if !item.Enabled {
				continue
			}
			loginURL := fmt.Sprintf("/auth/login/%s/start", url.PathEscape(item.ID))
			if item.Type == "saml" {
				loginURL = ""
			}
			providers = append(providers, domainidentity.Provider{
				ID:       item.ID,
				Type:     item.Type,
				Name:     item.Name,
				Enabled:  item.Enabled,
				LoginURL: loginURL,
			})
		}
		if len(loginProviders) > 0 {
			return providers
		}
	}
	oidcCfg, err := s.oidcConfig(ctx)
	if err == nil && oidcCfg.Enabled {
		providers = append(providers, domainidentity.Provider{
			ID:       firstNonEmpty(strings.TrimSpace(oidcCfg.ProviderName), "oidc-default"),
			Type:     "oidc",
			Name:     oidcCfg.ProviderName,
			Enabled:  true,
			LoginURL: "/auth/oidc/login",
		})
	}
	return providers
}

func (s *Service) LoginWithPassword(ctx context.Context, login, password string) (domainidentity.AuthResult, error) {
	login = strings.TrimSpace(login)
	password = strings.TrimSpace(password)
	if login == "" || password == "" {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: login and password are required", apperrors.ErrInvalidArgument)
	}

	user, err := s.users.FindByLogin(ctx, login)
	if err != nil {
		_ = s.recordAudit(ctx, domainidentity.Principal{UserName: login}, "login", "deny", "password login failed: user not found", map[string]any{"provider": "password"})
		return domainidentity.AuthResult{}, fmt.Errorf("%w: invalid username or password", apperrors.ErrUnauthorized)
	}
	if user.Status != "active" {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: account is not active", apperrors.ErrUnauthorized)
	}

	hash, err := s.users.GetPasswordHash(ctx, user.ID)
	if err != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: invalid username or password", apperrors.ErrUnauthorized)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		_ = s.recordAudit(ctx, domainidentity.Principal{UserID: user.ID, UserName: user.DisplayName, Email: user.Email}, "login", "deny", "password login failed: invalid password", map[string]any{"provider": "password"})
		return domainidentity.AuthResult{}, fmt.Errorf("%w: invalid username or password", apperrors.ErrUnauthorized)
	}

	principal, err := s.loadPrincipal(ctx, user.ID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	result, err := s.issueAuthResult(ctx, principal, "password")
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	_ = s.recordAudit(ctx, principal, "login", "success", "password login succeeded", map[string]any{"provider": "password"})
	return result, nil
}

func (s *Service) RefreshSession(ctx context.Context, refreshToken string) (domainidentity.AuthResult, error) {
	claims, err := s.parseToken(refreshToken, "refresh")
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	session, err := s.users.GetSessionByRefreshID(ctx, claims.ID)
	if err != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: session not found", apperrors.ErrUnauthorized)
	}
	if session.Status != "active" || session.ExpiresAt.Before(time.Now().UTC()) {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: session expired", apperrors.ErrUnauthorized)
	}

	principal, err := s.loadPrincipal(ctx, claims.Subject)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	accessToken, accessClaims, err := s.signAccessToken(principal, session.ID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	if err := s.users.TouchSession(ctx, claims.ID, time.Now().UTC()); err != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("touch session: %w", err)
	}
	return domainidentity.AuthResult{
		User: principal,
		Tokens: domainidentity.TokenSet{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			TokenType:    "Bearer",
			ExpiresIn:    int64(time.Until(accessClaims.ExpiresAt.Time).Seconds()),
			ExpiresAt:    accessClaims.ExpiresAt.Time,
		},
	}, nil
}

func (s *Service) Logout(ctx context.Context, accessToken, refreshToken string) error {
	var principal domainidentity.Principal
	if accessToken != "" {
		parsedPrincipal, accessCtx, err := s.ParseAccessToken(ctx, accessToken)
		if err == nil {
			principal = parsedPrincipal
			if err := s.users.RevokeSessionByID(ctx, accessCtx.SessionID); err != nil && !errors.Is(err, userrepo.ErrNotFound) {
				return fmt.Errorf("revoke session by access token: %w", err)
			}
		}
	}
	if refreshToken != "" {
		claims, err := s.parseToken(refreshToken, "refresh")
		if err != nil {
			return err
		}
		if err := s.users.RevokeSession(ctx, claims.ID); err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
		if principal.UserID == "" {
			loadedPrincipal, err := s.loadPrincipal(ctx, claims.Subject)
			if err == nil {
				principal = loadedPrincipal
			}
		}
	}
	if principal.UserID != "" {
		_ = s.recordAudit(ctx, principal, "logout", "success", "session revoked", nil)
	}
	return nil
}

func (s *Service) ParseAccessToken(ctx context.Context, accessToken string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	if strings.HasPrefix(accessToken, domainaigateway.PersonalAccessTokenPrefix) {
		return s.parsePersonalAccessToken(ctx, accessToken)
	}
	if strings.HasPrefix(accessToken, domainaigateway.ServiceAccountTokenPrefix) {
		return s.parseServiceAccountToken(ctx, accessToken)
	}
	claims, err := s.parseToken(accessToken, "access")
	if err != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, err
	}
	session, err := s.users.GetAuthSessionByID(ctx, claims.SessionID)
	if err != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: session not found", apperrors.ErrUnauthorized)
	}
	if session.Status != "active" || session.ExpiresAt.Before(time.Now().UTC()) {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: session revoked", apperrors.ErrUnauthorized)
	}
	if session.UserID != claims.Subject {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: session subject mismatch", apperrors.ErrUnauthorized)
	}
	principal := principalFromClaims(claims)
	return principal, domainidentity.AccessContext{TokenID: claims.ID, TokenKind: "session_access", SessionID: claims.SessionID, SubjectType: "user", SubjectID: principal.UserID, ExpiresAt: claims.ExpiresAt.Time}, nil
}

func (s *Service) parsePersonalAccessToken(ctx context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	if s.gateway == nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: gateway token store is not configured", apperrors.ErrUnauthorized)
	}
	item, err := s.gateway.GetPersonalAccessTokenByHash(ctx, domainaigateway.HashToken(token))
	if err != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: personal access token not found", apperrors.ErrUnauthorized)
	}
	if item.RevokedAt != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: personal access token revoked", apperrors.ErrUnauthorized)
	}
	if item.ExpiresAt != nil && item.ExpiresAt.Before(time.Now().UTC()) {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: personal access token expired", apperrors.ErrUnauthorized)
	}
	principal, err := s.loadPrincipal(ctx, item.UserID)
	if err != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, err
	}
	principal.PermissionKeys = append([]string(nil), item.PermissionKeys...)
	_ = s.gateway.TouchPersonalAccessToken(ctx, item.ID, time.Now().UTC())
	return principal, domainidentity.AccessContext{
		TokenID:     item.ID,
		TokenKind:   "personal_access_token",
		SubjectType: "user",
		SubjectID:   principal.UserID,
		ExpiresAt:   timePointerValue(item.ExpiresAt),
	}, nil
}

func (s *Service) parseServiceAccountToken(ctx context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	if s.gateway == nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: gateway token store is not configured", apperrors.ErrUnauthorized)
	}
	item, err := s.gateway.GetServiceAccountTokenByHash(ctx, domainaigateway.HashToken(token))
	if err != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: service account token not found", apperrors.ErrUnauthorized)
	}
	if item.RevokedAt != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: service account token revoked", apperrors.ErrUnauthorized)
	}
	if item.ExpiresAt != nil && item.ExpiresAt.Before(time.Now().UTC()) {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: service account token expired", apperrors.ErrUnauthorized)
	}
	account, err := s.gateway.GetServiceAccount(ctx, item.ServiceAccountID)
	if err != nil {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: service account not found", apperrors.ErrUnauthorized)
	}
	if strings.TrimSpace(account.Status) != "active" {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("%w: service account is not active", apperrors.ErrUnauthorized)
	}
	principal := domainidentity.Principal{
		UserID:         "service_account:" + account.ID,
		UserName:       account.Name,
		Roles:          append([]string(nil), account.RoleIDs...),
		Teams:          append([]string(nil), account.TeamIDs...),
		PermissionKeys: append([]string(nil), item.PermissionKeys...),
	}
	_ = s.gateway.TouchServiceAccountToken(ctx, item.ID, time.Now().UTC())
	return principal, domainidentity.AccessContext{
		TokenID:     item.ID,
		TokenKind:   "service_account_token",
		SubjectType: "service_account",
		SubjectID:   account.ID,
		ExpiresAt:   timePointerValue(item.ExpiresAt),
	}, nil
}

func (s *Service) ListActiveSessions(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainidentity.SessionRecord, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemOnlineUsersView); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	items, err := s.users.ListSessionRecords(ctx, limit)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	filtered := make([]domainidentity.SessionRecord, 0, len(items))
	for _, item := range items {
		if item.Status == "active" && item.ExpiresAt.After(now) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *Service) RevokeSessionByID(ctx context.Context, principal domainidentity.Principal, sessionID string) error {
	if err := s.authorize(ctx, principal, appaccess.PermSystemOnlineUsersManage); err != nil {
		return err
	}
	session, err := s.users.GetSessionByID(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	if err := s.users.RevokeSessionByID(ctx, session.ID); err != nil {
		return err
	}
	_ = s.recordAudit(ctx, principal, "session_revoke", "success", "admin revoked user session", map[string]any{
		"targetUserId": session.UserID,
		"sessionId":    session.ID,
	})
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			"system.session.revoke",
			map[string]any{
				"module":       "system",
				"resourceKind": "Session",
				"targetId":     session.ID,
				"targetLabel":  session.UserName,
				"userId":       session.UserID,
			},
			"success",
			"admin revoked user session",
			map[string]any{
				"targetUserId": session.UserID,
				"sessionId":    session.ID,
			},
		))
	}
	return nil
}

func (s *Service) CurrentPrincipal(ctx context.Context, userID string) (domainidentity.Principal, error) {
	return s.loadPrincipal(ctx, userID)
}

func (s *Service) CurrentProfile(ctx context.Context, principal domainidentity.Principal) (domainidentity.UserProfile, error) {
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return domainidentity.UserProfile{}, fmt.Errorf("%w: current user is required", apperrors.ErrUnauthorized)
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domainidentity.UserProfile{}, fmt.Errorf("%w: user not found", apperrors.ErrUnauthorized)
	}
	roles, err := s.users.ListRoles(ctx, userID)
	if err != nil {
		return domainidentity.UserProfile{}, fmt.Errorf("list user roles: %w", err)
	}
	teams, err := s.users.ListTeams(ctx, userID)
	if err != nil {
		return domainidentity.UserProfile{}, fmt.Errorf("list user teams: %w", err)
	}
	projects, err := s.users.ListProjects(ctx, userID)
	if err != nil {
		return domainidentity.UserProfile{}, fmt.Errorf("list user projects: %w", err)
	}
	identities, err := s.users.ListIdentitiesByUserID(ctx, userID)
	if err != nil {
		return domainidentity.UserProfile{}, fmt.Errorf("list user identities: %w", err)
	}
	sessions, err := s.users.ListSessionRecordsByUserID(ctx, userID, 20)
	if err != nil {
		return domainidentity.UserProfile{}, fmt.Errorf("list user sessions: %w", err)
	}

	now := time.Now().UTC()
	activeSessions := make([]domainidentity.SessionRecord, 0, len(sessions))
	for _, session := range sessions {
		if session.Status == "active" && session.ExpiresAt.After(now) {
			activeSessions = append(activeSessions, session)
		}
	}

	linkedIdentities := make([]domainidentity.LinkedIdentity, 0, len(identities)+1)
	if _, err := s.users.GetPasswordHash(ctx, userID); err == nil {
		linkedIdentities = append(linkedIdentities, domainidentity.LinkedIdentity{
			ID:             "password:" + userID,
			ProviderType:   "password",
			ProviderID:     "local",
			ProviderUserID: user.Username,
			DisplayName:    firstNonEmpty(user.DisplayName, user.Username),
			Email:          user.Email,
		})
	} else if err != nil && !errors.Is(err, userrepo.ErrNotFound) {
		return domainidentity.UserProfile{}, fmt.Errorf("load password credential: %w", err)
	}
	for _, identity := range identities {
		linkedIdentities = append(linkedIdentities, toLinkedIdentity(identity))
	}

	return domainidentity.UserProfile{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: firstNonEmpty(user.DisplayName, user.Username),
		Email:       user.Email,
		Status:      user.Status,
		Roles:       roles,
		Teams:       teams,
		Projects:    projects,
		Tags:        append([]string(nil), user.Tags...),
		Identities:  linkedIdentities,
		Sessions:    activeSessions,
		LastLoginAt: latestLoginAt(linkedIdentities, activeSessions),
	}, nil
}

func toLinkedIdentity(identity userrepo.OIDCIdentity) domainidentity.LinkedIdentity {
	var lastLoginAt *time.Time
	if !identity.LastLoginAt.IsZero() {
		value := identity.LastLoginAt
		lastLoginAt = &value
	}
	return domainidentity.LinkedIdentity{
		ID:             identity.ID,
		ProviderType:   identity.ProviderType,
		ProviderID:     identity.ProviderID,
		ProviderUserID: identity.ProviderUserID,
		DisplayName:    firstNonEmpty(nestedString(identity.Profile, "name"), nestedString(identity.Profile, "nick"), nestedString(identity.Profile, "preferred_username")),
		Email:          firstNonEmpty(nestedString(identity.Profile, "email"), nestedString(identity.Profile, "enterprise_email")),
		LastLoginAt:    lastLoginAt,
	}
}

func latestLoginAt(identities []domainidentity.LinkedIdentity, sessions []domainidentity.SessionRecord) *time.Time {
	var latest time.Time
	for _, identity := range identities {
		if identity.LastLoginAt != nil && identity.LastLoginAt.After(latest) {
			latest = *identity.LastLoginAt
		}
	}
	for _, session := range sessions {
		if session.CreatedAt.After(latest) {
			latest = session.CreatedAt
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) BeginOIDCLogin(ctx context.Context) (string, error) {
	return s.BeginProviderLogin(ctx, "")
}

func (s *Service) BeginProviderLogin(ctx context.Context, providerID string) (string, error) {
	provider, err := s.resolveLoginProvider(ctx, providerID)
	if err != nil {
		return "", err
	}
	if !provider.Enabled {
		return "", fmt.Errorf("%w: login provider is disabled", apperrors.ErrNotFound)
	}
	switch provider.Type {
	case "oidc":
		_, _, _, oauthConfig, err := s.oidcRuntimeWithProvider(ctx, provider)
		if err != nil {
			return "", fmt.Errorf("%w: oidc is disabled", apperrors.ErrNotFound)
		}
		state := uuid.NewString()
		nonce := uuid.NewString()
		if err := s.users.CreateEphemeralToken(ctx, userrepo.EphemeralToken{
			Token: state,
			Kind:  oidcStateKind,
			Payload: map[string]any{
				"nonce":      nonce,
				"providerId": provider.ID,
				"type":       provider.Type,
			},
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		}); err != nil {
			return "", fmt.Errorf("store oidc state: %w", err)
		}
		return oauthConfig.AuthCodeURL(state, oidc.Nonce(nonce)), nil
	case "oauth2", "feishu", "dingtalk", "wecom":
		state := uuid.NewString()
		if err := s.users.CreateEphemeralToken(ctx, userrepo.EphemeralToken{
			Token: state,
			Kind:  oauthStateKind,
			Payload: map[string]any{
				"providerId": provider.ID,
				"type":       provider.Type,
			},
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		}); err != nil {
			return "", fmt.Errorf("store oauth state: %w", err)
		}
		if provider.Type == "wecom" {
			return buildWecomAuthorizeURL(provider, state), nil
		}
		oauthConfig := oauth2ConfigFromProvider(provider)
		return oauthConfig.AuthCodeURL(state), nil
	case "saml":
		return "", fmt.Errorf("%w: saml login runtime is not enabled", apperrors.ErrInvalidArgument)
	default:
		return "", fmt.Errorf("%w: unsupported login provider type %s", apperrors.ErrInvalidArgument, provider.Type)
	}
}

func (s *Service) HandleOIDCCallback(ctx context.Context, state, code string) (string, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("%w: missing oidc callback parameters", apperrors.ErrInvalidArgument)
	}
	stateToken, err := s.users.ConsumeEphemeralToken(ctx, state, oidcStateKind)
	if err != nil {
		return "", fmt.Errorf("%w: oidc state missing or expired", apperrors.ErrUnauthorized)
	}
	var statePayload oidcStatePayload
	if payload, marshalErr := json.Marshal(stateToken.Payload); marshalErr != nil {
		return "", fmt.Errorf("encode oidc state: %w", marshalErr)
	} else if err := json.Unmarshal(payload, &statePayload); err != nil {
		return "", fmt.Errorf("decode oidc state: %w", err)
	}

	providerID, _ := stateToken.Payload["providerId"].(string)
	loginProvider, err := s.resolveLoginProvider(ctx, providerID)
	if err != nil {
		return "", err
	}
	oidcCfg, provider, verifier, oauthConfig, err := s.oidcRuntimeWithProvider(ctx, loginProvider)
	if err != nil {
		return "", fmt.Errorf("%w: oidc is disabled", apperrors.ErrNotFound)
	}
	oauthToken, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("exchange oidc code: %w", err)
	}
	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", fmt.Errorf("%w: oidc id_token is missing", apperrors.ErrUnauthorized)
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", fmt.Errorf("verify oidc id_token: %w", err)
	}
	var profile oidcProfile
	if err := idToken.Claims(&profile); err != nil {
		return "", fmt.Errorf("decode oidc claims: %w", err)
	}
	if statePayload.Nonce != "" && profile.Nonce != "" && profile.Nonce != statePayload.Nonce {
		return "", fmt.Errorf("%w: oidc nonce mismatch", apperrors.ErrUnauthorized)
	}
	if profile.Email == "" || profile.Name == "" {
		userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(oauthToken))
		if err == nil {
			var info oidcProfile
			if err := userInfo.Claims(&info); err == nil {
				if profile.Email == "" {
					profile.Email = info.Email
				}
				if profile.Name == "" {
					profile.Name = info.Name
				}
				if profile.PreferredUsername == "" {
					profile.PreferredUsername = info.PreferredUsername
				}
			}
		}
	}
	principal, err := s.reconcileOIDCUser(ctx, profile, oidcCfg)
	if err != nil {
		return "", err
	}
	result, err := s.issueAuthResult(ctx, principal, "oidc")
	if err != nil {
		return "", err
	}
	exchangeCode := uuid.NewString()
	payload, err := json.Marshal(oidcExchangePayload{Result: result})
	if err != nil {
		return "", fmt.Errorf("marshal oidc exchange payload: %w", err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return "", fmt.Errorf("decode oidc exchange payload: %w", err)
	}
	if err := s.users.CreateEphemeralToken(ctx, userrepo.EphemeralToken{
		Token:     exchangeCode,
		Kind:      oidcExchangeKind,
		Payload:   payloadMap,
		ExpiresAt: time.Now().UTC().Add(2 * time.Minute),
	}); err != nil {
		return "", fmt.Errorf("store oidc exchange payload: %w", err)
	}
	redirectURL, err := addQueryValue(oidcCfg.FrontendRedirectURL, "code", exchangeCode)
	if err != nil {
		return "", err
	}
	_ = s.recordAudit(ctx, principal, "login", "success", "oidc login succeeded", map[string]any{"provider": oidcCfg.ProviderName})
	return redirectURL, nil
}

func (s *Service) ConsumeOIDCExchange(ctx context.Context, code string) (domainidentity.AuthResult, error) {
	if strings.TrimSpace(code) == "" {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: exchange code is required", apperrors.ErrInvalidArgument)
	}
	token, err := s.users.ConsumeEphemeralToken(ctx, code, oidcExchangeKind)
	if err != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("%w: exchange code expired", apperrors.ErrUnauthorized)
	}
	var payload oidcExchangePayload
	if rawPayload, marshalErr := json.Marshal(token.Payload); marshalErr != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("encode oidc exchange payload: %w", marshalErr)
	} else if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("decode oidc exchange payload: %w", err)
	}
	return payload.Result, nil
}

func (s *Service) loginProviders(ctx context.Context) ([]domainsettings.LoginProviderSettings, string, error) {
	if s.settings != nil {
		return s.settings.ResolveLoginProviders(ctx)
	}
	return nil, "", nil
}

func (s *Service) resolveLoginProvider(ctx context.Context, providerID string) (domainsettings.LoginProviderSettings, error) {
	if s.settings != nil {
		return s.settings.ResolveLoginProvider(ctx, providerID)
	}
	oidcCfg, err := s.oidcConfig(ctx)
	if err != nil {
		return domainsettings.LoginProviderSettings{}, err
	}
	if !oidcCfg.Enabled {
		return domainsettings.LoginProviderSettings{}, fmt.Errorf("%w: login provider not found", apperrors.ErrNotFound)
	}
	return domainsettings.LoginProviderSettings{
		ID:                  firstNonEmpty(strings.TrimSpace(providerID), strings.TrimSpace(oidcCfg.ProviderName), "oidc-default"),
		Name:                firstNonEmpty(oidcCfg.ProviderName, "OIDC"),
		Type:                "oidc",
		Enabled:             oidcCfg.Enabled,
		ClientID:            oidcCfg.ClientID,
		ClientSecret:        oidcCfg.ClientSecret,
		Issuer:              oidcCfg.Issuer,
		RedirectURL:         oidcCfg.RedirectURL,
		FrontendRedirectURL: oidcCfg.FrontendRedirectURL,
		Scopes:              append([]string(nil), oidcCfg.Scopes...),
		DefaultRoles:        append([]string(nil), oidcCfg.DefaultRoles...),
		UserIDField:         "sub",
		UserNameField:       "name",
		EmailField:          "email",
	}, nil
}

func (s *Service) HandleProviderCallback(ctx context.Context, providerID, state, code string) (string, error) {
	provider, err := s.resolveLoginProvider(ctx, providerID)
	if err != nil {
		return "", err
	}
	switch provider.Type {
	case "oidc":
		return s.HandleOIDCCallback(ctx, state, code)
	case "oauth2", "feishu", "dingtalk", "wecom":
		return s.handleOAuth2Callback(ctx, provider, state, code)
	case "saml":
		return "", fmt.Errorf("%w: saml login runtime is not enabled", apperrors.ErrInvalidArgument)
	default:
		return "", fmt.Errorf("%w: unsupported login provider type %s", apperrors.ErrInvalidArgument, provider.Type)
	}
}

func (s *Service) issueAuthResult(ctx context.Context, principal domainidentity.Principal, providerType string) (domainidentity.AuthResult, error) {
	meta := requestctx.FromContext(ctx)
	sessionID := uuid.NewString()
	refreshID := uuid.NewString()
	accessToken, accessClaims, err := s.signAccessToken(principal, sessionID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	refreshToken, refreshClaims, err := s.signRefreshToken(principal.UserID, sessionID, refreshID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	session := userrepo.Session{
		ID:             sessionID,
		UserID:         principal.UserID,
		RefreshTokenID: refreshID,
		ProviderType:   providerType,
		Status:         "active",
		ExpiresAt:      refreshClaims.ExpiresAt.Time,
		LastSeenAt:     time.Now().UTC(),
		Metadata: map[string]any{
			"roles":     principal.Roles,
			"source":    meta.Source,
			"sourceIp":  meta.SourceIP,
			"userAgent": meta.UserAgent,
		},
	}
	if err := s.users.CreateSession(ctx, session); err != nil {
		return domainidentity.AuthResult{}, fmt.Errorf("create session: %w", err)
	}
	return domainidentity.AuthResult{
		User: principal,
		Tokens: domainidentity.TokenSet{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			TokenType:    "Bearer",
			ExpiresIn:    int64(time.Until(accessClaims.ExpiresAt.Time).Seconds()),
			ExpiresAt:    accessClaims.ExpiresAt.Time,
		},
	}, nil
}

func (s *Service) loadPrincipal(ctx context.Context, userID string) (domainidentity.Principal, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("%w: user not found", apperrors.ErrUnauthorized)
	}
	roles, err := s.users.ListRoles(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("list user roles: %w", err)
	}
	teams, err := s.users.ListTeams(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("list user teams: %w", err)
	}
	projects, err := s.users.ListProjects(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("list user projects: %w", err)
	}
	userName := strings.TrimSpace(user.DisplayName)
	if userName == "" {
		userName = user.Username
	}
	return domainidentity.Principal{
		UserID:   user.ID,
		UserName: userName,
		Email:    user.Email,
		Roles:    roles,
		Teams:    teams,
		Projects: projects,
		Tags:     append([]string(nil), user.Tags...),
	}, nil
}

func (s *Service) reconcileOIDCUser(ctx context.Context, profile oidcProfile, oidcCfg cfgpkg.OIDCConfig) (domainidentity.Principal, error) {
	if profile.Sub == "" {
		return domainidentity.Principal{}, fmt.Errorf("%w: oidc subject is missing", apperrors.ErrUnauthorized)
	}
	if profile.Email == "" {
		profile.Email = fmt.Sprintf("%s@%s.oidc.local", profile.Sub, strings.ReplaceAll(oidcCfg.ProviderName, " ", "-"))
	}
	if profile.Name == "" {
		profile.Name = firstNonEmpty(profile.PreferredUsername, profile.Email, profile.Sub)
	}
	identity, err := s.users.FindIdentity(ctx, "oidc", oidcCfg.ProviderName, profile.Sub)
	var user userrepo.User
	if err == nil {
		user, err = s.users.GetByID(ctx, identity.UserID)
		if err != nil {
			return domainidentity.Principal{}, err
		}
	} else if errors.Is(err, userrepo.ErrNotFound) {
		user, err = s.users.FindByEmail(ctx, profile.Email)
		if errors.Is(err, userrepo.ErrNotFound) {
			user = userrepo.User{
				ID:          uuid.NewString(),
				Username:    normalizeUsername(firstNonEmpty(profile.PreferredUsername, profile.Email, profile.Sub)),
				Email:       strings.ToLower(profile.Email),
				DisplayName: profile.Name,
				Status:      "active",
				Preferences: map[string]any{},
			}
			if err := s.users.UpsertUser(ctx, user); err != nil {
				return domainidentity.Principal{}, fmt.Errorf("create oidc user: %w", err)
			}
		} else if err != nil {
			return domainidentity.Principal{}, fmt.Errorf("find oidc user by email: %w", err)
		}
	} else if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("find oidc identity: %w", err)
	}

	if err := s.users.UpsertOIDCIdentity(ctx, userrepo.OIDCIdentity{
		ID:             uuid.NewString(),
		UserID:         user.ID,
		ProviderType:   "oidc",
		ProviderID:     oidcCfg.ProviderName,
		ProviderUserID: profile.Sub,
		Profile: map[string]any{
			"email": profile.Email,
			"name":  profile.Name,
		},
		LastLoginAt: time.Now().UTC(),
	}); err != nil {
		return domainidentity.Principal{}, fmt.Errorf("upsert oidc identity: %w", err)
	}

	roles, err := s.users.ListRoles(ctx, user.ID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("load oidc user roles: %w", err)
	}
	if len(roles) == 0 && len(oidcCfg.DefaultRoles) > 0 {
		if err := s.users.ReplaceRoleBindings(ctx, user.ID, oidcCfg.DefaultRoles); err != nil {
			return domainidentity.Principal{}, fmt.Errorf("assign default oidc roles: %w", err)
		}
	}
	return s.loadPrincipal(ctx, user.ID)
}

func (s *Service) reconcileExternalUser(ctx context.Context, provider domainsettings.LoginProviderSettings, profile genericProfile) (domainidentity.Principal, error) {
	if strings.TrimSpace(profile.ID) == "" {
		return domainidentity.Principal{}, fmt.Errorf("%w: external subject is missing", apperrors.ErrUnauthorized)
	}
	if profile.Email == "" {
		profile.Email = fmt.Sprintf("%s@%s.login.local", profile.ID, strings.ReplaceAll(provider.ID, " ", "-"))
	}
	if profile.Name == "" {
		profile.Name = firstNonEmpty(profile.Email, profile.ID)
	}
	identity, err := s.users.FindIdentity(ctx, provider.Type, provider.ID, profile.ID)
	var user userrepo.User
	if err == nil {
		user, err = s.users.GetByID(ctx, identity.UserID)
		if err != nil {
			return domainidentity.Principal{}, err
		}
	} else if errors.Is(err, userrepo.ErrNotFound) {
		user, err = s.users.FindByEmail(ctx, profile.Email)
		if errors.Is(err, userrepo.ErrNotFound) {
			user = userrepo.User{
				ID:          uuid.NewString(),
				Username:    normalizeUsername(firstNonEmpty(profile.Name, profile.Email, profile.ID)),
				Email:       strings.ToLower(profile.Email),
				DisplayName: profile.Name,
				Status:      "active",
				Preferences: map[string]any{},
			}
			if err := s.users.UpsertUser(ctx, user); err != nil {
				return domainidentity.Principal{}, fmt.Errorf("create external login user: %w", err)
			}
		} else if err != nil {
			return domainidentity.Principal{}, fmt.Errorf("find external login user by email: %w", err)
		}
	} else if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("find external login identity: %w", err)
	}

	if err := s.users.UpsertOIDCIdentity(ctx, userrepo.OIDCIdentity{
		ID:             uuid.NewString(),
		UserID:         user.ID,
		ProviderType:   provider.Type,
		ProviderID:     provider.ID,
		ProviderUserID: profile.ID,
		Profile:        profile.Raw,
		LastLoginAt:    time.Now().UTC(),
	}); err != nil {
		return domainidentity.Principal{}, fmt.Errorf("upsert external login identity: %w", err)
	}

	roles, err := s.users.ListRoles(ctx, user.ID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("load external user roles: %w", err)
	}
	if len(roles) == 0 && len(provider.DefaultRoles) > 0 {
		if err := s.users.ReplaceRoleBindings(ctx, user.ID, provider.DefaultRoles); err != nil {
			return domainidentity.Principal{}, fmt.Errorf("assign default external login roles: %w", err)
		}
	}
	return s.loadPrincipal(ctx, user.ID)
}

func (s *Service) oidcConfig(ctx context.Context) (cfgpkg.OIDCConfig, error) {
	if s.settings != nil {
		if item, err := s.settings.ResolveOIDCSettings(ctx); err == nil {
			return item, nil
		}
	}
	return s.cfg.OIDC, nil
}

func (s *Service) oidcRuntime(ctx context.Context) (cfgpkg.OIDCConfig, *oidc.Provider, *oidc.IDTokenVerifier, *oauth2.Config, error) {
	oidcCfg, err := s.oidcConfig(ctx)
	if err != nil {
		return cfgpkg.OIDCConfig{}, nil, nil, nil, err
	}
	if !oidcCfg.Enabled {
		return cfgpkg.OIDCConfig{}, nil, nil, nil, fmt.Errorf("%w: oidc is disabled", apperrors.ErrNotFound)
	}
	provider, err := oidc.NewProvider(ctx, oidcCfg.Issuer)
	if err != nil {
		return cfgpkg.OIDCConfig{}, nil, nil, nil, fmt.Errorf("build oidc provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: oidcCfg.ClientID})
	oauthConfig := &oauth2.Config{
		ClientID:     oidcCfg.ClientID,
		ClientSecret: oidcCfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  oidcCfg.RedirectURL,
		Scopes:       oidcCfg.Scopes,
	}
	return oidcCfg, provider, verifier, oauthConfig, nil
}

func (s *Service) oidcRuntimeWithProvider(ctx context.Context, item domainsettings.LoginProviderSettings) (cfgpkg.OIDCConfig, *oidc.Provider, *oidc.IDTokenVerifier, *oauth2.Config, error) {
	oidcCfg := cfgpkg.OIDCConfig{
		Enabled:             item.Enabled,
		ProviderName:        item.Name,
		Issuer:              item.Issuer,
		ClientID:            item.ClientID,
		ClientSecret:        item.ClientSecret,
		RedirectURL:         item.RedirectURL,
		FrontendRedirectURL: item.FrontendRedirectURL,
		Scopes:              append([]string(nil), item.Scopes...),
		DefaultRoles:        append([]string(nil), item.DefaultRoles...),
	}
	if !oidcCfg.Enabled {
		return cfgpkg.OIDCConfig{}, nil, nil, nil, fmt.Errorf("%w: oidc is disabled", apperrors.ErrNotFound)
	}
	provider, err := oidc.NewProvider(ctx, oidcCfg.Issuer)
	if err != nil {
		return cfgpkg.OIDCConfig{}, nil, nil, nil, fmt.Errorf("build oidc provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: oidcCfg.ClientID})
	oauthConfig := &oauth2.Config{
		ClientID:     oidcCfg.ClientID,
		ClientSecret: oidcCfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  oidcCfg.RedirectURL,
		Scopes:       oidcCfg.Scopes,
	}
	return oidcCfg, provider, verifier, oauthConfig, nil
}

func (s *Service) signAccessToken(principal domainidentity.Principal, sessionID string) (string, *tokenClaims, error) {
	now := time.Now().UTC()
	claims := &tokenClaims{
		TokenType: "access",
		SessionID: sessionID,
		UserName:  principal.UserName,
		Email:     principal.Email,
		Roles:     append([]string(nil), principal.Roles...),
		Teams:     append([]string(nil), principal.Teams...),
		Projects:  append([]string(nil), principal.Projects...),
		Tags:      append([]string(nil), principal.Tags...),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.cfg.JWT.Issuer,
			Subject:   principal.UserID,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.JWT.AccessTTL)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", nil, fmt.Errorf("sign access token: %w", err)
	}
	return token, claims, nil
}

func (s *Service) signRefreshToken(userID, sessionID, refreshID string) (string, *tokenClaims, error) {
	now := time.Now().UTC()
	claims := &tokenClaims{
		TokenType: "refresh",
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.cfg.JWT.Issuer,
			Subject:   userID,
			ID:        refreshID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.JWT.RefreshTTL)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", nil, fmt.Errorf("sign refresh token: %w", err)
	}
	return token, claims, nil
}

func (s *Service) parseToken(tokenString, expectedType string) (*tokenClaims, error) {
	tokenString = strings.TrimSpace(strings.TrimPrefix(tokenString, "Bearer "))
	if tokenString == "" {
		return nil, fmt.Errorf("%w: token is required", apperrors.ErrUnauthorized)
	}
	claims := &tokenClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return []byte(s.cfg.JWT.Secret), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: invalid token", apperrors.ErrUnauthorized)
	}
	if claims.TokenType != expectedType {
		return nil, fmt.Errorf("%w: unexpected token type", apperrors.ErrUnauthorized)
	}
	return claims, nil
}

func principalFromClaims(claims *tokenClaims) domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   claims.Subject,
		UserName: claims.UserName,
		Email:    claims.Email,
		Roles:    append([]string(nil), claims.Roles...),
		Teams:    append([]string(nil), claims.Teams...),
		Projects: append([]string(nil), claims.Projects...),
		Tags:     append([]string(nil), claims.Tags...),
	}
}

func hasAdminRole(roles []string) bool {
	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action, result, summary string, metadata map[string]any) error {
	if s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	if metadata == nil {
		metadata = map[string]any{}
	}
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
	})
}

func normalizeUsername(value string) string {
	candidate := strings.ToLower(strings.TrimSpace(value))
	candidate = strings.ReplaceAll(candidate, "@", ".")
	candidate = usernameSanitizer.ReplaceAllString(candidate, "-")
	candidate = strings.Trim(candidate, "-.")
	if candidate == "" {
		return fmt.Sprintf("user-%s", strings.ToLower(uuid.NewString()[:8]))
	}
	return candidate
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func addQueryValue(rawURL, key, value string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse redirect url: %w", err)
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func oauth2ConfigFromProvider(provider domainsettings.LoginProviderSettings) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		RedirectURL:  provider.RedirectURL,
		Scopes:       append([]string(nil), provider.Scopes...),
		Endpoint: oauth2.Endpoint{
			AuthURL:  provider.AuthorizeURL,
			TokenURL: provider.TokenURL,
		},
	}
}

func buildWecomAuthorizeURL(provider domainsettings.LoginProviderSettings, state string) string {
	authorizeURL := strings.TrimSpace(provider.AuthorizeURL)
	if authorizeURL == "" {
		authorizeURL = "https://open.weixin.qq.com/connect/oauth2/authorize"
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		return authorizeURL
	}
	query := parsed.Query()
	query.Set("appid", provider.ClientID)
	query.Set("redirect_uri", provider.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", firstNonEmpty(provider.Scopes...))
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	value := parsed.String()
	if !strings.Contains(value, "#wechat_redirect") {
		value += "#wechat_redirect"
	}
	return value
}

func (s *Service) exchangeOAuth2Code(ctx context.Context, provider domainsettings.LoginProviderSettings, code string) (*oauth2.Token, error) {
	switch provider.Type {
	case "wecom":
		corporateToken, err := s.fetchWecomAccessToken(ctx, provider)
		if err != nil {
			return nil, err
		}
		userID, err := s.fetchWecomUserID(ctx, provider, corporateToken, code)
		if err != nil {
			return nil, err
		}
		token := &oauth2.Token{
			AccessToken: corporateToken,
			TokenType:   "Bearer",
			Expiry:      time.Now().UTC().Add(1 * time.Hour),
		}
		return token.WithExtra(map[string]any{
			"user_id": userID,
		}), nil
	case "feishu":
		token, err := s.exchangeFeishuCode(ctx, provider, code)
		if err == nil {
			return token, nil
		}
	}
	oauthConfig := oauth2ConfigFromProvider(provider)
	return oauthConfig.Exchange(ctx, code)
}

func (s *Service) exchangeFeishuCode(ctx context.Context, provider domainsettings.LoginProviderSettings, code string) (*oauth2.Token, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     provider.ClientID,
		"client_secret": provider.ClientSecret,
		"redirect_uri":  provider.RedirectURL,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var token accessTokenEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}
	accessToken := firstNonEmpty(token.AccessToken, token.Data.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("provider response missing access_token")
	}
	tokenValue := &oauth2.Token{
		AccessToken: accessToken,
		TokenType:   firstNonEmpty(token.TokenType, "Bearer"),
		Expiry:      time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second),
	}
	return tokenValue.WithExtra(map[string]any{
		"open_id":  firstNonEmpty(token.OpenID, token.Data.OpenID),
		"union_id": firstNonEmpty(token.UnionID, token.Data.UnionID),
	}), nil
}

func (s *Service) fetchWecomAccessToken(ctx context.Context, provider domainsettings.LoginProviderSettings) (string, error) {
	parsed, err := url.Parse(provider.TokenURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("corpid", provider.ClientID)
	query.Set("corpsecret", provider.ClientSecret)
	parsed.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var token accessTokenEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.ErrCode != 0 {
		return "", fmt.Errorf("provider returned errcode=%d errmsg=%s", token.ErrCode, token.ErrMsg)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return "", fmt.Errorf("provider response missing access_token")
	}
	return strings.TrimSpace(token.AccessToken), nil
}

func (s *Service) fetchWecomUserID(ctx context.Context, provider domainsettings.LoginProviderSettings, accessToken, code string) (string, error) {
	parsed, err := url.Parse(provider.UserInfoURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("access_token", accessToken)
	query.Set("code", code)
	parsed.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if errCode, ok := payload["errcode"].(float64); ok && int(errCode) != 0 {
		return "", fmt.Errorf("provider returned errcode=%d errmsg=%s", int(errCode), strings.TrimSpace(fmt.Sprint(payload["errmsg"])))
	}
	userID := firstNonEmpty(nestedString(payload, "UserId"), nestedString(payload, "OpenId"))
	if userID == "" {
		return "", fmt.Errorf("provider response missing UserId")
	}
	return userID, nil
}

func (s *Service) handleOAuth2Callback(ctx context.Context, provider domainsettings.LoginProviderSettings, state, code string) (string, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("%w: missing oauth2 callback parameters", apperrors.ErrInvalidArgument)
	}
	stateToken, err := s.users.ConsumeEphemeralToken(ctx, state, oauthStateKind)
	if err != nil {
		return "", fmt.Errorf("%w: oauth state missing or expired", apperrors.ErrUnauthorized)
	}
	if payloadProviderID, _ := stateToken.Payload["providerId"].(string); payloadProviderID != "" && payloadProviderID != provider.ID {
		return "", fmt.Errorf("%w: oauth provider mismatch", apperrors.ErrUnauthorized)
	}
	oauthToken, err := s.exchangeOAuth2Code(ctx, provider, code)
	if err != nil {
		return "", fmt.Errorf("exchange oauth2 code: %w", err)
	}
	profile, err := s.fetchOAuth2Profile(ctx, provider, oauthToken)
	if err != nil {
		return "", err
	}
	principal, err := s.reconcileExternalUser(ctx, provider, profile)
	if err != nil {
		return "", err
	}
	result, err := s.issueAuthResult(ctx, principal, provider.Type)
	if err != nil {
		return "", err
	}
	exchangeCode := uuid.NewString()
	payload, err := json.Marshal(oidcExchangePayload{Result: result})
	if err != nil {
		return "", fmt.Errorf("marshal oauth exchange payload: %w", err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return "", fmt.Errorf("decode oauth exchange payload: %w", err)
	}
	if err := s.users.CreateEphemeralToken(ctx, userrepo.EphemeralToken{
		Token:     exchangeCode,
		Kind:      oidcExchangeKind,
		Payload:   payloadMap,
		ExpiresAt: time.Now().UTC().Add(2 * time.Minute),
	}); err != nil {
		return "", fmt.Errorf("store oauth exchange payload: %w", err)
	}
	redirectURL, err := addQueryValue(provider.FrontendRedirectURL, "code", exchangeCode)
	if err != nil {
		return "", err
	}
	_ = s.recordAudit(ctx, principal, "login", "success", "oauth2 login succeeded", map[string]any{"provider": provider.ID, "providerType": provider.Type})
	return redirectURL, nil
}

func (s *Service) fetchOAuth2Profile(ctx context.Context, provider domainsettings.LoginProviderSettings, oauthToken *oauth2.Token) (genericProfile, error) {
	profileURL := firstNonEmpty(provider.UserInfoURL, provider.ProfileURL)
	if strings.TrimSpace(profileURL) == "" {
		return genericProfile{}, fmt.Errorf("%w: oauth2 user info url is required", apperrors.ErrInvalidArgument)
	}
	if provider.Type == "wecom" {
		return genericProfile{
			ID:       nestedString(map[string]any{"user_id": oauthToken.Extra("user_id")}, "user_id"),
			Name:     nestedString(map[string]any{"user_id": oauthToken.Extra("user_id")}, "user_id"),
			Email:    "",
			Raw:      map[string]any{"user_id": oauthToken.Extra("user_id")},
			Provider: provider.ID,
		}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return genericProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+oauthToken.AccessToken)
	if provider.Type == "feishu" {
		req.Header.Set("Authorization", "Bearer "+oauthToken.AccessToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return genericProfile{}, fmt.Errorf("request oauth2 user info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return genericProfile{}, fmt.Errorf("oauth2 user info returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return genericProfile{}, fmt.Errorf("decode oauth2 user info: %w", err)
	}
	if data, ok := raw["data"].(map[string]any); ok && provider.Type == "feishu" {
		raw = data
	}
	id := nestedString(raw, provider.UserIDField)
	if id == "" {
		if provider.Type == "feishu" {
			id = firstNonEmpty(
				nestedString(map[string]any{"open_id": oauthToken.Extra("open_id")}, "open_id"),
				nestedString(map[string]any{"union_id": oauthToken.Extra("union_id")}, "union_id"),
			)
		}
	}
	if id == "" {
		id = nestedString(raw, "sub")
	}
	name := nestedString(raw, provider.UserNameField)
	if name == "" {
		name = firstNonEmpty(nestedString(raw, "name"), nestedString(raw, "nick"), nestedString(raw, "preferred_username"))
	}
	email := nestedString(raw, provider.EmailField)
	if email == "" {
		email = firstNonEmpty(nestedString(raw, "email"), nestedString(raw, "enterprise_email"))
	}
	return genericProfile{
		ID:       id,
		Email:    email,
		Name:     name,
		Raw:      raw,
		Provider: provider.ID,
	}, nil
}

func nestedString(raw map[string]any, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	current := any(raw)
	for _, part := range strings.Split(field, ".") {
		record, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = record[part]
		if !ok {
			return ""
		}
	}
	value := strings.TrimSpace(fmt.Sprint(current))
	if value == "<nil>" {
		return ""
	}
	return value
}

func timePointerValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

const (
	oidcStateKind    = "oidc_state"
	oauthStateKind   = "oauth_state"
	oidcExchangeKind = "oidc_exchange"
)
