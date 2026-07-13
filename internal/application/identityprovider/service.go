package identityprovider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/textproto"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultOIDCAccessTokenTTLSeconds = 3600
	defaultOIDCIDTokenTTLSeconds     = 300
	authorizationCodeTTL             = 5 * time.Minute
	proxySessionTTL                  = 12 * time.Hour
	proxySessionIssuer               = "soha-proxy-provider"
	proxySessionAudience             = "soha-proxy"
	proxySessionTokenType            = "proxy_session"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type UserRepository interface {
	GetByID(context.Context, string) (domainidentity.User, error)
	GetAuthzState(context.Context, string) (domainidentity.AuthzState, error)
	ListRoles(context.Context, string) ([]string, error)
	ListTeams(context.Context, string) ([]string, error)
	ListProjects(context.Context, string) ([]string, error)
}

type Service struct {
	repo           domainprovider.Repository
	users          UserRepository
	permissions    *appaccess.PermissionResolver
	audit          AuditRecorder
	encryptionKey  string
	encryptionKeys keyring.Ring
}

func New(repo domainprovider.Repository, users UserRepository, permissions *appaccess.PermissionResolver, audit AuditRecorder, encryptionKey string) *Service {
	return &Service{
		repo:          repo,
		users:         users,
		permissions:   permissions,
		audit:         audit,
		encryptionKey: strings.TrimSpace(encryptionKey),
	}
}

func NewWithEncryptionKeys(repo domainprovider.Repository, users UserRepository, permissions *appaccess.PermissionResolver, audit AuditRecorder, encryptionKeys keyring.Ring) *Service {
	return &Service{
		repo:           repo,
		users:          users,
		permissions:    permissions,
		audit:          audit,
		encryptionKeys: encryptionKeys,
	}
}

func (s *Service) Discovery(issuer string) domainprovider.DiscoveryDocument {
	issuer = normalizeIssuer(issuer)
	return domainprovider.DiscoveryDocument{
		Issuer:                issuer,
		AuthorizationEndpoint: issuer + "/oauth2/authorize",
		TokenEndpoint:         issuer + "/oauth2/token",
		UserInfoEndpoint:      issuer + "/oauth2/userinfo",
		JWKSURI:               issuer + "/oauth2/jwks",
		RevocationEndpoint:    issuer + "/oauth2/revoke",
		IntrospectionEndpoint: issuer + "/oauth2/introspect",
		ResponseTypesSupported: []string{
			"code",
		},
		SubjectTypesSupported: []string{
			"public",
		},
		IDTokenSigningAlgValuesSupported: []string{
			"ES256",
		},
		ScopesSupported: []string{
			"openid",
			"profile",
			"email",
			"roles",
			"teams",
			"projects",
			"tags",
		},
		TokenEndpointAuthMethodsSupported: []string{
			"client_secret_basic",
			"client_secret_post",
		},
		ClaimsSupported: []string{
			"sub",
			"name",
			"email",
			"roles",
			"teams",
			"projects",
			"tags",
		},
		CodeChallengeMethodsSupported: []string{
			"S256",
		},
		GrantTypesSupported: []string{
			"authorization_code",
		},
	}
}

func (s *Service) JWKS(ctx context.Context) (domainprovider.JWKS, error) {
	keys, err := s.repo.ListActivePublicKeys(ctx)
	if err != nil {
		return domainprovider.JWKS{}, err
	}
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		if strings.EqualFold(key.Algorithm, "ES256") && key.PublicJWK != nil {
			out = append(out, cloneMap(key.PublicJWK))
		}
	}
	return domainprovider.JWKS{Keys: out}, nil
}

func (s *Service) OIDCLaunchURL(ctx context.Context, application domainportal.Application) (string, error) {
	provider, err := s.resolveOIDCLaunchProvider(ctx, application)
	if err != nil {
		return "", err
	}
	client, err := s.resolveOIDCLaunchClient(ctx, application, provider)
	if err != nil {
		return "", err
	}
	redirectURI := oidcLaunchRedirectURI(application, client)
	if redirectURI == "" {
		return "", fmt.Errorf("%w: oidc redirect_uri is not configured", apperrors.ErrInvalidArgument)
	}
	if !containsString(client.RedirectURIs, redirectURI) {
		return "", fmt.Errorf("%w: oidc redirect_uri is not registered", apperrors.ErrInvalidArgument)
	}
	scopes, err := oidcLaunchScopes(application, client)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", client.ClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", strings.Join(scopes, " "))
	return "/oauth2/authorize?" + values.Encode(), nil
}

func (s *Service) Authorize(ctx context.Context, issuer string, principal domainidentity.Principal, input domainprovider.AuthorizeInput) (domainprovider.AuthorizeResult, error) {
	if strings.TrimSpace(principal.UserID) == "" {
		return domainprovider.AuthorizeResult{}, fmt.Errorf("%w: authentication required", apperrors.ErrUnauthorized)
	}
	client, provider, application, err := s.resolveAuthorizeClient(ctx, input.ClientID)
	if err != nil {
		return domainprovider.AuthorizeResult{}, err
	}
	redirectURI := strings.TrimSpace(input.RedirectURI)
	if !containsString(client.RedirectURIs, redirectURI) {
		return domainprovider.AuthorizeResult{}, fmt.Errorf("%w: redirect_uri is not registered", apperrors.ErrInvalidArgument)
	}
	state := strings.TrimSpace(input.State)
	if strings.TrimSpace(input.ResponseType) != "code" {
		err := fmt.Errorf("%w: response_type must be code", apperrors.ErrInvalidArgument)
		return domainprovider.AuthorizeResult{}, authorizeRedirectError(redirectURI, state, "unsupported_response_type", err)
	}
	if err := validateProviderAccess(principal, provider, application); err != nil {
		s.recordAudit(ctx, principal, "oidc.authorize", "deny", provider, client, map[string]any{"reason": err.Error()})
		return domainprovider.AuthorizeResult{}, authorizeRedirectError(redirectURI, state, authorizeRedirectErrorCode(err), err)
	}
	scopes, err := normalizeRequestedScopes(input.Scope, client.AllowedScopes)
	if err != nil {
		return domainprovider.AuthorizeResult{}, authorizeRedirectError(redirectURI, state, "invalid_scope", err)
	}
	codeChallengeMethod := normalizeCodeChallengeMethod(input.CodeChallengeMethod)
	if client.RequirePKCE {
		if strings.TrimSpace(input.CodeChallenge) == "" {
			err := fmt.Errorf("%w: code_challenge is required", apperrors.ErrInvalidArgument)
			return domainprovider.AuthorizeResult{}, authorizeRedirectError(redirectURI, state, "invalid_request", err)
		}
		if codeChallengeMethod != "S256" {
			err := fmt.Errorf("%w: only S256 PKCE is supported", apperrors.ErrInvalidArgument)
			return domainprovider.AuthorizeResult{}, authorizeRedirectError(redirectURI, state, "invalid_request", err)
		}
	}
	rawCode, err := randomToken(32)
	if err != nil {
		return domainprovider.AuthorizeResult{}, err
	}
	now := time.Now().UTC()
	if err := s.repo.CreateAuthorizationCode(ctx, domainprovider.AuthorizationCode{
		ID:                  uuid.NewString(),
		ProviderID:          provider.ID,
		ClientID:            client.ClientID,
		UserID:              principal.UserID,
		CodeHash:            hashToken(rawCode),
		RedirectURI:         redirectURI,
		Scopes:              scopes,
		Nonce:               strings.TrimSpace(input.Nonce),
		CodeChallenge:       strings.TrimSpace(input.CodeChallenge),
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           now.Add(authorizationCodeTTL),
		CreatedAt:           now,
		Metadata: map[string]any{
			"issuer": normalizeIssuer(issuer),
			"state":  state,
		},
	}); err != nil {
		return domainprovider.AuthorizeResult{}, fmt.Errorf("create authorization code: %w", err)
	}
	s.recordAudit(ctx, principal, "oidc.authorize", "success", provider, client, map[string]any{
		"redirectUri": redirectURI,
		"scopes":      scopes,
	})
	return domainprovider.AuthorizeResult{
		RedirectURI: redirectURI,
		Code:        rawCode,
		State:       state,
	}, nil
}

