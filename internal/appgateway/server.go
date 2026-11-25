package appgateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const (
	defaultCoreTimeout = 10 * time.Second
	readinessTimeout   = 3 * time.Second
	shutdownTimeout    = 10 * time.Second
	maxStaticFileBytes = 512 << 20
)

// CoreClient defines the Soha Core control-plane gateway protocol consumed by app-gateway.
type CoreClient interface {
	ExchangeHostedAppGatewayTicket(ctx context.Context, req sohaapi.HostedAppTicketExchangeRequest, gatewayToken ...string) (sohaapi.HostedAppTicketExchangeResult, error)
	CheckHostedAppGatewayAccess(ctx context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, gatewayToken ...string) (sohaapi.HostedAppGatewayAccessDecision, error)
	GetReadyz(ctx context.Context) (sohaapi.GenericObject, error)
}

// Option customizes optional dependencies on a Server instance.
type Option func(*serverOptions)

type serverOptions struct {
	coreClient   CoreClient
	objectReader ObjectReader
	now          func() time.Time
}

// WithCoreClient overrides the Soha Core client used for ticket exchange, access checks, and readiness.
func WithCoreClient(client CoreClient) Option {
	return func(o *serverOptions) {
		o.coreClient = client
	}
}

// WithObjectReader overrides the S3/MinIO object reader used to stream static files.
func WithObjectReader(reader ObjectReader) Option {
	return func(o *serverOptions) {
		o.objectReader = reader
	}
}

// WithClock overrides the clock used for cookie expiration and decision cache TTLs.
func WithClock(now func() time.Time) Option {
	return func(o *serverOptions) {
		o.now = now
	}
}

// Server serves hosted workspace applications with unified SSO, live access decisions, and S3 static streaming.
type Server struct {
	cfg              Config
	secrets          *secretProvider
	core             CoreClient
	objects          ObjectReader
	cache            *decisionCache
	now              func() time.Time
	requireS3        bool
	runtimeTransport http.RoundTripper
	draining         atomic.Bool
}

// NewServer validates configuration and builds a ready-to-serve Workspace App Hosting gateway.
func NewServer(cfg Config, opts ...Option) (*Server, error) {
	var options serverOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	requireS3 := options.objectReader == nil
	if err := cfg.NormalizeAndValidate(requireS3); err != nil {
		return nil, err
	}
	nowFn := options.now
	if nowFn == nil {
		nowFn = time.Now
	}
	secrets := newSecretProvider(cfg)
	coreClient := options.coreClient
	if coreClient == nil {
		coreClient = sohaapi.NewClient(
			cfg.CoreURL,
			sohaapi.WithHTTPClient(&http.Client{Timeout: defaultCoreTimeout}),
			sohaapi.WithUserAgent("soha-app-gateway/0.2.0"),
		)
	}
	objectReader := options.objectReader
	if objectReader == nil {
		s3Reader, err := newS3ObjectReaderWithSecrets(cfg, secrets)
		if err != nil {
			return nil, err
		}
		objectReader = s3Reader
	}
	return &Server{
		cfg:              cfg,
		secrets:          secrets,
		core:             coreClient,
		objects:          objectReader,
		cache:            newDecisionCache(cfg.DecisionCacheTTL, nowFn),
		now:              nowFn,
		requireS3:        requireS3,
		runtimeTransport: newRuntimeTransport(),
	}, nil
}

// Handler returns the main HTTP handler for hosted application traffic.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.ServeHTTP)
}

// RedirectHandler returns the dedicated HTTP -> HTTPS 308 Permanent Redirect handler.
func (s *Server) RedirectHandler() http.Handler {
	return http.HandlerFunc(s.serveHTTPRedirect)
}

// HealthHandler returns the probe handler serving GET /healthz and GET /readyz.
func (s *Server) HealthHandler() http.Handler {
	return http.HandlerFunc(s.serveHealth)
}

// InvalidateSession evicts cached access decisions for a session on the specified host (or all hosts when host is empty).
func (s *Server) InvalidateSession(host, sessionID string) {
	if s == nil {
		return
	}
	s.cache.invalidateSession(strings.ToLower(strings.TrimSpace(host)), strings.TrimSpace(sessionID))
}

// ClearDecisionCache removes all cached access decisions.
func (s *Server) ClearDecisionCache() {
	if s == nil {
		return
	}
	s.cache.clear()
}

