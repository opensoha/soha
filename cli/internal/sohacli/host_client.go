package sohacli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type hostClient struct {
	sdk          *sohaapi.Client
	api          APIClient
	accessToken  string
	refreshToken string
}

type hostAuthRoundTripper struct {
	base http.RoundTripper
	api  APIClient
}

type hostRefreshTransportError struct {
	err error
}

func (e *hostRefreshTransportError) Error() string { return e.err.Error() }
func (e *hostRefreshTransportError) Unwrap() error { return e.err }

func loadHostClient(ctx context.Context, rt Runtime, profileName string) (*hostClient, error) {
	_, _, profile, err := loadRuntimeProfile(ctx, rt, profileName)
	if err != nil {
		return nil, err
	}
	apiClient := gatewayClient(rt, profile)
	baseURL := strings.TrimRight(strings.TrimSpace(apiClient.ServerURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if !sameServerURL(baseURL, baseURL) {
		return nil, fmt.Errorf("invalid_server_url: API URL must not contain credentials, query or fragment")
	}
	httpClient := apiClient.httpClient()
	wrappedHTTP := *httpClient
	baseTransport := wrappedHTTP.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	wrappedHTTP.Transport = &hostAuthRoundTripper{
		base: baseTransport,
		api:  apiClient,
	}
	sdk := sohaapi.NewClient(
		baseURL,
		sohaapi.WithBearerToken(apiClient.effectiveToken(apiClient.Token)),
		sohaapi.WithHTTPClient(&wrappedHTTP),
		sohaapi.WithUserAgent(userAgent),
	)
	return &hostClient{
		sdk:          sdk,
		api:          apiClient,
		accessToken:  profile.AccessToken,
		refreshToken: profile.RefreshToken,
	}, nil
}

func (t *hostAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token := t.api.effectiveToken(t.api.Token)
	attemptReq := req.Clone(req.Context())
	if token != "" {
		attemptReq.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := t.base.RoundTrip(attemptReq)
	if err != nil {
		return nil, err
	}
	if !t.shouldRefresh(req, resp, token) {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()

	refreshed, refreshErr := t.api.refreshAfterUnauthorized(req.Context(), token)
	if refreshErr != nil {
		return nil, &hostRefreshTransportError{err: refreshErr}
	}
	retryReq := req.Clone(req.Context())
	if req.Body != nil {
		bodyCopy, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, bodyErr
		}
		retryReq.Body = bodyCopy
	}
	retryReq.Header.Set("Authorization", "Bearer "+refreshed)
	return t.base.RoundTrip(retryReq)
}

func (t *hostAuthRoundTripper) shouldRefresh(req *http.Request, resp *http.Response, token string) bool {
	if resp == nil || resp.StatusCode != http.StatusUnauthorized || token == "" || !t.api.canRefresh() {
		return false
	}
	if req.Body != nil && req.GetBody == nil {
		return false
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return true
	}
	return strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`)
}

func (c *hostClient) wrapErr(err error) error {
	if err == nil {
		return nil
	}
	var refreshErr *hostRefreshTransportError
	if errors.As(err, &refreshErr) {
		return refreshErr.err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := err.Error()
	for _, secret := range []string{c.accessToken, c.refreshToken, c.api.effectiveToken(c.api.Token)} {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "[REDACTED]")
		}
	}
	return errors.New(redactSensitiveText(msg))
}

func (c *hostClient) ListAudienceCandidates(ctx context.Context, params sohaapi.ListAppHostingAudienceCandidatesParams) (sohaapi.AppHostingAudienceCandidates, error) {
	res, err := c.sdk.ListAppHostingAudienceCandidates(ctx, params)
	return res, c.wrapErr(err)
}

func (c *hostClient) ListApps(ctx context.Context, params sohaapi.ListHostedAppsParams) (sohaapi.HostedAppList, error) {
	res, err := c.sdk.ListHostedApps(ctx, params)
	return res, c.wrapErr(err)
}

func (c *hostClient) CreateApp(ctx context.Context, req sohaapi.CreateHostedAppRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.CreateHostedApp(ctx, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) GetApp(ctx context.Context, appID string) (sohaapi.HostedApp, error) {
	res, err := c.sdk.GetHostedApp(ctx, appID)
	return res, c.wrapErr(err)
}

func (c *hostClient) UpdateApp(ctx context.Context, appID string, req sohaapi.UpdateHostedAppRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.UpdateHostedApp(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) UpdateVisibility(ctx context.Context, appID string, req sohaapi.UpdateHostedAppVisibilityRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.UpdateHostedAppVisibility(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) TakeOffline(ctx context.Context, appID string, req *sohaapi.HostedAppLifecycleActionRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.TakeHostedAppOffline(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) BringOnline(ctx context.Context, appID string, req *sohaapi.HostedAppLifecycleActionRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.BringHostedAppOnline(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) PreflightDeployment(ctx context.Context, appID string, req sohaapi.HostedAppPreflightRequest) (sohaapi.HostedAppPreflightResult, error) {
	res, err := c.sdk.PreflightHostedAppDeployment(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) UploadRevision(ctx context.Context, appID string, bundle io.Reader, params sohaapi.UploadHostedAppRevisionParams) (sohaapi.HostedAppDeploymentReceipt, error) {
	res, err := c.sdk.UploadHostedAppRevision(ctx, appID, bundle, params)
	return res, c.wrapErr(err)
}

func (c *hostClient) ListRevisions(ctx context.Context, appID string, params sohaapi.ListHostedAppRevisionsParams) (sohaapi.HostedAppRevisionList, error) {
	res, err := c.sdk.ListHostedAppRevisions(ctx, appID, params)
	return res, c.wrapErr(err)
}

func (c *hostClient) GetRevision(ctx context.Context, appID, revisionID string) (sohaapi.HostedAppRevision, error) {
	res, err := c.sdk.GetHostedAppRevision(ctx, appID, revisionID)
	return res, c.wrapErr(err)
}

func (c *hostClient) Rollback(ctx context.Context, appID string, req sohaapi.HostedAppRollbackRequest) (sohaapi.HostedAppDeploymentReceipt, error) {
	res, err := c.sdk.RollbackHostedApp(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) UpdateMaintainers(ctx context.Context, appID string, req sohaapi.UpdateHostedAppMaintainersRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.UpdateHostedAppMaintainers(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) DeleteApp(ctx context.Context, appID string, req sohaapi.DeleteHostedAppRequest) (sohaapi.DeleteHostedAppResult, error) {
	res, err := c.sdk.DeleteHostedApp(ctx, appID, req)
	return res, c.wrapErr(err)
}

func (c *hostClient) WithdrawVisibilityApproval(ctx context.Context, appID string, req *sohaapi.HostedAppLifecycleActionRequest) (sohaapi.HostedApp, error) {
	res, err := c.sdk.WithdrawHostedAppVisibilityApproval(ctx, appID, req)
	return res, c.wrapErr(err)
}