func (s *Service) Token(ctx context.Context, issuer string, input domainprovider.TokenInput) (domainprovider.TokenResponse, error) {
	if strings.TrimSpace(input.GrantType) != "authorization_code" {
		return domainprovider.TokenResponse{}, fmt.Errorf("%w: grant_type must be authorization_code", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	codeHash := hashToken(input.Code)
	code, err := s.repo.GetAuthorizationCode(ctx, codeHash, now)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	client, err := s.repo.GetOIDCClientByClientID(ctx, code.ClientID)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	provider, err := s.repo.GetProvider(ctx, client.ProviderID)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	if provider.ID != code.ProviderID {
		return domainprovider.TokenResponse{}, fmt.Errorf("%w: authorization code provider mismatch", apperrors.ErrUnauthorized)
	}
	if !clientEnabled(client) || !providerEnabled(provider) {
		return domainprovider.TokenResponse{}, fmt.Errorf("%w: oidc client or provider is disabled", apperrors.ErrUnauthorized)
	}
	if !containsString(normalizeGrantTypes(client.AllowedGrantTypes), "authorization_code") {
		return domainprovider.TokenResponse{}, fmt.Errorf("%w: authorization_code grant is not allowed", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.ClientID) != "" && strings.TrimSpace(input.ClientID) != client.ClientID {
		return domainprovider.TokenResponse{}, fmt.Errorf("%w: client_id mismatch", apperrors.ErrUnauthorized)
	}
	if strings.TrimSpace(input.RedirectURI) != code.RedirectURI {
		return domainprovider.TokenResponse{}, fmt.Errorf("%w: redirect_uri does not match authorization request", apperrors.ErrUnauthorized)
	}
	if err := s.verifyClientSecret(client, input); err != nil {
		return domainprovider.TokenResponse{}, err
	}
	if err := verifyPKCE(code, input.CodeVerifier); err != nil {
		return domainprovider.TokenResponse{}, err
	}
	code, err = s.repo.ConsumeAuthorizationCode(ctx, codeHash, now)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	principal, err := s.loadPrincipal(ctx, code.UserID)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	signingKey, privateKey, err := s.ensureSigningKey(ctx, provider.ID)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	issuer = normalizeIssuer(issuer)
	accessTTL := ttlSeconds(client.AccessTokenTTLSeconds, defaultOIDCAccessTokenTTLSeconds)
	idTTL := ttlSeconds(client.IDTokenTTLSeconds, defaultOIDCIDTokenTTLSeconds)
	accessClaims := newOIDCTokenClaims(domainprovider.TokenTypeAccess, issuer, client.ClientID, principal, now, accessTTL)
	accessClaims.Scope = strings.Join(code.Scopes, " ")
	accessToken, err := signOIDCToken(privateKey, signingKey.KeyID, accessClaims)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	idClaims := newOIDCTokenClaims(domainprovider.TokenTypeID, issuer, client.ClientID, principal, now, idTTL)
	idClaims.Nonce = code.Nonce
	idToken, err := signOIDCToken(privateKey, signingKey.KeyID, idClaims)
	if err != nil {
		return domainprovider.TokenResponse{}, err
	}
	s.recordAudit(ctx, principal, "oidc.token", "success", provider, client, map[string]any{
		"scopes": code.Scopes,
	})
	return domainprovider.TokenResponse{
		AccessToken: accessToken,
		IDToken:     idToken,
		TokenType:   "Bearer",
		ExpiresIn:   int64(accessTTL),
		Scope:       strings.Join(code.Scopes, " "),
	}, nil
}

func newOIDCTokenClaims(tokenType, issuer, clientID string, principal domainidentity.Principal, now time.Time, ttl int) oidcTokenClaims {
	return oidcTokenClaims{
		TokenType: tokenType,
		ClientID:  clientID,
		UserName:  principal.UserName,
		Email:     principal.Email,
		Roles:     append([]string(nil), principal.Roles...),
		Teams:     append([]string(nil), principal.Teams...),
		Projects:  append([]string(nil), principal.Projects...),
		Tags:      append([]string(nil), principal.Tags...),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: principal.UserID, Audience: jwt.ClaimStrings{clientID}, ID: uuid.NewString(),
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(ttl) * time.Second)),
		},
	}
}

func (s *Service) Introspect(ctx context.Context, issuer, token string, auth domainprovider.ClientAuthInput) (domainprovider.IntrospectionResponse, error) {
	client, err := s.authenticateConfidentialClient(ctx, auth)
	if err != nil {
		return domainprovider.IntrospectionResponse{}, err
	}
	token = normalizeOIDCTokenValue(token)
	if token == "" {
		return domainprovider.IntrospectionResponse{Active: false}, nil
	}
	claims, err := s.parseOIDCAccessToken(ctx, issuer, token)
	if err != nil {
		return domainprovider.IntrospectionResponse{Active: false}, nil
	}
	if claims.ClientID != client.ClientID {
		return domainprovider.IntrospectionResponse{Active: false}, nil
	}
	out := domainprovider.IntrospectionResponse{
		Active:    true,
		Subject:   claims.Subject,
		ClientID:  claims.ClientID,
		Scope:     claims.Scope,
		TokenType: "Bearer",
		Issuer:    claims.Issuer,
		Audience:  append([]string(nil), claims.Audience...),
		JWTID:     claims.ID,
		Username:  claims.UserName,
	}
	if claims.ExpiresAt != nil {
		out.ExpiresAt = claims.ExpiresAt.Unix()
	}
	if claims.IssuedAt != nil {
		out.IssuedAt = claims.IssuedAt.Unix()
	}
	if claims.NotBefore != nil {
		out.NotBefore = claims.NotBefore.Unix()
	}
	return out, nil
}

func (s *Service) Revoke(ctx context.Context, issuer, token string, auth domainprovider.ClientAuthInput) error {
	client, err := s.authenticateConfidentialClient(ctx, auth)
	if err != nil {
		return err
	}
	token = normalizeOIDCTokenValue(token)
	if token == "" {
		return nil
	}
	claims, err := s.parseOIDCAccessToken(ctx, issuer, token)
	if err != nil || claims.ClientID != client.ClientID {
		return nil
	}
	// Access tokens are stateless in the first provider baseline. The endpoint is
	// still exposed for OAuth2 client compatibility and can attach persisted token
	// revocation later without changing the protocol surface.
	return nil
}

func (s *Service) UserInfo(ctx context.Context, issuer, bearerToken string) (domainprovider.UserInfoResponse, error) {
	claims, err := s.parseOIDCAccessToken(ctx, issuer, bearerToken)
	if err != nil {
		return domainprovider.UserInfoResponse{}, err
	}
	principal, err := s.loadPrincipal(ctx, claims.Subject)
	if err != nil {
		return domainprovider.UserInfoResponse{}, err
	}
	return domainprovider.UserInfoResponse{
		Subject:  principal.UserID,
		Name:     principal.UserName,
		Email:    principal.Email,
		Roles:    append([]string(nil), principal.Roles...),
		Teams:    append([]string(nil), principal.Teams...),
		Projects: append([]string(nil), principal.Projects...),
		Tags:     append([]string(nil), principal.Tags...),
	}, nil
}

func (s *Service) IssueProxySession(_ context.Context, principal domainidentity.Principal) (domainprovider.ProxySession, error) {
	if strings.TrimSpace(principal.UserID) == "" {
		return domainprovider.ProxySession{}, fmt.Errorf("%w: authentication required", apperrors.ErrUnauthorized)
	}
	now := time.Now().UTC()
	expiresAt := now.Add(proxySessionTTL)
	claims := proxySessionClaims{
		TokenType: proxySessionTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    proxySessionIssuer,
			Subject:   principal.UserID,
			Audience:  jwt.ClaimStrings{proxySessionAudience},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	value, err := token.SignedString(s.proxySessionSigningKey())
	if err != nil {
		return domainprovider.ProxySession{}, fmt.Errorf("sign proxy session: %w", err)
	}
	return domainprovider.ProxySession{
		Token:     value,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) ProxyAuth(ctx context.Context, principal domainidentity.Principal, input domainprovider.ProxyAuthInput) (domainprovider.ProxyAuthResult, error) {
	provider, application, err := s.resolveProxyProvider(ctx, input)
	if err != nil {
		return domainprovider.ProxyAuthResult{}, err
	}
	originalURL := proxyOriginalURL(input)
	requestHost := proxyRequestHost(input)
	result := domainprovider.ProxyAuthResult{
		OriginalURL:  originalURL,
		CookieDomain: proxyCookieDomain(provider, requestHost),
		Provider:     provider,
		Application:  application,
	}
	if proxyPathSkipped(provider, proxyPath(input)) {
		result.Decision = domainprovider.ProxyDecisionAllow
		result.Reason = "skip_auth_path"
		result.Skipped = true
		s.recordAudit(ctx, principal, "proxy.allow", "success", provider, domainprovider.OIDCClient{}, map[string]any{
			"applicationId": application.ID,
			"originalUrl":   originalURL,
			"reason":        result.Reason,
			"skipped":       true,
		})
		return result, nil
	}
	if strings.TrimSpace(principal.UserID) == "" {
		if parsedPrincipal, parseErr := s.parseProxySession(ctx, input.SessionToken); parseErr == nil {
			principal = parsedPrincipal
		}
	}
	if strings.TrimSpace(principal.UserID) == "" {
		result.Decision = domainprovider.ProxyDecisionLogin
		result.Reason = "authentication required"
		result.LoginURL = proxyLoginURL(originalURL)
		s.recordAudit(ctx, principal, "proxy.login", "denied", provider, domainprovider.OIDCClient{}, map[string]any{
			"applicationId": application.ID,
			"originalUrl":   originalURL,
			"reason":        result.Reason,
		})
		return result, nil
	}
	if err := validateProxyProviderAccess(principal, provider, application); err != nil {
		result.Decision = domainprovider.ProxyDecisionDeny
		result.Reason = err.Error()
		s.recordAudit(ctx, principal, "proxy.deny", "denied", provider, domainprovider.OIDCClient{}, map[string]any{
			"applicationId": application.ID,
			"originalUrl":   originalURL,
			"reason":        result.Reason,
		})
		return result, nil
	}
	result.Decision = domainprovider.ProxyDecisionAllow
	result.Reason = "proxy access allowed"
	result.Headers = proxyIdentityHeaders(principal, provider)
	s.recordAudit(ctx, principal, "proxy.allow", "success", provider, domainprovider.OIDCClient{}, map[string]any{
		"applicationId": application.ID,
		"originalUrl":   originalURL,
		"headers":       sortedMapKeys(result.Headers),
	})
	return result, nil
}

func (s *Service) ProxyCookieDomain(ctx context.Context, input domainprovider.ProxyAuthInput) (string, error) {
	provider, _, err := s.resolveProxyProvider(ctx, input)
	if err != nil {
		return "", err
	}
	return proxyCookieDomain(provider, proxyRequestHost(input)), nil
}

func (s *Service) ReverseProxy(ctx context.Context, principal domainidentity.Principal, input domainprovider.ReverseProxyInput) (domainprovider.ReverseProxyResult, error) {
	providerID := strings.TrimSpace(input.ProviderID)
	if providerID == "" {
		return domainprovider.ReverseProxyResult{}, fmt.Errorf("%w: provider_id is required", apperrors.ErrInvalidArgument)
	}
	provider, err := s.repo.GetProvider(ctx, providerID)
	if err != nil {
		return domainprovider.ReverseProxyResult{}, err
	}
	if provider.Type != domainprovider.ProviderTypeProxy {
		return domainprovider.ReverseProxyResult{}, fmt.Errorf("%w: provider is not a proxy provider", apperrors.ErrInvalidArgument)
	}
	if !providerEnabled(provider) {
		return domainprovider.ReverseProxyResult{}, fmt.Errorf("%w: proxy provider is disabled", apperrors.ErrAccessDenied)
	}
	if mode := strings.ToLower(configString(provider.Config, "mode")); mode != domainprovider.ProxyModeReverseProxy {
		return domainprovider.ReverseProxyResult{}, fmt.Errorf("%w: proxy provider is not in reverse_proxy mode", apperrors.ErrInvalidArgument)
	}
	upstreamURL, err := validateReverseProxyUpstream(configString(provider.Config, "upstreamUrl", "upstreamURL", "upstream_url"))
	if err != nil {
		return domainprovider.ReverseProxyResult{}, err
	}
	hosts := proxyHostCandidates(provider, domainportal.Application{})
	if len(hosts) == 0 {
		return domainprovider.ReverseProxyResult{}, fmt.Errorf("%w: reverse proxy external host is required", apperrors.ErrInvalidArgument)
	}
	auth, err := s.ProxyAuth(ctx, principal, domainprovider.ProxyAuthInput{
		ProviderID:     providerID,
		OriginalURL:    strings.TrimSpace(input.OriginalURL),
		ForwardedHost:  hosts[0],
		ForwardedProto: "https",
		ForwardedURI:   normalizeReverseProxyPath(input.Path),
		Method:         strings.TrimSpace(input.Method),
		SessionToken:   strings.TrimSpace(input.SessionToken),
	})
	if err != nil {
		return domainprovider.ReverseProxyResult{}, err
	}
	return domainprovider.ReverseProxyResult{
		Auth:             auth,
		UpstreamURL:      upstreamURL,
		WebsocketEnabled: configBoolean(provider.Config, "websocketEnabled", "websocket_enabled"),
	}, nil
}

func (s *Service) ListProviders(ctx context.Context, principal domainidentity.Principal, filter domainprovider.ProviderFilter) ([]domainprovider.Provider, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersView); err != nil {
		return nil, err
	}
	return s.repo.ListProviders(ctx, filter)
}

func (s *Service) GetProvider(ctx context.Context, principal domainidentity.Principal, providerID string) (domainprovider.Provider, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersView); err != nil {
		return domainprovider.Provider{}, err
	}
	return s.repo.GetProvider(ctx, providerID)
}