// Run starts the health listener, optional HTTP redirect listener, and main listener until ctx is canceled.
func (s *Server) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("gateway server is nil")
	}
	s.draining.Store(false)
	servers := make([]*http.Server, 0, 3)
	errCh := make(chan error, 3)

	if s.cfg.HealthAddr != "" {
		srv, err := startHTTPListener(s.cfg.HealthAddr, s.HealthHandler(), nil, errCh)
		if err != nil {
			return fmt.Errorf("listen health %s: %w", s.cfg.HealthAddr, err)
		}
		servers = append(servers, srv)
	}
	if s.cfg.HTTPRedirectAddr != "" {
		srv, err := startHTTPListener(s.cfg.HTTPRedirectAddr, s.RedirectHandler(), nil, errCh)
		if err != nil {
			shutdownAll(servers)
			return fmt.Errorf("listen HTTP redirect %s: %w", s.cfg.HTTPRedirectAddr, err)
		}
		servers = append(servers, srv)
	}
	tlsCfg, err := s.buildListenerTLSConfig()
	if err != nil {
		shutdownAll(servers)
		return err
	}
	mainSrv, err := startHTTPListener(s.cfg.ListenAddr, s.Handler(), tlsCfg, errCh)
	if err != nil {
		shutdownAll(servers)
		return fmt.Errorf("listen main %s: %w", s.cfg.ListenAddr, err)
	}
	servers = append(servers, mainSrv)

	select {
	case <-ctx.Done():
		s.draining.Store(true)
		shutdownAll(servers)
		return nil
	case serveErr := <-errCh:
		s.draining.Store(true)
		shutdownAll(servers)
		return serveErr
	}
}

func (s *Server) buildListenerTLSConfig() (*tls.Config, error) {
	if s.cfg.TLSCertFile == "" || s.cfg.TLSKeyFile == "" {
		return nil, nil
	}
	if _, err := s.secrets.LoadTLSCertificate(); err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return s.secrets.LoadTLSCertificate()
		},
	}, nil
}

func startHTTPListener(addr string, handler http.Handler, tlsCfg *tls.Config, errCh chan<- error) (*http.Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if tlsCfg != nil {
		ln = tls.NewListener(ln, tlsCfg)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()
	return srv, nil
}

func shutdownAll(servers []*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for i := len(servers) - 1; i >= 0; i-- {
		if servers[i] != nil {
			_ = servers[i].Shutdown(ctx)
		}
	}
}

// ServeHTTP handles incoming hosted application requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, ok := parseRequestHost(r.Host, s.cfg.AppsDomain)
	if !ok {
		writePlatformStatus(w, r, http.StatusNotFound, "HOST_NOT_FOUND", "未找到请求的应用", "访问的域名无效或未分配托管应用。")
		return
	}
	if s.cfg.PublicScheme == "https" && !isHTTPSRequest(r, s.cfg.trustedProxyPrefixes) {
		w.Header().Set("Cache-Control", "private, no-store")
		//nolint:gosec // G710: host is strictly validated against <dnsLabel>.<appsDomain> and request URI is sanitized
		http.Redirect(w, r, httpsRedirectTarget(host, r, false), http.StatusPermanentRedirect)
		return
	}
	cookies := r.Cookies()
	sanitizedReq := r.Clone(r.Context())
	sanitizedReq.Header = SanitizeUpstreamHeaders(r.Header)

	if isReservedSystemPath(sanitizedReq.URL.Path) {
		s.handleSystemRoute(w, sanitizedReq, host, cookies)
		return
	}
	s.handleAppRequest(w, sanitizedReq, host, cookies)
}

