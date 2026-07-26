package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appsystemintegration "github.com/opensoha/soha/internal/application/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const maxOAuthResponseBytes = 1 << 20

type OAuthProvider struct{}

func NewOAuthProvider() *OAuthProvider { return &OAuthProvider{} }

func (*OAuthProvider) AuthorizationURL(config appsystemintegration.OAuthProviderConfig, state string) (string, error) {
	baseURL, err := oauthBaseURL(config.BaseURL)
	if err != nil {
		return "", err
	}
	values := url.Values{
		"client_id":     []string{strings.TrimSpace(config.ClientID)},
		"redirect_uri":  []string{strings.TrimSpace(config.RedirectURI)},
		"response_type": []string{"code"},
		"scope":         []string{"api"},
		"state":         []string{strings.TrimSpace(state)},
	}
	return baseURL + "/oauth/authorize?" + values.Encode(), nil
}

func (p *OAuthProvider) Exchange(ctx context.Context, config appsystemintegration.OAuthProviderConfig, code string) (appsystemintegration.OAuthToken, error) {
	return p.requestToken(ctx, config, url.Values{
		"grant_type":    []string{"authorization_code"},
		"code":          []string{strings.TrimSpace(code)},
		"redirect_uri":  []string{strings.TrimSpace(config.RedirectURI)},
		"client_id":     []string{strings.TrimSpace(config.ClientID)},
		"client_secret": []string{strings.TrimSpace(config.ClientSecret)},
	})
}

func (p *OAuthProvider) Refresh(ctx context.Context, config appsystemintegration.OAuthProviderConfig, refreshToken string) (appsystemintegration.OAuthToken, error) {
	return p.requestToken(ctx, config, url.Values{
		"grant_type":    []string{"refresh_token"},
		"refresh_token": []string{strings.TrimSpace(refreshToken)},
		"redirect_uri":  []string{strings.TrimSpace(config.RedirectURI)},
		"client_id":     []string{strings.TrimSpace(config.ClientID)},
		"client_secret": []string{strings.TrimSpace(config.ClientSecret)},
	})
}

func (*OAuthProvider) requestToken(ctx context.Context, config appsystemintegration.OAuthProviderConfig, values url.Values) (appsystemintegration.OAuthToken, error) {
	baseURL, err := oauthBaseURL(config.BaseURL)
	if err != nil {
		return appsystemintegration.OAuthToken{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/oauth/token", strings.NewReader(values.Encode()))
	if err != nil {
		return appsystemintegration.OAuthToken{}, fmt.Errorf("build gitlab oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return appsystemintegration.OAuthToken{}, fmt.Errorf("%w: gitlab oauth request failed", apperrors.ErrClusterUnready)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return appsystemintegration.OAuthToken{}, fmt.Errorf("%w: gitlab oauth request returned status %d", apperrors.ErrClusterUnready, resp.StatusCode)
	}
	var response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    any    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOAuthResponseBytes)).Decode(&response); err != nil {
		return appsystemintegration.OAuthToken{}, fmt.Errorf("decode gitlab oauth response: %w", err)
	}
	expiresIn, err := oauthExpiresIn(response.ExpiresIn)
	if err != nil {
		return appsystemintegration.OAuthToken{}, err
	}
	if strings.TrimSpace(response.AccessToken) == "" {
		return appsystemintegration.OAuthToken{}, fmt.Errorf("%w: gitlab oauth response omitted access token", apperrors.ErrClusterUnready)
	}
	return appsystemintegration.OAuthToken{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		ExpiresIn:    expiresIn,
	}, nil
}

func oauthBaseURL(apiURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: invalid gitlab api url", apperrors.ErrInvalidArgument)
	}
	parsed.Path = strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/api/v4")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func oauthExpiresIn(value any) (int64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case float64:
		if typed < 0 {
			return 0, fmt.Errorf("invalid gitlab oauth expires_in")
		}
		return int64(typed), nil
	case string:
		result, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil || result < 0 {
			return 0, fmt.Errorf("invalid gitlab oauth expires_in")
		}
		return result, nil
	default:
		return 0, fmt.Errorf("invalid gitlab oauth expires_in")
	}
}