func (s *Service) CreateProvider(ctx context.Context, principal domainidentity.Principal, input domainprovider.ProviderInput) (domainprovider.Provider, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return domainprovider.Provider{}, err
	}
	now := time.Now().UTC()
	item, err := providerFromInput("", input, principal, now)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	if err := s.ensureProviderApplicationAvailable(ctx, item.ApplicationID, item.ID); err != nil {
		return domainprovider.Provider{}, err
	}
	created, err := s.repo.CreateProvider(ctx, item)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	s.recordAudit(ctx, principal, "identity.provider.create", "success", created, domainprovider.OIDCClient{}, nil)
	return created, nil
}

func (s *Service) UpdateProvider(ctx context.Context, principal domainidentity.Principal, providerID string, input domainprovider.ProviderInput) (domainprovider.Provider, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return domainprovider.Provider{}, err
	}
	current, err := s.repo.GetProvider(ctx, providerID)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	now := time.Now().UTC()
	item, err := providerFromInput(current.ID, input, principal, now)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	item.CreatedBy = current.CreatedBy
	item.CreatedAt = current.CreatedAt
	if err := s.ensureProviderApplicationAvailable(ctx, item.ApplicationID, item.ID); err != nil {
		return domainprovider.Provider{}, err
	}
	updated, err := s.repo.UpdateProvider(ctx, item)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	s.recordAudit(ctx, principal, "identity.provider.update", "success", updated, domainprovider.OIDCClient{}, nil)
	return updated, nil
}

func (s *Service) DeleteProvider(ctx context.Context, principal domainidentity.Principal, providerID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return err
	}
	item, err := s.repo.GetProvider(ctx, providerID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteProvider(ctx, item.ID); err != nil {
		return err
	}
	s.recordAudit(ctx, principal, "identity.provider.delete", "success", item, domainprovider.OIDCClient{}, nil)
	return nil
}

func (s *Service) ensureProviderApplicationAvailable(ctx context.Context, applicationID, currentProviderID string) error {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return fmt.Errorf("%w: application_id is required", apperrors.ErrInvalidArgument)
	}
	providers, err := s.repo.ListProviders(ctx, domainprovider.ProviderFilter{ApplicationID: applicationID})
	if err != nil {
		return err
	}
	for _, provider := range providers {
		if provider.ID != strings.TrimSpace(currentProviderID) {
			return fmt.Errorf("%w: application already has an identity provider", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func (s *Service) ListOutposts(ctx context.Context, principal domainidentity.Principal, filter domainprovider.OutpostFilter) ([]domainprovider.Outpost, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityOutpostsView); err != nil {
		return nil, err
	}
	return s.repo.ListOutposts(ctx, filter)
}

func (s *Service) GetOutpost(ctx context.Context, principal domainidentity.Principal, outpostID string) (domainprovider.Outpost, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityOutpostsView); err != nil {
		return domainprovider.Outpost{}, err
	}
	return s.repo.GetOutpost(ctx, outpostID)
}

func (s *Service) CreateOutpost(ctx context.Context, principal domainidentity.Principal, input domainprovider.OutpostInput) (domainprovider.Outpost, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityOutpostsManage); err != nil {
		return domainprovider.Outpost{}, err
	}
	now := time.Now().UTC()
	item, err := outpostFromInput("", input, principal, now)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	item.TokenHash = hashToken(token)
	created, err := s.repo.CreateOutpost(ctx, item)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	created.Token = token
	s.recordAudit(ctx, principal, "identity.outpost.create", "success", domainprovider.Provider{ID: created.ID, Type: "outpost"}, domainprovider.OIDCClient{}, map[string]any{
		"mode":   created.Mode,
		"status": created.Status,
	})
	return created, nil
}

func (s *Service) UpdateOutpost(ctx context.Context, principal domainidentity.Principal, outpostID string, input domainprovider.OutpostInput) (domainprovider.Outpost, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityOutpostsManage); err != nil {
		return domainprovider.Outpost{}, err
	}
	current, err := s.repo.GetOutpost(ctx, outpostID)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	now := time.Now().UTC()
	item, err := outpostFromInput(current.ID, input, principal, now)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	item.TokenHash = current.TokenHash
	item.LastSeenAt = current.LastSeenAt
	item.CreatedBy = current.CreatedBy
	item.CreatedAt = current.CreatedAt
	updated, err := s.repo.UpdateOutpost(ctx, item)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	s.recordAudit(ctx, principal, "identity.outpost.update", "success", domainprovider.Provider{ID: updated.ID, Type: "outpost"}, domainprovider.OIDCClient{}, map[string]any{
		"mode":   updated.Mode,
		"status": updated.Status,
	})
	return updated, nil
}

func (s *Service) DeleteOutpost(ctx context.Context, principal domainidentity.Principal, outpostID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityOutpostsManage); err != nil {
		return err
	}
	item, err := s.repo.GetOutpost(ctx, outpostID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteOutpost(ctx, item.ID); err != nil {
		return err
	}
	s.recordAudit(ctx, principal, "identity.outpost.delete", "success", domainprovider.Provider{ID: item.ID, Type: "outpost"}, domainprovider.OIDCClient{}, map[string]any{
		"mode": item.Mode,
	})
	return nil
}

func (s *Service) ClaimOutpost(ctx context.Context, input domainprovider.OutpostClaimInput) (domainprovider.OutpostClaimResult, error) {
	outpost, err := s.authenticateOutpost(ctx, input.OutpostID, input.Token)
	if err != nil {
		return domainprovider.OutpostClaimResult{}, err
	}
	now := time.Now().UTC()
	outpost.Status = domainprovider.OutpostStatusOnline
	outpost.LastSeenAt = &now
	outpost.UpdatedAt = now
	if version := strings.TrimSpace(input.Version); version != "" {
		outpost.Version = version
	}
	if input.Metadata != nil {
		outpost.Metadata = input.Metadata
	}
	updated, err := s.repo.UpdateOutpost(ctx, outpost)
	if err != nil {
		return domainprovider.OutpostClaimResult{}, err
	}
	providers, err := s.outpostProxyProviders(ctx, updated.ID)
	if err != nil {
		return domainprovider.OutpostClaimResult{}, err
	}
	s.recordAudit(ctx, domainidentity.Principal{}, "outpost.claim", "success", domainprovider.Provider{ID: updated.ID, Type: "outpost"}, domainprovider.OIDCClient{}, map[string]any{
		"mode":          updated.Mode,
		"version":       updated.Version,
		"providerCount": len(providers),
	})
	return domainprovider.OutpostClaimResult{
		Outpost:   updated,
		Providers: providers,
	}, nil
}