func (s *Server) serveHTTPRedirect(w http.ResponseWriter, r *http.Request) {
	host, ok := parseRequestHost(r.Host, s.cfg.AppsDomain)
	if !ok {
		writePlatformStatus(w, r, http.StatusNotFound, "HOST_NOT_FOUND", "未找到请求的应用", "访问的域名无效或未分配托管应用。")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	//nolint:gosec // G710: host is strictly validated against <dnsLabel>.<appsDomain> and request URI is sanitized
	http.Redirect(w, r, httpsRedirectTarget(host, r, true), http.StatusPermanentRedirect)
}

func (s *Server) serveHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	case "/readyz":
		if !s.checkReady(r.Context()) {
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"status":"not_ready"}`))
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Server) checkReady(ctx context.Context) bool {
	if s.draining.Load() {
		return false
	}
	if err := s.secrets.ValidateAll(s.requireS3); err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()
	_, err := s.core.GetReadyz(probeCtx)
	return err == nil
}

func isReservedSystemPath(rawPath string) bool {
	if rawPath == "/_soha" || strings.HasPrefix(rawPath, "/_soha/") {
		return true
	}
	cleaned := path.Clean("/" + rawPath)
	return cleaned == "/_soha" || strings.HasPrefix(cleaned, "/_soha/")
}

func (s *Server) handleSystemRoute(w http.ResponseWriter, r *http.Request, host validatedHost, cookies []*http.Cookie) {
	switch r.URL.Path {
	case "/_soha/auth/callback":
		s.handleAuthCallback(w, r, host, cookies)
	case "/_soha/logout":
		s.handleLogout(w, r, host, cookies)
	case "/_soha/me":
		s.handleMe(w, r, host, cookies)
	default:
		writePlatformStatus(w, r, http.StatusNotFound, "NOT_FOUND", "未找到系统路径", "请求的系统路径不存在。")
	}
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request, host validatedHost, cookies []*http.Cookie) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writePlatformStatus(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不支持", "登录回调仅支持 GET 请求。")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	stateParam := strings.TrimSpace(r.URL.Query().Get("state"))
	secureCookie := s.isSecureCookie()
	stateCookieKey := oauthStateCookieName(secureCookie)
	stateCookie, present, ambiguous := extractSingleCookie(cookies, stateCookieKey)
	http.SetCookie(w, clearHostOnlyCookie(stateCookieKey, secureCookie))
	if ticket == "" || stateParam == "" || !present || ambiguous {
		writePlatformStatus(w, r, http.StatusBadRequest, "INVALID_CALLBACK", "登录验证失败", "登录状态缺失或已失效，请重新访问应用。")
		return
	}
	cookieSecret, gatewayToken, ok := s.loadAuthSecrets(w, r)
	if !ok {
		return
	}
	now := s.now()
	stateClaims, err := verifyOAuthState(cookieSecret, host.Hostname, stateCookie, stateParam, now)
	if err != nil {
		writePlatformStatus(w, r, http.StatusForbidden, "INVALID_OAUTH_STATE", "登录状态校验未通过", "浏览器登录状态已过期或不匹配，请重新打开应用链接。")
		return
	}
	result, err := s.core.ExchangeHostedAppGatewayTicket(r.Context(), sohaapi.HostedAppTicketExchangeRequest{
		Ticket:       ticket,
		Host:         host.Authority,
		GatewayState: stateParam,
	}, gatewayToken)
	if err != nil {
		status := classifyTicketExchangeError(err)
		writeExchangeErrorStatus(w, r, status)
		return
	}
	safeReturn, valid := s.validateExchangeResult(result, host, stateClaims, now)
	if !valid {
		writePlatformStatus(w, r, http.StatusForbidden, "TICKET_HOST_MISMATCH", "登录票据校验未通过", "票据目标域名或会话无效。")
		return
	}
	sessionCookie, err := sealSessionCookie(cookieSecret, host.Hostname, sessionClaims{
		SessionID: strings.TrimSpace(result.SessionID),
		UserID:    strings.TrimSpace(result.PrincipalUserID),
		Host:      host.Hostname,
		ExpiresAt: result.ExpiresAt,
	}, secureCookie, now)
	if err != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "无法建立应用会话，请稍后重试。")
		return
	}
	http.SetCookie(w, sessionCookie)
	http.Redirect(w, r, safeReturn, http.StatusFound)
}

func (s *Server) validateExchangeResult(result sohaapi.HostedAppTicketExchangeResult, host validatedHost, state oauthStateClaims, now time.Time) (string, bool) {
	retHost, retPort, ok := extractNormalizedHostname(result.Host)
	if !ok || retHost != host.Hostname {
		return "", false
	}
	if retPort != "" && host.Port != "" && retPort != host.Port {
		return "", false
	}
	if strings.TrimSpace(result.SessionID) == "" || strings.TrimSpace(result.PrincipalUserID) == "" || strings.TrimSpace(result.User.ID) == "" || !result.ExpiresAt.After(now) {
		return "", false
	}
	safeReturn, ok := validateReturnPath(result.ReturnPath)
	if !ok {
		return "", false
	}
	if state.ReturnPath != "" && safeReturn != state.ReturnPath {
		return "", false
	}
	return safeReturn, true
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, host validatedHost, cookies []*http.Cookie) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST is required")
		return
	}
	if !validateSameOrigin(r, s.cfg.PublicScheme, host, true) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN_ORIGIN", "Cross-origin logout request rejected")
		return
	}
	secureCookie := s.isSecureCookie()
	sessCookieKey := sessionCookieName(secureCookie)
	if rawCookie, present, ambiguous := extractSingleCookie(cookies, sessCookieKey); present && !ambiguous {
		if secret, err := s.secrets.CookieSecret(); err == nil {
			var claims sessionClaims
			if openAEAD(secret, sessionCookieAAD+host.Hostname, rawCookie, &claims) == nil && claims.SessionID != "" {
				s.cache.invalidateSession(host.Hostname, claims.SessionID)
			}
		}
	}
	http.SetCookie(w, clearHostOnlyCookie(sessCookieKey, secureCookie))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, host validatedHost, cookies []*http.Cookie) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET is required")
		return
	}
	if !validateSameOrigin(r, s.cfg.PublicScheme, host, false) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN_ORIGIN", "Cross-origin identity read rejected")
		return
	}
	session, hadCookie, secretErr := s.readVisitorSession(w, host.Hostname, cookies)
	if secretErr != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "Gateway session verifier unavailable")
		return
	}
	req := sohaapi.HostedAppGatewayAccessCheckRequest{Host: host.Authority}
	if session != nil {
		req.SessionID, req.UserID = session.SessionID, session.UserID
	}
	decision, err := s.requestAccessDecision(r.Context(), req)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "Access verification unavailable")
		return
	}
	switch decision.Decision {
	case sohaapi.HostedAppAccessDecisionKindAllow:
		if decision.AppStatus == sohaapi.HostedAppStatusOffline {
			writeJSONError(w, http.StatusServiceUnavailable, "APP_OFFLINE", "应用已下线或暂停访问")
			return
		}
		if decision.AppStatus == sohaapi.HostedAppStatusSuspendedByAdmin || decision.User == nil {
			writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "无权访问该应用")
			return
		}
		writeJSON(w, http.StatusOK, *decision.User)
	case sohaapi.HostedAppAccessDecisionKindUnauthenticated:
		s.clearStaleSession(w, host.Hostname, session, hadCookie)
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
	case sohaapi.HostedAppAccessDecisionKindForbidden:
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "无权访问该应用")
	case sohaapi.HostedAppAccessDecisionKindOffline:
		writeJSONError(w, http.StatusServiceUnavailable, "APP_OFFLINE", "应用已下线或暂停访问")
	case sohaapi.HostedAppAccessDecisionKindNotFound:
		writeJSONError(w, http.StatusNotFound, "APP_NOT_FOUND", "未找到请求的应用")
	default:
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "无权访问该应用")
	}
}

func (s *Server) handleAppRequest(w http.ResponseWriter, r *http.Request, host validatedHost, cookies []*http.Cookie) {
	session, hadCookie, secretErr := s.readVisitorSession(w, host.Hostname, cookies)
	if secretErr != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "网关会话密钥不可用，请稍后重试。")
		return
	}
	decision, err := s.evaluateAccess(r.Context(), host, session)
	if err != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "无法完成实时访问权限校验，请稍后重试。")
		return
	}
	switch decision.Decision {
	case sohaapi.HostedAppAccessDecisionKindNotFound:
		writePlatformStatus(w, r, http.StatusNotFound, "APP_NOT_FOUND", "未找到请求的应用", "该托管应用不存在或尚未发布。")
	case sohaapi.HostedAppAccessDecisionKindOffline:
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "APP_OFFLINE", "应用已下线或暂停访问", "当前应用已下线或暂停访问，请联系应用作者或管理员。")
	case sohaapi.HostedAppAccessDecisionKindUnauthenticated:
		s.clearStaleSession(w, host.Hostname, session, hadCookie)
		s.handleUnauthenticatedAppRequest(w, r, host)
	case sohaapi.HostedAppAccessDecisionKindForbidden:
		writePlatformStatus(w, r, http.StatusForbidden, "FORBIDDEN", "无权访问该应用", "你不在当前应用的授权可见范围内。")
	case sohaapi.HostedAppAccessDecisionKindAllow:
		if decision.RuntimeService != "" {
			s.serveDynamicApp(w, r, host, cookies, session, decision)
			return
		}
		s.serveAuthorizedApp(w, r, decision)
	default:
		writePlatformStatus(w, r, http.StatusForbidden, "FORBIDDEN", "无权访问该应用", "你不在当前应用的授权可见范围内。")
	}
}

func (s *Server) handleUnauthenticatedAppRequest(w http.ResponseWriter, r *http.Request, host validatedHost) {
	if !shouldRedirectToLogin(r) {
		writePlatformStatus(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "需要登录访问", "登录状态已失效，请刷新页面重新登录。")
		return
	}
	cookieSecret, err := s.secrets.CookieSecret()
	if err != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "网关会话密钥不可用，请稍后重试。")
		return
	}
	returnPath := safeRequestURI(r)
	stateCookie, stateToken, err := mintOAuthStateCookie(cookieSecret, host.Hostname, returnPath, s.isSecureCookie(), s.now())
	if err != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "无法生成登录校验状态，请稍后重试。")
		return
	}
	http.SetCookie(w, stateCookie)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, buildAuthStartURL(s.cfg.PublicCoreURL, host.Authority, returnPath, stateToken), http.StatusFound)
}

func (s *Server) serveAuthorizedApp(w http.ResponseWriter, r *http.Request, decision sohaapi.HostedAppGatewayAccessDecision) {
	if decision.AppStatus == sohaapi.HostedAppStatusOffline {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "APP_OFFLINE", "应用已下线或暂停访问", "当前应用已下线或暂停访问。")
		return
	}
	if decision.AppStatus == sohaapi.HostedAppStatusSuspendedByAdmin {
		writePlatformStatus(w, r, http.StatusForbidden, "FORBIDDEN", "无权访问该应用", "当前应用已被管理员暂停访问。")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writePlatformStatus(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不支持", "静态托管应用仅支持 GET 与 HEAD 请求。")
		return
	}
	manifestFile, found := resolveManifestFile(r.URL.Path, decision.Files, decision.SpaFallback)
	if !found {
		writePlatformStatus(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "未找到请求的页面或资源", "请求的资源不在当前激活版本清单中。")
		return
	}
	s.streamManifestFile(w, r, decision, manifestFile)
}

func (s *Server) streamManifestFile(w http.ResponseWriter, r *http.Request, decision sohaapi.HostedAppGatewayAccessDecision, file sohaapi.HostedAppManifestFile) {
	contentType := strings.TrimSpace(file.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	etag := computeETag(file)
	if r.Method == http.MethodGet && ifNoneMatchHit(r.Header.Values("If-None-Match"), etag) {
		setStaticAssetHeaders(w, contentType, etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		setStaticAssetHeaders(w, contentType, etag)
		if ifNoneMatchHit(r.Header.Values("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if file.SizeBytes >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	fullKey, err := resolveObjectKey(s.cfg.S3Prefix, decision.StoragePrefix, file.ObjectKey)
	if err != nil {
		writePlatformStatus(w, r, http.StatusBadGateway, "STORAGE_KEY_INVALID", "应用资源读取失败", "静态对象存储路径无效。")
		return
	}
	stream, err := s.objects.GetObject(r.Context(), fullKey)
	if err != nil || stream == nil || stream.Body == nil {
		if stream != nil && stream.Body != nil {
			_ = stream.Body.Close()
		}
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "应用资源读取失败", "私有对象存储暂时不可用，请稍后重试。")
		return
	}
	defer func() { _ = stream.Body.Close() }()

	setStaticAssetHeaders(w, contentType, etag)
	size := file.SizeBytes
	if stream.ContentLength >= 0 {
		size = stream.ContentLength
	}
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(stream.Body, maxStaticFileBytes))
}

func setStaticAssetHeaders(w http.ResponseWriter, contentType, etag string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, no-cache")
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
}

func (s *Server) evaluateAccess(ctx context.Context, host validatedHost, session *sessionClaims) (sohaapi.HostedAppGatewayAccessDecision, error) {
	req := sohaapi.HostedAppGatewayAccessCheckRequest{Host: host.Authority}
	var cacheKey decisionCacheKey
	var sessionExp time.Time
	if session != nil {
		req.SessionID = session.SessionID
		req.UserID = session.UserID
		sessionExp = session.ExpiresAt
		cacheKey = decisionCacheKey{
			Host:      host.Hostname,
			SessionID: session.SessionID,
			UserID:    session.UserID,
		}
		if cached, hit := s.cache.get(cacheKey); hit {
			return cached, nil
		}
	}
	decision, err := s.requestAccessDecision(ctx, req)
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	}
	if session != nil {
		s.cache.put(cacheKey, decision, sessionExp)
	}
	return decision, nil
}

func (s *Server) requestAccessDecision(ctx context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest) (sohaapi.HostedAppGatewayAccessDecision, error) {
	gatewayToken, err := s.secrets.GatewayToken()
	if err != nil {
		return sohaapi.HostedAppGatewayAccessDecision{}, err
	}
	return s.core.CheckHostedAppGatewayAccess(ctx, req, gatewayToken)
}

func (s *Server) readVisitorSession(w http.ResponseWriter, host string, cookies []*http.Cookie) (*sessionClaims, bool, error) {
	secureCookie := s.isSecureCookie()
	sessCookieKey := sessionCookieName(secureCookie)
	rawCookie, present, ambiguous := extractSingleCookie(cookies, sessCookieKey)
	if !present {
		return nil, false, nil
	}
	if ambiguous {
		http.SetCookie(w, clearHostOnlyCookie(sessCookieKey, secureCookie))
		return nil, false, nil
	}
	secret, err := s.secrets.CookieSecret()
	if err != nil {
		return nil, false, err
	}
	claims, err := openSessionCookie(secret, host, rawCookie, s.now())
	if err != nil {
		http.SetCookie(w, clearHostOnlyCookie(sessCookieKey, secureCookie))
		return nil, false, nil
	}
	return &claims, true, nil
}

func (s *Server) clearStaleSession(w http.ResponseWriter, host string, session *sessionClaims, hadCookie bool) {
	if session != nil {
		s.cache.invalidateSession(host, session.SessionID)
	}
	if hadCookie {
		http.SetCookie(w, clearHostOnlyCookie(sessionCookieName(s.isSecureCookie()), s.isSecureCookie()))
	}
}

func (s *Server) loadAuthSecrets(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	cookieSecret, err := s.secrets.CookieSecret()
	if err != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "网关会话密钥不可用，请稍后重试。")
		return nil, "", false
	}
	gatewayToken, err := s.secrets.GatewayToken()
	if err != nil {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "网关控制面凭据不可用，请稍后重试。")
		return nil, "", false
	}
	return cookieSecret, gatewayToken, true
}

func (s *Server) isSecureCookie() bool {
	return s.cfg.PublicScheme == "https"
}

func buildAuthStartURL(publicCoreURL, host, returnPath, stateToken string) string {
	base := strings.TrimRight(strings.TrimSpace(publicCoreURL), "/")
	base = strings.TrimSuffix(base, "/api/v1")
	query := url.Values{}
	query.Set("host", host)
	query.Set("returnPath", returnPath)
	query.Set("gatewayState", stateToken)
	return base + "/api/v1/app-hosting/auth/start?" + query.Encode()
}

func classifyTicketExchangeError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, ": 400 "):
		return http.StatusBadRequest
	case strings.Contains(msg, ": 403 "):
		return http.StatusForbidden
	case strings.Contains(msg, ": 404 "):
		return http.StatusNotFound
	default:
		return http.StatusServiceUnavailable
	}
}

func writeExchangeErrorStatus(w http.ResponseWriter, r *http.Request, status int) {
	switch status {
	case http.StatusBadRequest:
		writePlatformStatus(w, r, http.StatusBadRequest, "INVALID_TICKET", "登录票据无效", "登录请求参数无效，请重新打开应用。")
	case http.StatusForbidden:
		writePlatformStatus(w, r, http.StatusForbidden, "TICKET_REJECTED", "无权访问该应用", "登录票据已过期、已使用或当前账号无权访问该应用。")
	case http.StatusNotFound:
		writePlatformStatus(w, r, http.StatusNotFound, "APP_NOT_FOUND", "未找到请求的应用", "该托管应用不存在。")
	default:
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "无法完成登录票据校验，请稍后重试。")
	}
}
