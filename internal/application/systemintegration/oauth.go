package systemintegration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const (
	gitLabAuthModeToken = "access_token"
	gitLabAuthModeOAuth = "oauth"
	oauthStateTTL       = 10 * time.Minute
)

type OAuthProviderConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Timeout      time.Duration
}

type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type OAuthProvider interface {
	AuthorizationURL(OAuthProviderConfig, string) (string, error)
	Exchange(context.Context, OAuthProviderConfig, string) (OAuthToken, error)
	Refresh(context.Context, OAuthProviderConfig, string) (OAuthToken, error)
}

type OAuthCallbackInput struct {
	Code              string
	State             string
	ProviderError     string
	ProviderErrorText string
}

type oauthState struct {
	IntegrationID string    `json:"integrationId"`
	ProviderType  string    `json:"providerType"`
	ActorID       string    `json:"actorId"`
	ActorName     string    `json:"actorName,omitempty"`
	ReturnURI     string    `json:"returnUri,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (s *Service) BeginOAuth(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SystemIntegrationOAuthAuthorization, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsManage); err != nil {
		return sohaapi.SystemIntegrationOAuthAuthorization{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.SystemIntegrationOAuthAuthorization{}, err
	}
	credentials, err := s.decryptCredentials(ctx, item.ID)
	if err != nil {
		return sohaapi.SystemIntegrationOAuthAuthorization{}, err
	}
	config, provider, err := s.oauthProvider(item, credentials)
	if err != nil {
		return sohaapi.SystemIntegrationOAuthAuthorization{}, err
	}
	returnURI := strings.TrimSpace(configurationMap(item.Configuration)["oauth_return_uri"])
	if returnURI == "" {
		returnURI = config.RedirectURI
	}
	state, err := s.encryptOAuthState(oauthState{
		IntegrationID: item.ID,
		ProviderType:  item.ProviderType,
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		ReturnURI:     returnURI,
		ExpiresAt:     s.now().UTC().Add(oauthStateTTL),
	})
	if err != nil {
		return sohaapi.SystemIntegrationOAuthAuthorization{}, err
	}
	authorizationURL, err := provider.AuthorizationURL(config, state)
	if err != nil {
		return sohaapi.SystemIntegrationOAuthAuthorization{}, fmt.Errorf("build oauth authorization url: %w", err)
	}
	return sohaapi.SystemIntegrationOAuthAuthorization{AuthorizationURL: authorizationURL}, nil
}

func (s *Service) CompleteOAuth(ctx context.Context, input OAuthCallbackInput) (string, error) {
	state, err := s.decryptOAuthState(input.State)
	if err != nil {
		return "", err
	}
	item, err := s.repo.Get(ctx, state.IntegrationID)
	if err != nil {
		return "", err
	}
	credentials, err := s.decryptCredentials(ctx, item.ID)
	if err != nil {
		return "", err
	}
	config, provider, err := s.oauthProvider(item, credentials)
	if err != nil {
		return "", err
	}
	if item.ProviderType != state.ProviderType {
		return "", fmt.Errorf("%w: oauth provider changed", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.ProviderError) != "" {
		return oauthReturnURL(state.ReturnURI, item.ID, "error"), nil
	}
	if strings.TrimSpace(input.Code) == "" {
		return "", fmt.Errorf("%w: oauth authorization code is required", apperrors.ErrInvalidArgument)
	}
	token, err := provider.Exchange(ctx, config, strings.TrimSpace(input.Code))
	if err != nil {
		_ = s.repo.UpdateHealth(ctx, item.ID, domain.HealthUnhealthy, "oauth token exchange failed", s.now().UTC())
		return oauthReturnURL(state.ReturnURI, item.ID, "error"), nil
	}
	principal := domainidentity.Principal{UserID: state.ActorID, UserName: state.ActorName}
	if _, _, err := s.persistOAuthToken(ctx, item, token, principal.UserID); err != nil {
		return "", err
	}
	s.recordMutation(ctx, principal, "settings.system-integration.oauth.authorize", item, "authorized system integration oauth")
	return oauthReturnURL(state.ReturnURI, item.ID, "success"), nil
}

func (s *Service) refreshOAuthCredentials(ctx context.Context, item domain.Integration, credentials map[string]string) (domain.Integration, map[string]string, error) {
	configValues := configurationMap(item.Configuration)
	if normalizedGitLabAuthMode(configValues) != gitLabAuthModeOAuth {
		return item, credentials, nil
	}
	accessToken := strings.TrimSpace(credentials["access_token"])
	expiresAt, _ := time.Parse(time.RFC3339, configValues["oauth_expires_at"])
	if accessToken != "" && (expiresAt.IsZero() || s.now().UTC().Add(time.Minute).Before(expiresAt)) {
		return item, credentials, nil
	}
	refreshToken := strings.TrimSpace(credentials["refresh_token"])
	if refreshToken == "" {
		return domain.Integration{}, nil, fmt.Errorf("%w: gitlab oauth authorization is required", apperrors.ErrInvalidArgument)
	}
	providerConfig, provider, err := s.oauthProvider(item, credentials)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	token, err := provider.Refresh(ctx, providerConfig, refreshToken)
	if err != nil {
		return domain.Integration{}, nil, fmt.Errorf("refresh gitlab oauth token: %w", err)
	}
	updated, refreshed, err := s.persistOAuthToken(ctx, item, token, "system")
	if err != nil {
		return domain.Integration{}, nil, err
	}
	for key, value := range credentials {
		if _, exists := refreshed[key]; !exists {
			refreshed[key] = value
		}
	}
	return updated, refreshed, nil
}

func (s *Service) oauthProvider(item domain.Integration, credentials map[string]string) (OAuthProviderConfig, OAuthProvider, error) {
	config := configurationMap(item.Configuration)
	if item.Category != domain.CategorySourceControl || item.ProviderType != domain.ProviderGitLab || normalizedGitLabAuthMode(config) != gitLabAuthModeOAuth {
		return OAuthProviderConfig{}, nil, fmt.Errorf("%w: integration is not configured for gitlab oauth", apperrors.ErrInvalidArgument)
	}
	provider := s.oauth[item.ProviderType]
	if provider == nil {
		return OAuthProviderConfig{}, nil, fmt.Errorf("%w: oauth provider is unavailable", apperrors.ErrInvalidArgument)
	}
	timeout, _ := time.ParseDuration(config["timeout"])
	return OAuthProviderConfig{
		BaseURL:      config["base_url"],
		ClientID:     config["client_id"],
		ClientSecret: credentials["client_secret"],
		RedirectURI:  config["oauth_redirect_uri"],
		Timeout:      timeout,
	}, provider, nil
}

func (s *Service) persistOAuthToken(ctx context.Context, item domain.Integration, token OAuthToken, actor string) (domain.Integration, map[string]string, error) {
	if strings.TrimSpace(token.AccessToken) == "" {
		return domain.Integration{}, nil, fmt.Errorf("%w: oauth access token is missing", apperrors.ErrInvalidArgument)
	}
	inputs := []sohaapi.SystemIntegrationCredentialInput{{Key: "access_token", Value: token.AccessToken}}
	if strings.TrimSpace(token.RefreshToken) != "" {
		inputs = append(inputs, sohaapi.SystemIntegrationCredentialInput{Key: "refresh_token", Value: token.RefreshToken})
	}
	encrypted, err := s.encryptCredentialInputs(inputs)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	expiresAt := ""
	if token.ExpiresIn > 0 {
		expiresAt = s.now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	item.Configuration = setConfigurationValue(item.Configuration, "oauth_expires_at", expiresAt)
	item.UpdatedBy = actor
	item.UpdatedAt = s.now().UTC()
	updated, err := s.repo.Update(ctx, item, item.Version, encrypted, nil)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	plain := map[string]string{"access_token": strings.TrimSpace(token.AccessToken)}
	if strings.TrimSpace(token.RefreshToken) != "" {
		plain["refresh_token"] = strings.TrimSpace(token.RefreshToken)
	}
	return updated, plain, nil
}

func (s *Service) encryptOAuthState(state oauthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal oauth state: %w", err)
	}
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(payload))
	if err != nil {
		return "", fmt.Errorf("encrypt oauth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(encrypted)), nil
}

func (s *Service) decryptOAuthState(value string) (oauthState, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return oauthState{}, fmt.Errorf("%w: invalid oauth state", apperrors.ErrInvalidArgument)
	}
	payload, err := secretcrypto.DecryptStringWithKeyring(s.keys, string(encoded))
	if err != nil {
		return oauthState{}, fmt.Errorf("%w: invalid oauth state", apperrors.ErrInvalidArgument)
	}
	var state oauthState
	if err := json.Unmarshal([]byte(payload), &state); err != nil || state.IntegrationID == "" || state.ProviderType == "" || !s.now().UTC().Before(state.ExpiresAt) {
		return oauthState{}, fmt.Errorf("%w: expired or invalid oauth state", apperrors.ErrInvalidArgument)
	}
	return state, nil
}

func normalizedGitLabAuthMode(config map[string]string) string {
	mode := strings.ToLower(strings.TrimSpace(config["auth_mode"]))
	if mode == "" {
		return gitLabAuthModeToken
	}
	return mode
}

func setConfigurationValue(fields []sohaapi.SystemIntegrationConfigurationField, key, value string) []sohaapi.SystemIntegrationConfigurationField {
	result := make([]sohaapi.SystemIntegrationConfigurationField, 0, len(fields)+1)
	found := false
	for _, field := range fields {
		if field.Key == key {
			field.Value = value
			found = true
		}
		result = append(result, field)
	}
	if !found {
		result = append(result, sohaapi.SystemIntegrationConfigurationField{Key: key, Value: value})
	}
	return result
}

func oauthReturnURL(redirectURI, integrationID, status string) string {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return ""
	}
	parsed.Path = "/settings/source-control/" + url.PathEscape(integrationID)
	parsed.RawQuery = "oauth=" + url.QueryEscape(status)
	parsed.Fragment = ""
	return parsed.String()
}