func (s *Service) HeartbeatOutpost(ctx context.Context, outpostID string, input domainprovider.OutpostHeartbeatInput) (domainprovider.OutpostHeartbeatResult, error) {
	outpost, err := s.authenticateOutpost(ctx, outpostID, input.Token)
	if err != nil {
		return domainprovider.OutpostHeartbeatResult{}, err
	}
	now := time.Now().UTC()
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = domainprovider.OutpostStatusOnline
	}
	if status != domainprovider.OutpostStatusOnline && status != domainprovider.OutpostStatusOffline && status != domainprovider.OutpostStatusDegraded {
		return domainprovider.OutpostHeartbeatResult{}, fmt.Errorf("%w: unsupported outpost status", apperrors.ErrInvalidArgument)
	}
	outpost.Status = status
	outpost.LastSeenAt = &now
	outpost.UpdatedAt = now
	if version := strings.TrimSpace(input.Version); version != "" {
		outpost.Version = version
	}
	if input.Metadata != nil {
		outpost.Metadata = input.Metadata
	}
	updated, err := s.repo.UpdateOutpost(ctx, outpost)
	if err != nil {
		return domainprovider.OutpostHeartbeatResult{}, err
	}
	s.recordAudit(ctx, domainidentity.Principal{}, "outpost.heartbeat", "success", domainprovider.Provider{ID: updated.ID, Type: "outpost"}, domainprovider.OIDCClient{}, map[string]any{
		"mode":    updated.Mode,
		"status":  updated.Status,
		"version": updated.Version,
	})
	return domainprovider.OutpostHeartbeatResult{Outpost: updated}, nil
}

func (s *Service) CheckOutpost(ctx context.Context, outpostID string, input domainprovider.OutpostCheckInput) (domainprovider.ProxyAuthResult, error) {
	outpost, err := s.authenticateOutpost(ctx, outpostID, input.Token)
	if err != nil {
		return domainprovider.ProxyAuthResult{}, err
	}
	proxyInput := proxyAuthInputFromOutpostCheck(input)
	provider, _, err := s.resolveProxyProvider(ctx, proxyInput)
	if err != nil {
		return domainprovider.ProxyAuthResult{}, err
	}
	if !providerAssignedToOutpost(provider, outpost.ID) {
		return domainprovider.ProxyAuthResult{}, fmt.Errorf("%w: proxy provider is not assigned to outpost", apperrors.ErrAccessDenied)
	}
	proxyInput.ProviderID = provider.ID
	return s.ProxyAuth(ctx, domainidentity.Principal{}, proxyInput)
}

func (s *Service) RecordOutpostEvents(ctx context.Context, outpostID string, input domainprovider.OutpostEventsInput) (domainprovider.OutpostEventsResult, error) {
	outpost, err := s.authenticateOutpost(ctx, outpostID, input.Token)
	if err != nil {
		return domainprovider.OutpostEventsResult{}, err
	}
	assigned, err := s.outpostAssignedProviderIDs(ctx, outpost.ID)
	if err != nil {
		return domainprovider.OutpostEventsResult{}, err
	}
	accepted := 0
	for _, event := range input.Events {
		providerID := strings.TrimSpace(event.ProviderID)
		if providerID != "" {
			if _, ok := assigned[providerID]; !ok {
				return domainprovider.OutpostEventsResult{}, fmt.Errorf("%w: proxy provider is not assigned to outpost", apperrors.ErrAccessDenied)
			}
		}
		s.recordOutpostEvent(ctx, outpost, event)
		accepted++
	}
	return domainprovider.OutpostEventsResult{Accepted: accepted}, nil
}

func (s *Service) ListOIDCClients(ctx context.Context, principal domainidentity.Principal, providerID string) ([]domainprovider.OIDCClient, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersView); err != nil {
		return nil, err
	}
	if _, err := s.requireOIDCProvider(ctx, providerID); err != nil {
		return nil, err
	}
	return s.repo.ListOIDCClients(ctx, providerID)
}

func (s *Service) CreateOIDCClient(ctx context.Context, principal domainidentity.Principal, providerID string, input domainprovider.OIDCClientInput) (domainprovider.OIDCClientCreated, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return domainprovider.OIDCClientCreated{}, err
	}
	input.ProviderID = providerID
	provider, err := s.requireOIDCProvider(ctx, input.ProviderID)
	if err != nil {
		return domainprovider.OIDCClientCreated{}, err
	}
	secret := strings.TrimSpace(input.ClientSecret)
	if secret == "" {
		secret, err = randomToken(32)
		if err != nil {
			return domainprovider.OIDCClientCreated{}, err
		}
	}
	item, err := oidcClientFromInput("", input, secret)
	if err != nil {
		return domainprovider.OIDCClientCreated{}, err
	}
	created, err := s.repo.CreateOIDCClient(ctx, item)
	if err != nil {
		return domainprovider.OIDCClientCreated{}, err
	}
	s.recordAudit(ctx, principal, "identity.oidc_client.create", "success", provider, created, nil)
	return domainprovider.OIDCClientCreated{Client: created, ClientSecret: secret}, nil
}

func (s *Service) UpdateOIDCClient(ctx context.Context, principal domainidentity.Principal, clientID string, input domainprovider.OIDCClientInput) (domainprovider.OIDCClient, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return domainprovider.OIDCClient{}, err
	}
	current, err := s.repo.GetOIDCClient(ctx, clientID)
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	if strings.TrimSpace(input.ProviderID) == "" {
		input.ProviderID = current.ProviderID
	}
	provider, err := s.requireOIDCProvider(ctx, input.ProviderID)
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	item, err := oidcClientFromInput(current.ID, input, strings.TrimSpace(input.ClientSecret))
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	item.CreatedAt = current.CreatedAt
	if strings.TrimSpace(input.ClientSecret) == "" {
		item.ClientSecretHash = current.ClientSecretHash
	}
	updated, err := s.repo.UpdateOIDCClient(ctx, item)
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	s.recordAudit(ctx, principal, "identity.oidc_client.update", "success", provider, updated, nil)
	return updated, nil
}

func (s *Service) DeleteOIDCClient(ctx context.Context, principal domainidentity.Principal, clientID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return err
	}
	item, err := s.repo.GetOIDCClient(ctx, clientID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteOIDCClient(ctx, item.ID); err != nil {
		return err
	}
	s.recordAudit(ctx, principal, "identity.oidc_client.delete", "success", domainprovider.Provider{ID: item.ProviderID, Type: domainprovider.ProviderTypeOIDC}, item, nil)
	return nil
}

type oidcTokenClaims struct {
	TokenType string   `json:"token_type"`
	ClientID  string   `json:"client_id"`
	Scope     string   `json:"scope,omitempty"`
	Nonce     string   `json:"nonce,omitempty"`
	UserName  string   `json:"name,omitempty"`
	Email     string   `json:"email,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	Teams     []string `json:"teams,omitempty"`
	Projects  []string `json:"projects,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	jwt.RegisteredClaims
}

