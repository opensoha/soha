package identityprovider

import (
	"context"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
)

const (
	ProviderTypeOIDC  = "oidc"
	ProviderTypeProxy = "proxy"

	ProviderStatusEnabled  = "enabled"
	ProviderStatusDisabled = "disabled"

	OIDCClientStatusEnabled  = "enabled"
	OIDCClientStatusDisabled = "disabled"

	TokenTypeAccess = "access"
	TokenTypeID     = "id"

	ProxyDecisionAllow    = "allow"
	ProxyDecisionDeny     = "deny"
	ProxyDecisionLogin    = "login"
	ProxyModeForwardAuth  = "forward_auth"
	ProxyModeReverseProxy = "reverse_proxy"

	OutpostModeEmbedded   = "embedded"
	OutpostModeAgent      = "agent"
	OutpostModeKubernetes = "kubernetes"
	OutpostModeExternal   = "external"

	OutpostStatusOnline   = "online"
	OutpostStatusOffline  = "offline"
	OutpostStatusDegraded = "degraded"
)

type Provider struct {
	ID            string         `json:"id"`
	ApplicationID string         `json:"applicationId"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Enabled       bool           `json:"enabled"`
	Config        map[string]any `json:"config,omitempty"`
	SecretRefs    map[string]any `json:"secretRefs,omitempty"`
	Status        string         `json:"status"`
	CreatedBy     string         `json:"createdBy,omitempty"`
	UpdatedBy     string         `json:"updatedBy,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ProviderInput struct {
	ApplicationID string         `json:"applicationId"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Enabled       bool           `json:"enabled"`
	Config        map[string]any `json:"config"`
	SecretRefs    map[string]any `json:"secretRefs"`
	Status        string         `json:"status"`
}

type ProviderFilter struct {
	ApplicationID string
	Type          string
	Status        string
	Limit         int
	Offset        int
}

type OIDCClient struct {
	ID                     string    `json:"id"`
	ProviderID             string    `json:"providerId"`
	ClientID               string    `json:"clientId"`
	ClientSecretHash       string    `json:"-"`
	RedirectURIs           []string  `json:"redirectUris"`
	AllowedScopes          []string  `json:"allowedScopes"`
	AllowedGrantTypes      []string  `json:"allowedGrantTypes"`
	RequirePKCE            bool      `json:"requirePkce"`
	AccessTokenTTLSeconds  int       `json:"accessTokenTtlSeconds"`
	IDTokenTTLSeconds      int       `json:"idTokenTtlSeconds"`
	RefreshTokenTTLSeconds int       `json:"refreshTokenTtlSeconds"`
	Status                 string    `json:"status"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type OIDCClientInput struct {
	ProviderID             string   `json:"providerId"`
	ClientID               string   `json:"clientId"`
	ClientSecret           string   `json:"clientSecret"`
	RedirectURIs           []string `json:"redirectUris"`
	AllowedScopes          []string `json:"allowedScopes"`
	AllowedGrantTypes      []string `json:"allowedGrantTypes"`
	RequirePKCE            bool     `json:"requirePkce"`
	AccessTokenTTLSeconds  int      `json:"accessTokenTtlSeconds"`
	IDTokenTTLSeconds      int      `json:"idTokenTtlSeconds"`
	RefreshTokenTTLSeconds int      `json:"refreshTokenTtlSeconds"`
	Status                 string   `json:"status"`
}

type OIDCClientCreated struct {
	Client       OIDCClient `json:"client"`
	ClientSecret string     `json:"clientSecret,omitempty"`
}

type Outpost struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Mode       string         `json:"mode"`
	Endpoint   string         `json:"endpoint,omitempty"`
	Token      string         `json:"token,omitempty"`
	TokenHash  string         `json:"-"`
	Status     string         `json:"status"`
	Version    string         `json:"version,omitempty"`
	LastSeenAt *time.Time     `json:"lastSeenAt,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedBy  string         `json:"createdBy,omitempty"`
	UpdatedBy  string         `json:"updatedBy,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type OutpostInput struct {
	Name     string         `json:"name"`
	Mode     string         `json:"mode"`
	Endpoint string         `json:"endpoint"`
	Status   string         `json:"status"`
	Version  string         `json:"version"`
	Metadata map[string]any `json:"metadata"`
}

