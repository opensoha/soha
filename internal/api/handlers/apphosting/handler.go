package apphosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	"github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/api/response"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const maxControlJSONBodyBytes = 256 << 10

type SettingsService interface {
	GetSettings(ctx context.Context, principal domainidentity.Principal) (sohaapi.AppHostingSettings, error)
	UpdateSettings(ctx context.Context, principal domainidentity.Principal, req sohaapi.AppHostingSettingsUpdateRequest) (sohaapi.AppHostingSettings, error)
	RotateGatewayToken(ctx context.Context, principal domainidentity.Principal, reason string) (sohaapi.AppHostingGatewayTokenSecret, error)
}

type AppService interface {
	ListAudienceCandidates(ctx context.Context, principal domainidentity.Principal, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error)
	ListApps(ctx context.Context, principal domainidentity.Principal, filter domainapphosting.AppListFilter) (sohaapi.HostedAppList, error)
	CreateApp(ctx context.Context, principal domainidentity.Principal, req sohaapi.CreateHostedAppRequest) (sohaapi.HostedApp, bool, error)
	GetApp(ctx context.Context, principal domainidentity.Principal, appID string) (sohaapi.HostedApp, error)
	UpdateApp(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppRequest) (sohaapi.HostedApp, error)
	UpdateVisibility(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppVisibilityRequest) (sohaapi.HostedApp, error)
}

type LifecycleService interface {
	TakeOffline(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error)
	BringOnline(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error)
	SuspendApp(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error)
	UnsuspendApp(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error)
}

type DeploymentService interface {
	Preflight(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.HostedAppPreflightRequest) (sohaapi.HostedAppPreflightResult, error)
	UploadRevision(ctx context.Context, principal domainidentity.Principal, appID string, input domainapphosting.UploadRevisionInput, bundle io.Reader) (sohaapi.HostedAppDeploymentReceipt, error)
	ListRevisions(ctx context.Context, principal domainidentity.Principal, appID string, limit, offset int) (sohaapi.HostedAppRevisionList, error)
	GetRevision(ctx context.Context, principal domainidentity.Principal, appID, revisionID string) (sohaapi.HostedAppRevision, error)
	Rollback(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.HostedAppRollbackRequest) (sohaapi.HostedAppDeploymentReceipt, error)
}

type GatewayAuthService interface {
	StartBrowserAuth(ctx context.Context, principal domainidentity.Principal, input domainapphosting.BrowserAuthStartInput) (domainapphosting.BrowserAuthStartResult, error)
	ExchangeGatewayTicket(ctx context.Context, gatewayBearerToken string, req sohaapi.HostedAppTicketExchangeRequest) (sohaapi.HostedAppTicketExchangeResult, error)
	CheckGatewayAccess(ctx context.Context, gatewayBearerToken string, req sohaapi.HostedAppGatewayAccessCheckRequest) (sohaapi.HostedAppGatewayAccessDecision, error)
}

type AccessTokenParser interface {
	ParseAccessToken(ctx context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error)
}

type Capabilities struct {
	Settings    SettingsService
	Apps        AppService
	Lifecycle   LifecycleService
	Deployments DeploymentService
	GatewayAuth GatewayAuthService
}

type Option func(*Handler)

func WithAccessTokenParser(parser AccessTokenParser) Option {
	return func(h *Handler) {
		if !isTypedNil(parser) {
			h.tokenParser = parser
		}
	}
}

type Handler struct {
	settings    SettingsService
	apps        AppService
	lifecycle   LifecycleService
	deployments DeploymentService
	gatewayAuth GatewayAuthService
	tokenParser AccessTokenParser
}

func New[T interface {
	SettingsService
	AppService
	LifecycleService
	DeploymentService
	GatewayAuthService
}](service T, opts ...Option) *Handler {
	if isTypedNil(service) {
		return NewFromCapabilities(Capabilities{}, opts...)
	}
	return NewFromCapabilities(Capabilities{
		Settings:    service,
		Apps:        service,
		Lifecycle:   service,
		Deployments: service,
		GatewayAuth: service,
	}, opts...)
}