type proxySessionClaims struct {
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

func (s *Service) resolveOIDCLaunchProvider(ctx context.Context, application domainportal.Application) (domainprovider.Provider, error) {
	providerID := strings.TrimSpace(application.ProviderID)
	if providerID != "" {
		provider, err := s.repo.GetProvider(ctx, providerID)
		if err != nil {
			return domainprovider.Provider{}, err
		}
		if provider.ApplicationID != application.ID {
			return domainprovider.Provider{}, fmt.Errorf("%w: oidc provider is not bound to application", apperrors.ErrAccessDenied)
		}
		if !providerEnabled(provider) || provider.Type != domainprovider.ProviderTypeOIDC {
			return domainprovider.Provider{}, fmt.Errorf("%w: oidc provider is disabled", apperrors.ErrUnauthorized)
		}
		return provider, nil
	}
	providers, err := s.repo.ListProviders(ctx, domainprovider.ProviderFilter{
		Type:   domainprovider.ProviderTypeOIDC,
		Status: domainprovider.ProviderStatusEnabled,
	})
	if err != nil {
		return domainprovider.Provider{}, err
	}
	for _, provider := range providers {
		if provider.ApplicationID == application.ID && providerEnabled(provider) && provider.Type == domainprovider.ProviderTypeOIDC {
			return provider, nil
		}
	}
	return domainprovider.Provider{}, fmt.Errorf("%w: oidc provider not found", apperrors.ErrNotFound)
}

func (s *Service) resolveOIDCLaunchClient(ctx context.Context, application domainportal.Application, provider domainprovider.Provider) (domainprovider.OIDCClient, error) {
	clients, err := s.repo.ListOIDCClients(ctx, provider.ID)
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	preferredClientID := applicationMetadataString(application.Metadata, "oidcClientId", "clientId")
	for _, client := range clients {
		if !clientEnabled(client) {
			continue
		}
		if preferredClientID != "" && client.ClientID != preferredClientID {
			continue
		}
		if !containsString(normalizeGrantTypes(client.AllowedGrantTypes), "authorization_code") {
			if preferredClientID != "" {
				return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc client does not allow authorization_code", apperrors.ErrInvalidArgument)
			}
			continue
		}
		if client.RequirePKCE {
			if preferredClientID != "" {
				return domainprovider.OIDCClient{}, fmt.Errorf("%w: portal oidc launch cannot use a PKCE-required client", apperrors.ErrInvalidArgument)
			}
			continue
		}
		return client, nil
	}
	if preferredClientID != "" {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc client is not available", apperrors.ErrNotFound)
	}
	return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc portal launch requires an enabled non-PKCE authorization_code client", apperrors.ErrNotFound)
}

func (s *Service) resolveAuthorizeClient(ctx context.Context, clientID string) (domainprovider.OIDCClient, domainprovider.Provider, domainportal.Application, error) {
	client, err := s.repo.GetOIDCClientByClientID(ctx, clientID)
	if err != nil {
		return domainprovider.OIDCClient{}, domainprovider.Provider{}, domainportal.Application{}, err
	}
	if !clientEnabled(client) {
		return domainprovider.OIDCClient{}, domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: oidc client is disabled", apperrors.ErrUnauthorized)
	}
	if !containsString(normalizeGrantTypes(client.AllowedGrantTypes), "authorization_code") {
		return domainprovider.OIDCClient{}, domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: authorization_code grant is not allowed", apperrors.ErrInvalidArgument)
	}
	provider, err := s.repo.GetProvider(ctx, client.ProviderID)
	if err != nil {
		return domainprovider.OIDCClient{}, domainprovider.Provider{}, domainportal.Application{}, err
	}
	application, err := s.repo.GetProviderApplication(ctx, provider.ID)
	if err != nil {
		return domainprovider.OIDCClient{}, domainprovider.Provider{}, domainportal.Application{}, err
	}
	return client, provider, application, nil
}

func (s *Service) resolveProxyProvider(ctx context.Context, input domainprovider.ProxyAuthInput) (domainprovider.Provider, domainportal.Application, error) {
	providerID := strings.TrimSpace(input.ProviderID)
	if providerID != "" {
		provider, err := s.repo.GetProvider(ctx, providerID)
		if err != nil {
			return domainprovider.Provider{}, domainportal.Application{}, err
		}
		application, err := s.repo.GetProviderApplication(ctx, provider.ID)
		if err != nil {
			return domainprovider.Provider{}, domainportal.Application{}, err
		}
		if provider.Type != domainprovider.ProviderTypeProxy {
			return domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: provider is not a proxy provider", apperrors.ErrInvalidArgument)
		}
		host := proxyRequestHost(input)
		if host == "" {
			return domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: proxy host is required", apperrors.ErrInvalidArgument)
		}
		if !proxyHostMatches(provider, application, host) {
			return domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: proxy host does not match provider", apperrors.ErrAccessDenied)
		}
		pathPrefix := proxyPathPrefix(provider)
		if !pathHasPrefix(proxyPath(input), pathPrefix) {
			return domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: proxy path does not match provider", apperrors.ErrAccessDenied)
		}
		return provider, application, nil
	}
	host := proxyRequestHost(input)
	path := proxyPath(input)
	if host == "" {
		return domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: proxy host is required", apperrors.ErrInvalidArgument)
	}
	providers, err := s.repo.ListProviders(ctx, domainprovider.ProviderFilter{
		Type:   domainprovider.ProviderTypeProxy,
		Status: domainprovider.ProviderStatusEnabled,
	})
	if err != nil {
		return domainprovider.Provider{}, domainportal.Application{}, err
	}
	var bestProvider domainprovider.Provider
	var bestApplication domainportal.Application
	bestScore := -1
	for _, provider := range providers {
		if !providerEnabled(provider) || provider.Type != domainprovider.ProviderTypeProxy {
			continue
		}
		application, err := s.repo.GetProviderApplication(ctx, provider.ID)
		if err != nil {
			continue
		}
		if !proxyHostMatches(provider, application, host) {
			continue
		}
		pathPrefix := proxyPathPrefix(provider)
		if !pathHasPrefix(path, pathPrefix) {
			continue
		}
		score := len(pathPrefix)
		if score > bestScore {
			bestProvider = provider
			bestApplication = application
			bestScore = score
		}
	}
	if bestScore < 0 {
		return domainprovider.Provider{}, domainportal.Application{}, fmt.Errorf("%w: proxy provider not found", apperrors.ErrNotFound)
	}
	return bestProvider, bestApplication, nil
}

func validateProviderAccess(principal domainidentity.Principal, provider domainprovider.Provider, application domainportal.Application) error {
	if !providerEnabled(provider) || provider.Type != domainprovider.ProviderTypeOIDC {
		return fmt.Errorf("%w: oidc provider is disabled", apperrors.ErrUnauthorized)
	}
	if application.Status != domainportal.ApplicationStatusEnabled {
		return fmt.Errorf("%w: application is disabled", apperrors.ErrAccessDenied)
	}
	if !domainportal.CanAccessApplication(principal, application) {
		return fmt.Errorf("%w: application access denied", apperrors.ErrAccessDenied)
	}
	return nil
}

func authorizeRedirectError(redirectURI, state, code string, err error) error {
	if strings.TrimSpace(code) == "" {
		code = authorizeRedirectErrorCode(err)
	}
	description := ""
	if err != nil {
		description = err.Error()
	}
	return &domainprovider.AuthorizeRedirectError{
		RedirectURI: strings.TrimSpace(redirectURI),
		State:       strings.TrimSpace(state),
		Code:        strings.TrimSpace(code),
		Description: description,
		Err:         err,
	}
}

func authorizeRedirectErrorCode(err error) string {
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied), errors.Is(err, apperrors.ErrUnauthorized):
		return "access_denied"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		return "invalid_request"
	default:
		return "server_error"
	}
}

func validateProxyProviderAccess(principal domainidentity.Principal, provider domainprovider.Provider, application domainportal.Application) error {
	if !providerEnabled(provider) || provider.Type != domainprovider.ProviderTypeProxy {
		return fmt.Errorf("%w: proxy provider is disabled", apperrors.ErrUnauthorized)
	}
	if application.Status != domainportal.ApplicationStatusEnabled {
		return fmt.Errorf("%w: application is disabled", apperrors.ErrAccessDenied)
	}
	if !domainportal.CanAccessApplication(principal, application) {
		return fmt.Errorf("%w: application access denied", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) verifyClientSecret(client domainprovider.OIDCClient, input domainprovider.TokenInput) error {
	if strings.TrimSpace(client.ClientSecretHash) == "" {
		return nil
	}
	secret := strings.TrimSpace(input.ClientSecret)
	if secret == "" {
		return fmt.Errorf("%w: client authentication is required", apperrors.ErrUnauthorized)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(client.ClientSecretHash), []byte(secret)); err != nil {
		return fmt.Errorf("%w: invalid client credentials", apperrors.ErrUnauthorized)
	}
	return nil
}

func (s *Service) authenticateConfidentialClient(ctx context.Context, input domainprovider.ClientAuthInput) (domainprovider.OIDCClient, error) {
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: client authentication is required", apperrors.ErrUnauthorized)
	}
	client, err := s.repo.GetOIDCClientByClientID(ctx, clientID)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: invalid client credentials", apperrors.ErrUnauthorized)
	}
	if !clientEnabled(client) {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc client is disabled", apperrors.ErrUnauthorized)
	}
	if strings.TrimSpace(client.ClientSecretHash) == "" {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: confidential client authentication is required", apperrors.ErrUnauthorized)
	}
	secret := strings.TrimSpace(input.ClientSecret)
	if secret == "" {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: client authentication is required", apperrors.ErrUnauthorized)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(client.ClientSecretHash), []byte(secret)); err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: invalid client credentials", apperrors.ErrUnauthorized)
	}
	return client, nil
}

func (s *Service) requireOIDCProvider(ctx context.Context, providerID string) (domainprovider.Provider, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return domainprovider.Provider{}, fmt.Errorf("%w: provider_id is required", apperrors.ErrInvalidArgument)
	}
	provider, err := s.repo.GetProvider(ctx, providerID)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	if provider.Type != domainprovider.ProviderTypeOIDC {
		return domainprovider.Provider{}, fmt.Errorf("%w: oidc clients require an oidc provider", apperrors.ErrInvalidArgument)
	}
	return provider, nil
}

func verifyPKCE(code domainprovider.AuthorizationCode, verifier string) error {
	challenge := strings.TrimSpace(code.CodeChallenge)
	if challenge == "" {
		return nil
	}
	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		return fmt.Errorf("%w: code_verifier is required", apperrors.ErrUnauthorized)
	}
	switch normalizeCodeChallengeMethod(code.CodeChallengeMethod) {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		expected := base64.RawURLEncoding.EncodeToString(sum[:])
		if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) != 1 {
			return fmt.Errorf("%w: invalid code_verifier", apperrors.ErrUnauthorized)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported code_challenge_method", apperrors.ErrUnauthorized)
	}
}