type OutpostFilter struct {
	Mode   string
	Status string
	Limit  int
	Offset int
}

type OutpostClaimInput struct {
	OutpostID string         `json:"outpostId"`
	Token     string         `json:"token"`
	Version   string         `json:"version"`
	Metadata  map[string]any `json:"metadata"`
}

type OutpostClaimResult struct {
	Outpost   Outpost    `json:"outpost"`
	Providers []Provider `json:"providers"`
}

type OutpostHeartbeatInput struct {
	Token    string         `json:"token"`
	Status   string         `json:"status"`
	Version  string         `json:"version"`
	Metadata map[string]any `json:"metadata"`
}

type OutpostHeartbeatResult struct {
	Outpost Outpost `json:"outpost"`
}

type OutpostCheckInput struct {
	Token          string `json:"token"`
	ProviderID     string `json:"providerId"`
	OriginalURL    string `json:"originalUrl"`
	ForwardedHost  string `json:"forwardedHost"`
	ForwardedProto string `json:"forwardedProto"`
	ForwardedURI   string `json:"forwardedUri"`
	RequestHost    string `json:"requestHost"`
	RequestPath    string `json:"requestPath"`
	Method         string `json:"method"`
	SessionToken   string `json:"sessionToken"`
}

type OutpostEvent struct {
	EventType     string         `json:"eventType"`
	ProviderID    string         `json:"providerId,omitempty"`
	ApplicationID string         `json:"applicationId,omitempty"`
	Result        string         `json:"result,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	OriginalURL   string         `json:"originalUrl,omitempty"`
	SourceIP      string         `json:"sourceIp,omitempty"`
	UserAgent     string         `json:"userAgent,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     *time.Time     `json:"createdAt,omitempty"`
}

type OutpostEventsInput struct {
	Token  string         `json:"token"`
	Events []OutpostEvent `json:"events"`
}

type OutpostEventsResult struct {
	Accepted int `json:"accepted"`
}

type SigningKey struct {
	ID                  string         `json:"id"`
	ProviderID          string         `json:"providerId"`
	KeyID               string         `json:"kid"`
	Algorithm           string         `json:"alg"`
	EncryptedPrivateKey string         `json:"-"`
	PublicJWK           map[string]any `json:"publicJwk"`
	Active              bool           `json:"active"`
	CreatedAt           time.Time      `json:"createdAt"`
	RotatedAt           *time.Time     `json:"rotatedAt,omitempty"`
}

type AuthorizationCode struct {
	ID                  string         `json:"id"`
	ProviderID          string         `json:"providerId"`
	ClientID            string         `json:"clientId"`
	UserID              string         `json:"userId"`
	CodeHash            string         `json:"-"`
	RedirectURI         string         `json:"redirectUri"`
	Scopes              []string       `json:"scopes"`
	Nonce               string         `json:"nonce,omitempty"`
	CodeChallenge       string         `json:"codeChallenge,omitempty"`
	CodeChallengeMethod string         `json:"codeChallengeMethod,omitempty"`
	ExpiresAt           time.Time      `json:"expiresAt"`
	ConsumedAt          *time.Time     `json:"consumedAt,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

type DiscoveryDocument struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserInfoEndpoint                  string   `json:"userinfo_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ClaimsSupported                   []string `json:"claims_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
}

type JWKS struct {
	Keys []map[string]any `json:"keys"`
}

type AuthorizeInput struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
}

type AuthorizeResult struct {
	RedirectURI string
	Code        string
	State       string
}

type AuthorizeRedirectError struct {
	RedirectURI string
	State       string
	Code        string
	Description string
	Err         error
}

func (e *AuthorizeRedirectError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Description != "" {
		return e.Description
	}
	if e.Code != "" {
		return e.Code
	}
	return "authorization request failed"
}

