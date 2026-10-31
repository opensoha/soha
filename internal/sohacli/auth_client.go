package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func (c APIClient) CLIAuthOptions(ctx context.Context) (sohaapi.CLIAuthOptions, error) {
	var out sohaapi.CLIAuthOptionsEnvelope
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/auth/cli/options", "", nil, nil, &out)
	return out.Data, err
}
func (c APIClient) CreateDeviceAuthorization(ctx context.Context, input sohaapi.CLIDeviceAuthorizationCreateRequest) (sohaapi.CLIDeviceAuthorization, error) {
	var out sohaapi.CLIDeviceAuthorizationEnvelope
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/device-authorizations", "", nil, input, &out)
	return out.Data, err
}
func (c APIClient) ExchangeDeviceToken(ctx context.Context, input sohaapi.CLIDeviceTokenRequest) (loginResponse, error) {
	var out loginResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/device-token", "", nil, input, &out)
	return out, err
}
func (c APIClient) CancelDeviceAuthorization(ctx context.Context, input sohaapi.CLIDeviceCancelRequest) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/device-authorizations/cancel", "", nil, input, nil)
}
func (c APIClient) CLIPassword(ctx context.Context, login, password string) (loginResponse, error) {
	var out loginResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/password", "", nil, sohaapi.PasswordLoginRequest{Login: login, Password: password}, &out)
	return out, err
}
func (c APIClient) CLIRefresh(ctx context.Context, input sohaapi.CLIRefreshRequest) (refreshResponse, error) {
	var out refreshResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/refresh", "", nil, input, &out)
	return out, err
}
func (c APIClient) CLILogout(ctx context.Context, refresh string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/logout", "", nil, sohaapi.CLILogoutRequest{RefreshToken: refresh}, nil)
}
func (c APIClient) AuthContext(ctx context.Context) (sohaapi.CLIAuthContext, error) {
	var out sohaapi.CLIAuthContextEnvelope
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/auth/context", c.Token, nil, nil, &out)
	return out.Data, err
}
func (c APIClient) CLISessions(ctx context.Context, limit int, cursor string) (sohaapi.CLISessionPage, error) {
	var out sohaapi.CLISessionPageEnvelope
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	path := "/api/v1/auth/cli/sessions"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	err := c.doJSON(ctx, http.MethodGet, path, c.Token, nil, nil, &out)
	return out.Data, err
}
func (c APIClient) RevokeCLISession(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/auth/cli/sessions/"+url.PathEscape(id)+"/revoke", c.Token, nil, nil, nil)
}