func (s *Service) loadPrincipal(ctx context.Context, userID string) (domainidentity.Principal, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("%w: user not found", apperrors.ErrUnauthorized)
	}
	state, err := s.users.GetAuthzState(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, fmt.Errorf("%w: user not found", apperrors.ErrUnauthorized)
	}
	if strings.TrimSpace(state.Status) != "active" || strings.TrimSpace(user.Status) != "active" {
		return domainidentity.Principal{}, fmt.Errorf("%w: account is not active", apperrors.ErrUnauthorized)
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

func (s *Service) authenticateOutpost(ctx context.Context, outpostID, token string) (domainprovider.Outpost, error) {
	outpostID = strings.TrimSpace(outpostID)
	if outpostID == "" {
		return domainprovider.Outpost{}, fmt.Errorf("%w: outpost_id is required", apperrors.ErrInvalidArgument)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return domainprovider.Outpost{}, fmt.Errorf("%w: outpost token is required", apperrors.ErrUnauthorized)
	}
	outpost, err := s.repo.GetOutpost(ctx, outpostID)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	if strings.TrimSpace(outpost.TokenHash) == "" {
		return domainprovider.Outpost{}, fmt.Errorf("%w: outpost token is not configured", apperrors.ErrUnauthorized)
	}
	if subtle.ConstantTimeCompare([]byte(outpost.TokenHash), []byte(hashToken(token))) != 1 {
		return domainprovider.Outpost{}, fmt.Errorf("%w: invalid outpost token", apperrors.ErrUnauthorized)
	}
	return outpost, nil
}

func (s *Service) outpostProxyProviders(ctx context.Context, outpostID string) ([]domainprovider.Provider, error) {
	providers, err := s.repo.ListProviders(ctx, domainprovider.ProviderFilter{
		Type:   domainprovider.ProviderTypeProxy,
		Status: domainprovider.ProviderStatusEnabled,
	})
	if err != nil {
		return nil, err
	}
	out := make([]domainprovider.Provider, 0)
	for _, provider := range providers {
		if !providerEnabled(provider) || provider.Type != domainprovider.ProviderTypeProxy {
			continue
		}
		if configString(provider.Config, "outpostId", "outpost_id") != outpostID {
			continue
		}
		out = append(out, provider)
	}
	return out, nil
}

func (s *Service) outpostAssignedProviderIDs(ctx context.Context, outpostID string) (map[string]struct{}, error) {
	providers, err := s.outpostProxyProviders(ctx, outpostID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		out[provider.ID] = struct{}{}
	}
	return out, nil
}

func providerAssignedToOutpost(provider domainprovider.Provider, outpostID string) bool {
	return configString(provider.Config, "outpostId", "outpost_id") == strings.TrimSpace(outpostID)
}

func proxyAuthInputFromOutpostCheck(input domainprovider.OutpostCheckInput) domainprovider.ProxyAuthInput {
	return domainprovider.ProxyAuthInput{
		ProviderID:     strings.TrimSpace(input.ProviderID),
		OriginalURL:    strings.TrimSpace(input.OriginalURL),
		ForwardedHost:  strings.TrimSpace(input.ForwardedHost),
		ForwardedProto: strings.TrimSpace(input.ForwardedProto),
		ForwardedURI:   strings.TrimSpace(input.ForwardedURI),
		RequestHost:    strings.TrimSpace(input.RequestHost),
		RequestPath:    strings.TrimSpace(input.RequestPath),
		Method:         strings.TrimSpace(input.Method),
		SessionToken:   strings.TrimSpace(input.SessionToken),
	}
}

func (s *Service) ensureSigningKey(ctx context.Context, providerID string) (domainprovider.SigningKey, *ecdsa.PrivateKey, error) {
	key, err := s.repo.GetActiveSigningKey(ctx, providerID)
	if err == nil {
		privateKey, parseErr := s.decryptPrivateKey(key.EncryptedPrivateKey)
		if parseErr != nil {
			return domainprovider.SigningKey{}, nil, parseErr
		}
		return key, privateKey, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return domainprovider.SigningKey{}, nil, err
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return domainprovider.SigningKey{}, nil, fmt.Errorf("generate oidc signing key: %w", err)
	}
	kid, err := randomToken(16)
	if err != nil {
		return domainprovider.SigningKey{}, nil, err
	}
	encryptedPrivateKey, err := s.encryptPrivateKey(privateKey)
	if err != nil {
		return domainprovider.SigningKey{}, nil, err
	}
	now := time.Now().UTC()
	created, err := s.repo.CreateSigningKey(ctx, domainprovider.SigningKey{
		ID:                  uuid.NewString(),
		ProviderID:          providerID,
		KeyID:               kid,
		Algorithm:           "ES256",
		EncryptedPrivateKey: encryptedPrivateKey,
		PublicJWK:           publicJWK(privateKey.PublicKey, kid),
		Active:              true,
		CreatedAt:           now,
	})
	if err != nil {
		return domainprovider.SigningKey{}, nil, err
	}
	return created, privateKey, nil
}

func (s *Service) encryptPrivateKey(privateKey *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", fmt.Errorf("marshal oidc signing key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	var encrypted string
	if s.encryptionKeys.Active().ID() != "" {
		encrypted, err = secretcrypto.EncryptStringWithKeyring(s.encryptionKeys, string(pemBytes))
	} else {
		encrypted, err = secretcrypto.EncryptString(s.encryptionKey, string(pemBytes))
	}
	if err != nil {
		return "", fmt.Errorf("encrypt oidc signing key: %w", err)
	}
	return encrypted, nil
}

func (s *Service) decryptPrivateKey(ciphertext string) (*ecdsa.PrivateKey, error) {
	var plaintext string
	var err error
	if s.encryptionKeys.Active().ID() != "" {
		plaintext, err = secretcrypto.DecryptStringWithKeyring(s.encryptionKeys, ciphertext)
	} else {
		plaintext, err = secretcrypto.DecryptString(s.encryptionKey, ciphertext)
	}
	if err != nil {
		return nil, fmt.Errorf("decrypt oidc signing key: %w", err)
	}
	block, _ := pem.Decode([]byte(plaintext))
	if block == nil {
		return nil, fmt.Errorf("%w: oidc signing key payload is invalid", apperrors.ErrUnauthorized)
	}
	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse oidc signing key: %w", err)
	}
	return privateKey, nil
}

func (s *Service) parseOIDCAccessToken(ctx context.Context, issuer, tokenString string) (*oidcTokenClaims, error) {
	tokenString = normalizeOIDCTokenValue(tokenString)
	if tokenString == "" {
		return nil, fmt.Errorf("%w: bearer token is required", apperrors.ErrUnauthorized)
	}
	keys, err := s.repo.ListActivePublicKeys(ctx)
	if err != nil {
		return nil, err
	}
	publicKeys := map[string]*ecdsa.PublicKey{}
	for _, key := range keys {
		publicKey, err := publicKeyFromJWK(key.PublicJWK)
		if err != nil {
			continue
		}
		publicKeys[key.KeyID] = publicKey
	}
	claims := &oidcTokenClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodES256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		kid, _ := token.Header["kid"].(string)
		publicKey, ok := publicKeys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown signing key")
		}
		return publicKey, nil
	}, jwt.WithIssuer(normalizeIssuer(issuer)))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: invalid token", apperrors.ErrUnauthorized)
	}
	if claims.TokenType != domainprovider.TokenTypeAccess {
		return nil, fmt.Errorf("%w: unexpected token type", apperrors.ErrUnauthorized)
	}
	return claims, nil
}

func (s *Service) parseProxySession(ctx context.Context, tokenString string) (domainidentity.Principal, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return domainidentity.Principal{}, fmt.Errorf("%w: proxy session is required", apperrors.ErrUnauthorized)
	}
	claims := &proxySessionClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return s.proxySessionSigningKey(), nil
	}, jwt.WithIssuer(proxySessionIssuer), jwt.WithAudience(proxySessionAudience))
	if err != nil || !token.Valid {
		return domainidentity.Principal{}, fmt.Errorf("%w: invalid proxy session", apperrors.ErrUnauthorized)
	}
	if claims.TokenType != proxySessionTokenType || strings.TrimSpace(claims.Subject) == "" {
		return domainidentity.Principal{}, fmt.Errorf("%w: invalid proxy session", apperrors.ErrUnauthorized)
	}
	return s.loadPrincipal(ctx, claims.Subject)
}

func (s *Service) proxySessionSigningKey() []byte {
	return []byte("soha-proxy-session:" + strings.TrimSpace(s.encryptionKey))
}

func normalizeOIDCTokenValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return value
}

func signOIDCToken(privateKey *ecdsa.PrivateKey, kid string, claims oidcTokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid
	value, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("sign oidc token: %w", err)
	}
	return value, nil
}

func publicJWK(publicKey ecdsa.PublicKey, kid string) map[string]any {
	return map[string]any{
		"kty": "EC",
		"use": "sig",
		"kid": kid,
		"alg": "ES256",
		"crv": "P-256",
		"x":   base64urlUInt(publicKey.X),
		"y":   base64urlUInt(publicKey.Y),
	}
}

func publicKeyFromJWK(jwk map[string]any) (*ecdsa.PublicKey, error) {
	xValue, _ := jwk["x"].(string)
	yValue, _ := jwk["y"].(string)
	xBytes, err := base64.RawURLEncoding.DecodeString(xValue)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yValue)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}, nil
}