func (e *AuthorizeRedirectError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type TokenInput struct {
	GrantType     string
	Code          string
	RedirectURI   string
	ClientID      string
	ClientSecret  string
	CodeVerifier  string
	Authenticated bool
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

type ClientAuthInput struct {
	ClientID     string
	ClientSecret string
}

type IntrospectionResponse struct {
	Active    bool     `json:"active"`
	Subject   string   `json:"sub,omitempty"`
	ClientID  string   `json:"client_id,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	TokenType string   `json:"token_type,omitempty"`
	Issuer    string   `json:"iss,omitempty"`
	Audience  []string `json:"aud,omitempty"`
	ExpiresAt int64    `json:"exp,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
	NotBefore int64    `json:"nbf,omitempty"`
	JWTID     string   `json:"jti,omitempty"`
	Username  string   `json:"username,omitempty"`
}

type UserInfoResponse struct {
	Subject  string   `json:"sub"`
	Name     string   `json:"name,omitempty"`
	Email    string   `json:"email,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Teams    []string `json:"teams,omitempty"`
	Projects []string `json:"projects,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type ProxyAuthInput struct {
	ProviderID     string
	OriginalURL    string
	ForwardedHost  string
	ForwardedProto string
	ForwardedURI   string
	RequestHost    string
	RequestPath    string
	Method         string
	Redirect       bool
	SessionToken   string
}

type ProxySession struct {
	Token     string    `json:"token,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ProxyAuthResult struct {
	Decision     string                   `json:"decision"`
	Reason       string                   `json:"reason,omitempty"`
	LoginURL     string                   `json:"loginUrl,omitempty"`
	OriginalURL  string                   `json:"originalUrl,omitempty"`
	CookieDomain string                   `json:"cookieDomain,omitempty"`
	Provider     Provider                 `json:"provider,omitempty"`
	Application  domainportal.Application `json:"application,omitempty"`
	Headers      map[string]string        `json:"headers,omitempty"`
	Skipped      bool                     `json:"skipped,omitempty"`
}

type ReverseProxyInput struct {
	ProviderID   string
	Path         string
	OriginalURL  string
	Method       string
	SessionToken string
}

type ReverseProxyResult struct {
	Auth             ProxyAuthResult
	UpstreamURL      string
	WebsocketEnabled bool
}

type Repository interface {
	ListProviders(context.Context, ProviderFilter) ([]Provider, error)
	GetProvider(context.Context, string) (Provider, error)
	CreateProvider(context.Context, Provider) (Provider, error)
	UpdateProvider(context.Context, Provider) (Provider, error)
	DeleteProvider(context.Context, string) error
	GetProviderApplication(context.Context, string) (domainportal.Application, error)
	ListOutposts(context.Context, OutpostFilter) ([]Outpost, error)
	GetOutpost(context.Context, string) (Outpost, error)
	CreateOutpost(context.Context, Outpost) (Outpost, error)
	UpdateOutpost(context.Context, Outpost) (Outpost, error)
	DeleteOutpost(context.Context, string) error
	ListOIDCClients(context.Context, string) ([]OIDCClient, error)
	GetOIDCClient(context.Context, string) (OIDCClient, error)
	GetOIDCClientByClientID(context.Context, string) (OIDCClient, error)
	CreateOIDCClient(context.Context, OIDCClient) (OIDCClient, error)
	UpdateOIDCClient(context.Context, OIDCClient) (OIDCClient, error)
	DeleteOIDCClient(context.Context, string) error
	GetActiveSigningKey(context.Context, string) (SigningKey, error)
	CreateSigningKey(context.Context, SigningKey) (SigningKey, error)
	ListActivePublicKeys(context.Context) ([]SigningKey, error)
	CreateAuthorizationCode(context.Context, AuthorizationCode) error
	GetAuthorizationCode(context.Context, string, time.Time) (AuthorizationCode, error)
	ConsumeAuthorizationCode(context.Context, string, time.Time) (AuthorizationCode, error)
}

type PrincipalLoader interface {
	GetByID(context.Context, string) (User, error)
	ListRoles(context.Context, string) ([]string, error)
	ListTeams(context.Context, string) ([]string, error)
	ListProjects(context.Context, string) ([]string, error)
	GetAuthzState(context.Context, string) (AuthzState, error)
}

type User struct {
	ID          string
	Username    string
	Email       string
	DisplayName string
	Status      string
	Tags        []string
}

type AuthzState struct {
	UserID string
	Status string
}

type AuditRecorder interface {
	Record(context.Context, string, string, domainidentity.Principal, map[string]any) error
}