func NewFromCapabilities(caps Capabilities, opts ...Option) *Handler {
	h := &Handler{
		settings:    cleanNil(caps.Settings),
		apps:        cleanNil(caps.Apps),
		lifecycle:   cleanNil(caps.Lifecycle),
		deployments: cleanNil(caps.Deployments),
		gatewayAuth: cleanNil(caps.GatewayAuth),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

func (h *Handler) GetSettings(c *gin.Context) {
	if h.settings == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	out, err := h.settings.GetSettings(c.Request.Context(), middleware.PrincipalFromContext(c))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	if h.settings == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.AppHostingSettingsUpdateRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.settings.UpdateSettings(c.Request.Context(), middleware.PrincipalFromContext(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) RotateGatewayToken(c *gin.Context) {
	if h.settings == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.AppHostingGatewayTokenRotateRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}
	c.Header("Cache-Control", "no-store")
	out, err := h.settings.RotateGatewayToken(c.Request.Context(), middleware.PrincipalFromContext(c), req.Reason)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) ListAudienceCandidates(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	limit, ok := parseOptionalIntQuery(c, "limit")
	if !ok {
		return
	}
	out, err := h.apps.ListAudienceCandidates(c.Request.Context(), middleware.PrincipalFromContext(c), c.Query("query"), limit)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) ListApps(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	limit, ok := parseOptionalIntQuery(c, "limit")
	if !ok {
		return
	}
	offset, ok := parseOptionalIntQuery(c, "offset")
	if !ok {
		return
	}
	filter := domainapphosting.AppListFilter{
		Scope:  sohaapi.HostedAppListScope(strings.TrimSpace(c.Query("scope"))),
		Query:  c.Query("query"),
		Status: sohaapi.HostedAppStatus(strings.TrimSpace(c.Query("status"))),
		Limit:  limit,
		Offset: offset,
	}
	out, err := h.apps.ListApps(c.Request.Context(), middleware.PrincipalFromContext(c), filter)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) CreateApp(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.CreateHostedAppRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, reused, err := h.apps.CreateApp(c.Request.Context(), middleware.PrincipalFromContext(c), req)
	status := http.StatusCreated
	if reused {
		status = http.StatusOK
	}
	respondItem(c, status, out, err)
}

func (h *Handler) GetApp(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	out, err := h.apps.GetApp(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UpdateApp(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.UpdateHostedAppRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.apps.UpdateApp(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UpdateVisibility(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.UpdateHostedAppVisibilityRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.apps.UpdateVisibility(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) TakeOffline(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppLifecycleActionRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}
	out, err := h.lifecycle.TakeOffline(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req.Reason)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) BringOnline(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppLifecycleActionRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}
	out, err := h.lifecycle.BringOnline(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req.Reason)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) SuspendApp(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppSuspendRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.lifecycle.SuspendApp(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req.Reason)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UnsuspendApp(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppLifecycleActionRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}
	out, err := h.lifecycle.UnsuspendApp(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req.Reason)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) PreflightDeployment(c *gin.Context) {
	if h.deployments == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppPreflightRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.deployments.Preflight(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) ListRevisions(c *gin.Context) {
	if h.deployments == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	limit, ok := parseOptionalIntQuery(c, "limit")
	if !ok {
		return
	}
	offset, ok := parseOptionalIntQuery(c, "offset")
	if !ok {
		return
	}
	out, err := h.deployments.ListRevisions(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), limit, offset)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UploadRevision(c *gin.Context) {
	if h.deployments == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	if !validGzipContentType(c.GetHeader("Content-Type")) {
		fail(c, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_content_type", "Content-Type must be application/gzip", "请求 Content-Type 必须为 application/gzip"))
		return
	}
	spaFallback, ok := parseOptionalBoolQuery(c, "spaFallback")
	if !ok {
		return
	}
	input := domainapphosting.UploadRevisionInput{
		IdempotencyKey: c.Query("idempotencyKey"),
		SpaFallback:    spaFallback,
		Description:    c.Query("description"),
	}
	out, err := h.deployments.UploadRevision(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), input, c.Request.Body)
	status := http.StatusCreated
	if out.ReusedIdempotencyKey {
		status = http.StatusOK
	}
	respondItem(c, status, out, err)
}

func (h *Handler) GetRevision(c *gin.Context) {
	if h.deployments == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	out, err := h.deployments.GetRevision(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), revisionIDParam(c))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) Rollback(c *gin.Context) {
	if h.deployments == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppRollbackRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.deployments.Rollback(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) StartBrowserAuth(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if h.gatewayAuth == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	principal, sourceSessionID, err := h.resolveBrowserPrincipal(c)
	if err != nil {
		fail(c, err)
		return
	}
	out, err := h.gatewayAuth.StartBrowserAuth(c.Request.Context(), principal, domainapphosting.BrowserAuthStartInput{
		Host:            c.Query("host"),
		ReturnPath:      c.Query("returnPath"),
		GatewayState:    c.Query("gatewayState"),
		SourceSessionID: sourceSessionID,
	})
	if err != nil {
		fail(c, err)
		return
	}
	if out.LoginRequired {
		c.Redirect(http.StatusFound, "/login?return_to="+url.QueryEscape(c.Request.URL.RequestURI()))
		return
	}
	c.Redirect(http.StatusFound, out.RedirectURL)
}

func (h *Handler) ExchangeGatewayTicket(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.gatewayAuth == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	token, ok := extractSingleBearerToken(c)
	if !ok {
		return
	}
	var req sohaapi.HostedAppTicketExchangeRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.gatewayAuth.ExchangeGatewayTicket(c.Request.Context(), token, req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) CheckGatewayAccess(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.gatewayAuth == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	token, ok := extractSingleBearerToken(c)
	if !ok {
		return
	}
	var req sohaapi.HostedAppGatewayAccessCheckRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.gatewayAuth.CheckGatewayAccess(c.Request.Context(), token, req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) resolveBrowserPrincipal(c *gin.Context) (domainidentity.Principal, string, error) {
	principal := middleware.PrincipalFromContext(c)
	accessCtx := middleware.AccessContextFromContext(c)
	if strings.TrimSpace(principal.UserID) != "" {
		return validateBrowserAccessContext(principal, accessCtx)
	}
	if h.tokenParser == nil {
		return domainidentity.Principal{}, "", nil
	}
	rawCookie, err := c.Cookie(middleware.ProtocolAccessCookieName)
	if err != nil || strings.TrimSpace(rawCookie) == "" {
		return domainidentity.Principal{}, "", nil
	}
	parsed, parsedCtx, err := h.tokenParser.ParseAccessToken(c.Request.Context(), strings.TrimSpace(rawCookie))
	if err != nil || strings.TrimSpace(parsed.UserID) == "" {
		return domainidentity.Principal{}, "", nil
	}
	return validateBrowserAccessContext(parsed, parsedCtx)
}

func validateBrowserAccessContext(principal domainidentity.Principal, accessCtx domainidentity.AccessContext) (domainidentity.Principal, string, error) {
	sessionID := strings.TrimSpace(accessCtx.SessionID)
	if accessCtx.TokenKind != "session_access" || sessionID == "" {
		return domainidentity.Principal{}, "", apperrors.NewBusiness(
			apperrors.ErrAccessDenied,
			"browser_session_required",
			"interactive browser session is required for hosted application SSO",
			"访问托管应用需要使用交互式浏览器登录会话",
		)
	}
	return principal, sessionID, nil
}

func extractSingleBearerToken(c *gin.Context) (string, bool) {
	header, valid := middleware.SingleAuthorizationHeader(c.Request)
	if !valid || len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") {
		fail(c, apperrors.ErrUnauthorized)
		return "", false
	}
	token := strings.TrimSpace(header[7:])
	if token == "" {
		fail(c, apperrors.ErrUnauthorized)
		return "", false
	}
	return token, true
}

func decodeStrictJSON(c *gin.Context, dst any, optional bool) bool {
	if c.Request == nil || c.Request.Body == nil {
		if optional {
			return true
		}
		fail(c, apperrors.ErrInvalidArgument)
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxControlJSONBodyBytes))
	if err != nil {
		fail(c, apperrors.ErrInvalidArgument)
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if optional {
			return true
		}
		fail(c, apperrors.ErrInvalidArgument)
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(c, apperrors.ErrInvalidArgument)
		return false
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		fail(c, apperrors.ErrInvalidArgument)
		return false
	}
	return true
}

func parseOptionalIntQuery(c *gin.Context, key string) (int, bool) {
	raw, exists := c.GetQuery(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return 0, true
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		fail(c, apperrors.ErrInvalidArgument)
		return 0, false
	}
	return value, true
}

func parseOptionalBoolQuery(c *gin.Context, key string) (*bool, bool) {
	raw, exists := c.GetQuery(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return nil, true
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		fail(c, apperrors.ErrInvalidArgument)
		return nil, false
	}
	return &value, true
}

func validGzipContentType(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "application/gzip", "application/x-gzip", "application/octet-stream":
		return true
	default:
		return false
	}
}

func appIDParam(c *gin.Context) string {
	if v := c.Param("appId"); v != "" {
		return v
	}
	return c.Param("appID")
}

func revisionIDParam(c *gin.Context) string {
	if v := c.Param("revisionId"); v != "" {
		return v
	}
	return c.Param("revisionID")
}

func respondItem(c *gin.Context, status int, data any, err error) {
	c.Header("Cache-Control", "no-store")
	if err != nil {
		fail(c, err)
		return
	}
	response.Item(c, status, data)
}

func fail(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	status, code := mapStatusAndCode(err)
	response.Error(c, status, code, apierrors.Message(err, apierrors.RequestLanguage(c.Request)))
}

func mapStatusAndCode(err error) (int, string) {
	switch {
	case errors.Is(err, domainapphosting.ErrPayloadTooLarge):
		return http.StatusRequestEntityTooLarge, nonDefaultCode(err, "bundle_too_large")
	case errors.Is(err, domainapphosting.ErrBundleRejected):
		return http.StatusUnprocessableEntity, nonDefaultCode(err, "bundle_validation_failed")
	case errors.Is(err, domainapphosting.ErrQuotaExceeded):
		return http.StatusTooManyRequests, nonDefaultCode(err, "quota_exceeded")
	default:
		return apierrors.StatusCode(err), apierrors.Code(err)
	}
}

func nonDefaultCode(err error, fallback string) string {
	var biz *apperrors.BusinessError
	if errors.As(err, &biz) && biz.Code() != "" {
		return biz.Code()
	}
	return fallback
}

func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

func cleanNil[T any](value T) T {
	if isTypedNil(value) {
		var zero T
		return zero
	}
	return value
}