func providerFromInput(providerID string, input domainprovider.ProviderInput, principal domainidentity.Principal, now time.Time) (domainprovider.Provider, error) {
	applicationID := strings.TrimSpace(input.ApplicationID)
	if applicationID == "" {
		return domainprovider.Provider{}, fmt.Errorf("%w: application_id is required", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainprovider.Provider{}, fmt.Errorf("%w: provider name is required", apperrors.ErrInvalidArgument)
	}
	providerType := strings.ToLower(strings.TrimSpace(input.Type))
	if providerType == "" {
		providerType = domainprovider.ProviderTypeOIDC
	}
	if providerType != domainprovider.ProviderTypeOIDC && providerType != domainprovider.ProviderTypeProxy {
		return domainprovider.Provider{}, fmt.Errorf("%w: unsupported provider type", apperrors.ErrInvalidArgument)
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		if input.Enabled {
			status = domainprovider.ProviderStatusEnabled
		} else {
			status = domainprovider.ProviderStatusDisabled
		}
	}
	if status != domainprovider.ProviderStatusEnabled && status != domainprovider.ProviderStatusDisabled {
		return domainprovider.Provider{}, fmt.Errorf("%w: unsupported provider status", apperrors.ErrInvalidArgument)
	}
	if providerID == "" {
		providerID = uuid.NewString()
	}
	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	if providerType == domainprovider.ProviderTypeProxy {
		mode := strings.ToLower(configString(config, "mode"))
		switch mode {
		case "", domainprovider.ProxyModeForwardAuth:
		case domainprovider.ProxyModeReverseProxy:
			if _, err := validateReverseProxyUpstream(configString(config, "upstreamUrl", "upstreamURL", "upstream_url")); err != nil {
				return domainprovider.Provider{}, err
			}
		default:
			return domainprovider.Provider{}, fmt.Errorf("%w: unsupported proxy mode", apperrors.ErrInvalidArgument)
		}
	}
	secretRefs := input.SecretRefs
	if secretRefs == nil {
		secretRefs = map[string]any{}
	}
	return domainprovider.Provider{
		ID:            providerID,
		ApplicationID: applicationID,
		Name:          name,
		Type:          providerType,
		Enabled:       input.Enabled || status == domainprovider.ProviderStatusEnabled,
		Config:        config,
		SecretRefs:    secretRefs,
		Status:        status,
		CreatedBy:     actorID(principal),
		UpdatedBy:     actorID(principal),
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func outpostFromInput(outpostID string, input domainprovider.OutpostInput, principal domainidentity.Principal, now time.Time) (domainprovider.Outpost, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainprovider.Outpost{}, fmt.Errorf("%w: outpost name is required", apperrors.ErrInvalidArgument)
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = domainprovider.OutpostModeEmbedded
	}
	switch mode {
	case domainprovider.OutpostModeEmbedded, domainprovider.OutpostModeAgent, domainprovider.OutpostModeKubernetes, domainprovider.OutpostModeExternal:
	default:
		return domainprovider.Outpost{}, fmt.Errorf("%w: unsupported outpost mode", apperrors.ErrInvalidArgument)
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = domainprovider.OutpostStatusOffline
	}
	switch status {
	case domainprovider.OutpostStatusOnline, domainprovider.OutpostStatusOffline, domainprovider.OutpostStatusDegraded:
	default:
		return domainprovider.Outpost{}, fmt.Errorf("%w: unsupported outpost status", apperrors.ErrInvalidArgument)
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if outpostID == "" {
		outpostID = uuid.NewString()
	}
	return domainprovider.Outpost{
		ID:        outpostID,
		Name:      name,
		Mode:      mode,
		Endpoint:  strings.TrimSpace(input.Endpoint),
		Status:    status,
		Version:   strings.TrimSpace(input.Version),
		Metadata:  metadata,
		CreatedBy: actorID(principal),
		UpdatedBy: actorID(principal),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func oidcClientFromInput(clientID string, input domainprovider.OIDCClientInput, plainSecret string) (domainprovider.OIDCClient, error) {
	providerID := strings.TrimSpace(input.ProviderID)
	if providerID == "" {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: provider_id is required", apperrors.ErrInvalidArgument)
	}
	oidcClientID := strings.TrimSpace(input.ClientID)
	if oidcClientID == "" {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: client_id is required", apperrors.ErrInvalidArgument)
	}
	redirectURIs, err := normalizeRedirectURIs(input.RedirectURIs)
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	if len(redirectURIs) == 0 {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: at least one redirect_uri is required", apperrors.ErrInvalidArgument)
	}
	scopes := normalizeAllowedScopes(input.AllowedScopes)
	grantTypes, err := normalizeOIDCClientGrantTypes(input.AllowedGrantTypes)
	if err != nil {
		return domainprovider.OIDCClient{}, err
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = domainprovider.OIDCClientStatusEnabled
	}
	if status != domainprovider.OIDCClientStatusEnabled && status != domainprovider.OIDCClientStatusDisabled {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: unsupported oidc client status", apperrors.ErrInvalidArgument)
	}
	secretHash := ""
	if strings.TrimSpace(plainSecret) != "" {
		raw, err := bcrypt.GenerateFromPassword([]byte(plainSecret), bcrypt.DefaultCost)
		if err != nil {
			return domainprovider.OIDCClient{}, fmt.Errorf("hash oidc client secret: %w", err)
		}
		secretHash = string(raw)
	}
	now := time.Now().UTC()
	if clientID == "" {
		clientID = uuid.NewString()
	}
	return domainprovider.OIDCClient{
		ID:                     clientID,
		ProviderID:             providerID,
		ClientID:               oidcClientID,
		ClientSecretHash:       secretHash,
		RedirectURIs:           redirectURIs,
		AllowedScopes:          scopes,
		AllowedGrantTypes:      grantTypes,
		RequirePKCE:            input.RequirePKCE,
		AccessTokenTTLSeconds:  ttlSeconds(input.AccessTokenTTLSeconds, defaultOIDCAccessTokenTTLSeconds),
		IDTokenTTLSeconds:      ttlSeconds(input.IDTokenTTLSeconds, defaultOIDCIDTokenTTLSeconds),
		RefreshTokenTTLSeconds: 0,
		Status:                 status,
		CreatedAt:              now,
		UpdatedAt:              now,
	}, nil
}

func normalizeRequestedScopes(scope string, allowed []string) ([]string, error) {
	requested := compactStrings(strings.Fields(scope))
	if len(requested) == 0 {
		requested = []string{"openid"}
	}
	if !containsString(requested, "openid") {
		return nil, fmt.Errorf("%w: openid scope is required", apperrors.ErrInvalidArgument)
	}
	allowed = normalizeAllowedScopes(allowed)
	for _, item := range requested {
		if !containsString(allowed, item) {
			return nil, fmt.Errorf("%w: scope is not allowed", apperrors.ErrInvalidArgument)
		}
	}
	sort.Strings(requested)
	return requested, nil
}

func normalizeAllowedScopes(values []string) []string {
	out := compactStrings(values)
	if len(out) == 0 {
		out = []string{"openid", "profile", "email"}
	}
	if !containsString(out, "openid") {
		out = append(out, "openid")
	}
	sort.Strings(out)
	return out
}

func normalizeGrantTypes(values []string) []string {
	out := compactStrings(values)
	if len(out) == 0 {
		out = []string{"authorization_code"}
	}
	sort.Strings(out)
	return out
}

func normalizeOIDCClientGrantTypes(values []string) ([]string, error) {
	grantTypes := normalizeGrantTypes(values)
	for _, grantType := range grantTypes {
		if grantType != "authorization_code" {
			return nil, fmt.Errorf("%w: only authorization_code grant is supported", apperrors.ErrInvalidArgument)
		}
	}
	return []string{"authorization_code"}, nil
}

func normalizeRedirectURIs(values []string) ([]string, error) {
	out := compactStrings(values)
	for _, value := range out {
		parsed, err := url.Parse(value)
		if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
			return nil, fmt.Errorf("%w: redirect_uri must be an absolute URI without fragment", apperrors.ErrInvalidArgument)
		}
		if parsed.Scheme != "https" && (parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname())) {
			return nil, fmt.Errorf("%w: redirect_uri must use https except loopback localhost", apperrors.ErrInvalidArgument)
		}
	}
	return out, nil
}

func oidcLaunchRedirectURI(application domainportal.Application, client domainprovider.OIDCClient) string {
	if value := applicationMetadataString(application.Metadata, "oidcRedirectUri", "redirectUri"); value != "" {
		return value
	}
	if launchURL := strings.TrimSpace(application.LaunchURL); launchURL != "" && containsString(client.RedirectURIs, launchURL) {
		return launchURL
	}
	if len(client.RedirectURIs) == 0 {
		return ""
	}
	return client.RedirectURIs[0]
}

func oidcLaunchScopes(application domainportal.Application, client domainprovider.OIDCClient) ([]string, error) {
	if values := applicationMetadataStringSlice(application.Metadata, "oidcScopes", "scopes"); len(values) > 0 {
		return validateOIDCLaunchScopes(values, client.AllowedScopes)
	}
	allowed := normalizeAllowedScopes(client.AllowedScopes)
	defaults := []string{"openid", "profile", "email"}
	out := make([]string, 0, len(defaults))
	for _, item := range defaults {
		if containsString(allowed, item) {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		out = []string{"openid"}
	}
	return out, nil
}

func validateOIDCLaunchScopes(values, allowed []string) ([]string, error) {
	scopes := compactStrings(values)
	if !containsString(scopes, "openid") {
		scopes = append([]string{"openid"}, scopes...)
	}
	allowed = normalizeAllowedScopes(allowed)
	for _, scope := range scopes {
		if !containsString(allowed, scope) {
			return nil, fmt.Errorf("%w: oidc scope is not allowed", apperrors.ErrInvalidArgument)
		}
	}
	return scopes, nil
}

func normalizeCodeChallengeMethod(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "S256"
	}
	return strings.ToUpper(value)
}

func normalizeIssuer(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "http://localhost:8080"
	}
	return strings.TrimRight(value, "/")
}

func clientEnabled(client domainprovider.OIDCClient) bool {
	return client.Status == "" || client.Status == domainprovider.OIDCClientStatusEnabled
}

func providerEnabled(provider domainprovider.Provider) bool {
	return provider.Enabled && (provider.Status == "" || provider.Status == domainprovider.ProviderStatusEnabled)
}

func proxyOriginalURL(input domainprovider.ProxyAuthInput) string {
	if value := strings.TrimSpace(input.OriginalURL); value != "" {
		return value
	}
	proto := strings.TrimSpace(input.ForwardedProto)
	if proto == "" {
		proto = "https"
	}
	host := proxyRequestHost(input)
	uri := strings.TrimSpace(firstNonEmpty(input.ForwardedURI, input.RequestPath))
	if uri == "" {
		uri = "/"
	}
	if host != "" {
		return proto + "://" + host + uri
	}
	return uri
}

func proxyRequestHost(input domainprovider.ProxyAuthInput) string {
	return normalizeProxyHost(firstNonEmpty(input.ForwardedHost, hostFromURL(input.OriginalURL), input.RequestHost))
}

func proxyPath(input domainprovider.ProxyAuthInput) string {
	value := strings.TrimSpace(firstNonEmpty(input.ForwardedURI, pathFromURL(input.OriginalURL), input.RequestPath))
	if value == "" {
		return "/"
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		value = parsed.Path
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func normalizeReverseProxyPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func validateReverseProxyUpstream(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: reverse proxy upstream URL must use http or https", apperrors.ErrInvalidArgument)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: reverse proxy upstream URL must not include credentials or a fragment", apperrors.ErrInvalidArgument)
	}
	return parsed.String(), nil
}

func proxyLoginURL(originalURL string) string {
	target := strings.TrimSpace(originalURL)
	if target == "" {
		target = "/portal"
	}
	return "/api/v1/provider/proxy/start?return_to=" + url.QueryEscape(target)
}

func proxyHostMatches(provider domainprovider.Provider, application domainportal.Application, requestHost string) bool {
	requestHost = normalizeProxyHost(requestHost)
	if requestHost == "" {
		return false
	}
	for _, host := range proxyHostCandidates(provider, application) {
		if proxyHostsEqual(host, requestHost) {
			return true
		}
	}
	return false
}

func proxyHostCandidates(provider domainprovider.Provider, application domainportal.Application) []string {
	out := make([]string, 0)
	out = append(out, configStringSlice(provider.Config, "externalHosts", "external_hosts", "hosts")...)
	out = append(out, configString(provider.Config, "externalHost", "external_host", "host"))
	if host := hostFromURL(application.LaunchURL); host != "" {
		out = append(out, host)
	}
	return compactStrings(out)
}

func proxyCookieDomain(provider domainprovider.Provider, requestHost string) string {
	domain := normalizeCookieDomain(configString(provider.Config, "cookieDomain", "cookie_domain"))
	if domain == "" {
		return ""
	}
	host := stripHostPort(normalizeProxyHost(requestHost))
	if host == "" || strings.Contains(host, ":") {
		return ""
	}
	if host == domain || strings.HasSuffix(host, "."+domain) {
		return domain
	}
	return ""
}

func normalizeCookieDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, ".")
	value = strings.TrimSuffix(value, ".")
	if value == "" || strings.ContainsAny(value, "/: \t\r\n") {
		return ""
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return ""
	}
	if !strings.Contains(value, ".") {
		return ""
	}
	return value
}

func proxyPathPrefix(provider domainprovider.Provider) string {
	value := strings.TrimSpace(configString(provider.Config, "pathPrefix", "path_prefix", "protectedPathPrefix", "protected_path_prefix"))
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return strings.TrimRight(value, "/")
}

func proxyPathSkipped(provider domainprovider.Provider, requestPath string) bool {
	for _, prefix := range configStringSlice(provider.Config, "skipAuthPaths", "skip_auth_paths") {
		if pathHasPrefix(requestPath, prefix) {
			return true
		}
	}
	return false
}

func pathHasPrefix(requestPath, prefix string) bool {
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return true
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}

func proxyIdentityHeaders(principal domainidentity.Principal, provider domainprovider.Provider) map[string]string {
	defaults := map[string]string{
		"user":     "X-Soha-User",
		"userId":   "X-Soha-User-ID",
		"email":    "X-Soha-Email",
		"roles":    "X-Soha-Roles",
		"teams":    "X-Soha-Teams",
		"groups":   "X-Soha-Groups",
		"projects": "X-Soha-Projects",
		"tags":     "X-Soha-Tags",
	}
	claims := map[string]string{
		"user":     firstNonEmpty(principal.UserName, principal.UserID),
		"userId":   principal.UserID,
		"email":    principal.Email,
		"roles":    strings.Join(principal.Roles, ","),
		"teams":    strings.Join(principal.Teams, ","),
		"groups":   strings.Join(compactStrings(append(append([]string{}, principal.Roles...), principal.Teams...)), ","),
		"projects": strings.Join(principal.Projects, ","),
		"tags":     strings.Join(principal.Tags, ","),
	}
	mapping := configStringMap(provider.Config, "headerMappings", "header_mappings")
	headers := make(map[string]string, len(defaults))
	for claim, defaultHeader := range defaults {
		headerName := firstNonEmpty(mapping[claim], defaultHeader)
		if !isSafeHeaderName(headerName) {
			continue
		}
		if value := strings.TrimSpace(claims[claim]); value != "" {
			headers[textproto.CanonicalMIMEHeaderKey(headerName)] = value
		}
	}
	return headers
}

func proxyHostsEqual(left, right string) bool {
	left = normalizeProxyHost(left)
	right = normalizeProxyHost(right)
	if left == "" || right == "" {
		return false
	}
	return left == right || stripHostPort(left) == stripHostPort(right)
}

func normalizeProxyHost(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		return hostFromURL(value)
	}
	value = strings.TrimSuffix(value, ".")
	return value
}

func stripHostPort(value string) string {
	value = normalizeProxyHost(value)
	if strings.Count(value, ":") == 1 {
		if host, _, ok := strings.Cut(value, ":"); ok {
			return host
		}
	}
	return value
}

func hostFromURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return normalizeProxyHost(parsed.Host)
}

func pathFromURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return parsed.RequestURI()
}

func configString(config map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := config[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		}
	}
	return ""
}

func configStringSlice(config map[string]any, keys ...string) []string {
	out := make([]string, 0)
	for _, key := range keys {
		value, ok := config[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			out = append(out, strings.Split(typed, ",")...)
		case []string:
			out = append(out, typed...)
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
		}
	}
	return compactStrings(out)
}

func configStringMap(config map[string]any, keys ...string) map[string]string {
	out := map[string]string{}
	for _, key := range keys {
		value, ok := config[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case map[string]string:
			for itemKey, itemValue := range typed {
				out[itemKey] = strings.TrimSpace(itemValue)
			}
		case map[string]any:
			for itemKey, itemValue := range typed {
				if text, ok := itemValue.(string); ok {
					out[itemKey] = strings.TrimSpace(text)
				}
			}
		}
	}
	return out
}

func configBoolean(config map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := config[key].(bool); ok {
			return value
		}
	}
	return false
}

func applicationMetadataString(metadata map[string]any, keys ...string) string {
	if value := metadataStringFromMap(metadata, keys...); value != "" {
		return value
	}
	if nested := metadataObject(metadata, "oidc"); nested != nil {
		return metadataStringFromMap(nested, keys...)
	}
	return ""
}

func applicationMetadataStringSlice(metadata map[string]any, keys ...string) []string {
	if values := metadataStringSliceFromMap(metadata, keys...); len(values) > 0 {
		return values
	}
	if nested := metadataObject(metadata, "oidc"); nested != nil {
		return metadataStringSliceFromMap(nested, keys...)
	}
	return nil
}

func metadataStringFromMap(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func metadataStringSliceFromMap(metadata map[string]any, keys ...string) []string {
	out := make([]string, 0)
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			out = append(out, strings.Fields(typed)...)
		case []string:
			out = append(out, typed...)
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
		}
	}
	return compactStrings(out)
}

func metadataObject(metadata map[string]any, key string) map[string]any {
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	default:
		return nil
	}
}

func isSafeHeaderName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ":\r\n") {
		return false
	}
	for _, char := range value {
		if char <= 32 || char >= 127 {
			return false
		}
	}
	return true
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action, result string, provider domainprovider.Provider, client domainprovider.OIDCClient, metadata map[string]any) {
	if s.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["providerId"] = provider.ID
	metadata["providerType"] = provider.Type
	if client.ClientID != "" {
		metadata["clientId"] = client.ClientID
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ActorID:       actorID(principal),
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "IdentityProvider",
		ResourceName:  provider.ID,
		Action:        action,
		Result:        result,
		Summary:       action,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
		CreatedAt:     time.Now().UTC(),
	})
}

func (s *Service) recordOutpostEvent(ctx context.Context, outpost domainprovider.Outpost, event domainprovider.OutpostEvent) {
	if s.audit == nil {
		return
	}
	eventType := strings.TrimSpace(event.EventType)
	if eventType == "" {
		eventType = "access"
	}
	result := strings.TrimSpace(event.Result)
	if result == "" {
		result = "reported"
	}
	providerID := strings.TrimSpace(event.ProviderID)
	providerType := domainprovider.ProviderTypeProxy
	if providerID == "" {
		providerID = outpost.ID
		providerType = "outpost"
	}
	metadata := cloneMap(event.Metadata)
	metadata["eventType"] = eventType
	metadata["outpostId"] = outpost.ID
	metadata["outpostMode"] = outpost.Mode
	if applicationID := strings.TrimSpace(event.ApplicationID); applicationID != "" {
		metadata["applicationId"] = applicationID
	}
	if originalURL := strings.TrimSpace(event.OriginalURL); originalURL != "" {
		metadata["originalUrl"] = originalURL
	}
	if reason := strings.TrimSpace(event.Reason); reason != "" {
		metadata["reason"] = reason
	}
	if userAgent := strings.TrimSpace(event.UserAgent); userAgent != "" {
		metadata["userAgent"] = userAgent
	}
	if event.CreatedAt != nil {
		metadata["reportedAt"] = event.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	meta := requestctx.FromContext(ctx)
	sourceIP := firstNonEmpty(strings.TrimSpace(event.SourceIP), meta.SourceIP)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ResourceKind:  "IdentityProvider",
		ResourceName:  providerID,
		Action:        "outpost.event",
		Result:        result,
		Summary:       "outpost event",
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      sourceIP,
		Metadata: map[string]any{
			"providerId":   providerID,
			"providerType": providerType,
			"event":        metadata,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func base64urlUInt(value *big.Int) string {
	raw := value.Bytes()
	if len(raw) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(raw):], raw)
		raw = padded
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func containsString(values []string, value string) bool {
	return slices.Contains(values, strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func ttlSeconds(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func actorID(principal domainidentity.Principal) string {
	if strings.TrimSpace(principal.UserID) != "" {
		return principal.UserID
	}
	return "system"
}
