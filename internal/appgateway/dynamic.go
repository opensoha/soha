package appgateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const (
	dynamicRecheckInterval = 2 * time.Second
	dynamicRecheckTimeout  = 1 * time.Second
	visitorEmailHeaderCap  = 320
	visitorDeptHeaderCap   = 256
)

var (
	runtimeRevisionPattern = regexp.MustCompile(`^rev_[0-9a-f]{32}$`)
	runtimeServicePattern  = regexp.MustCompile(`^soha-app-[0-9a-f]{32}$`)
)

func newRuntimeTransport() *http.Transport {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	}
	cloned := transport.Clone()
	cloned.Proxy = nil
	return cloned
}

func runtimeTarget(decision sohaapi.HostedAppGatewayAccessDecision, namespace, clusterDomain string) (*url.URL, error) {
	if namespace == "" {
		return nil, errors.New("dynamic runtime namespace is not configured")
	}
	if !runtimeRevisionPattern.MatchString(decision.ActiveRevisionID) {
		return nil, errors.New("active revision ID is not routable")
	}
	service := "soha-app-" + strings.TrimPrefix(decision.ActiveRevisionID, "rev_")
	if decision.RuntimeService != service || !runtimeServicePattern.MatchString(decision.RuntimeService) {
		return nil, errors.New("runtime service does not match the active revision")
	}
	return &url.URL{
		Scheme: "http",
		Host:   service + "." + namespace + ".svc." + clusterDomain,
	}, nil
}

func (s *Server) serveDynamicApp(w http.ResponseWriter, r *http.Request, host validatedHost, cookies []*http.Cookie, session *sessionClaims, decision sohaapi.HostedAppGatewayAccessDecision) {
	if decision.AppStatus == sohaapi.HostedAppStatusOffline {
		writePlatformStatus(w, r, http.StatusServiceUnavailable, "APP_OFFLINE", "应用已下线或暂停访问", "当前应用已下线或暂停访问。")
		return
	}
	if decision.AppStatus == sohaapi.HostedAppStatusSuspendedByAdmin {
		writePlatformStatus(w, r, http.StatusForbidden, "FORBIDDEN", "无权访问该应用", "当前应用已被管理员暂停访问。")
		return
	}
	if !exactPublicOriginAllowed(r, s.cfg.PublicScheme, host) {
		writePlatformStatus(w, r, http.StatusForbidden, "FORBIDDEN", "无权访问该应用", "跨站请求已被拒绝。")
		return
	}
	if decision.User == nil || strings.TrimSpace(decision.User.ID) == "" || strings.TrimSpace(decision.User.DisplayName) == "" || containsControlChar(decision.User.ID) || containsControlChar(decision.User.DisplayName) {
		writeDynamicUnavailable(w, r)
		return
	}
	target, err := runtimeTarget(decision, s.cfg.RuntimeNamespace, s.cfg.RuntimeClusterDomain)
	if err != nil || s.runtimeTransport == nil {
		writeDynamicUnavailable(w, r)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchDynamicAccess(ctx, cancel, host, session, decision)
	}()
	defer func() {
		cancel()
		<-done
	}()

	proxy := s.newDynamicProxy(target, host, decision, cookies)
	proxy.ServeHTTP(w, r.WithContext(ctx))
}

func writeDynamicUnavailable(w http.ResponseWriter, r *http.Request) {
	writePlatformStatus(w, r, http.StatusServiceUnavailable, "GATEWAY_UNAVAILABLE", "托管网关服务暂不可用", "动态应用暂时不可用，请稍后重试。")
}

func (s *Server) newDynamicProxy(target *url.URL, host validatedHost, decision sohaapi.HostedAppGatewayAccessDecision, cookies []*http.Cookie) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: s.runtimeTransport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL.Scheme = target.Scheme
			out.URL.Host = target.Host
			out.URL.User = nil
			out.URL.Opaque = ""
			out.URL.Fragment = ""
			out.Host = target.Host
			out.Header = dynamicUpstreamHeaders(pr.Out.Header, cookies)
			out.Header.Set("X-Soha-User-ID", decision.User.ID)
			out.Header.Set("X-Soha-User", decision.User.DisplayName)
			if value := forwardedVisitorHeaderValue(decision.User.Email, visitorEmailHeaderCap); value != "" {
				out.Header.Set("X-Soha-User-Email", value)
			}
			if value := forwardedVisitorHeaderValue(decision.User.Department, visitorDeptHeaderCap); value != "" {
				out.Header.Set("X-Soha-User-Department", value)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			filterDynamicResponse(resp)
			rawLocation := resp.Header.Get("Location")
			if rawLocation != "" {
				location, err := url.Parse(rawLocation)
				if err == nil && location.User == nil && strings.EqualFold(location.Host, target.Host) {
					location.Scheme = s.cfg.PublicScheme
					location.Host = host.Authority
					resp.Header.Set("Location", location.String())
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeDynamicUnavailable(w, r)
		},
	}
}

// forwardedVisitorHeaderValue returns "" for values that must not be forwarded.
func forwardedVisitorHeaderValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

func dynamicUpstreamHeaders(in http.Header, cookies []*http.Cookie) http.Header {
	out := SanitizeUpstreamHeaders(in)
	for _, name := range []string{"Authorizer", "X-Authorizer", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port", "X-Forwarded-Prefix", "X-Real-IP"} {
		out.Del(name)
	}
	out.Del("Cookie")
	upstream := http.Request{Header: out}
	for _, cookie := range cookies {
		if cookie == nil || isReservedCookieName(cookie.Name) {
			continue
		}
		upstream.AddCookie(cookie)
	}
	return out
}

func filterDynamicResponse(resp *http.Response) {
	resp.Header.Set("Cache-Control", "private, no-store")
	rawCookies := resp.Header.Values("Set-Cookie")
	if len(rawCookies) == 0 {
		return
	}
	resp.Header.Del("Set-Cookie")
	for _, raw := range rawCookies {
		cookie, err := http.ParseSetCookie(raw)
		if err != nil || cookie == nil || isReservedCookieName(cookie.Name) {
			continue
		}
		cookie.Domain = ""
		resp.Header.Add("Set-Cookie", cookie.String())
	}
}

func isReservedCookieName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "soha_app_session", "_soha_app_session", "__host-soha_app_session", "__secure-soha_app_session",
		"soha_app_oauth_state", "_soha_app_oauth_state", "__host-soha_app_oauth_state", "__secure-soha_app_oauth_state",
		"soha_refresh_token", "soha_protocol_access_token", "soha_proxy_session":
		return true
	}
	for _, prefix := range []string{"soha_", "_soha_", "__host-soha_", "__secure-soha_"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func exactPublicOriginAllowed(r *http.Request, scheme string, host validatedHost) bool {
	if r == nil || !requiresExactPublicOrigin(r) {
		return true
	}
	originValues := r.Header.Values("Origin")
	if len(originValues) != 1 {
		return false
	}
	return originMatchesExpected(originValues[0], scheme, host)
}

func requiresExactPublicOrigin(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return true
	}
	return strings.TrimSpace(r.Header.Get("Upgrade")) != ""
}

func (s *Server) watchDynamicAccess(ctx context.Context, cancel context.CancelFunc, host validatedHost, session *sessionClaims, initial sohaapi.HostedAppGatewayAccessDecision) {
	ticker := time.NewTicker(dynamicRecheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		checkCtx, checkCancel := context.WithTimeout(ctx, dynamicRecheckTimeout)
		decision, err := s.evaluateAccess(checkCtx, host, session)
		checkCancel()
		if err != nil || decision.Decision != sohaapi.HostedAppAccessDecisionKindAllow ||
			decision.AppStatus != initial.AppStatus ||
			decision.RuntimeService != initial.RuntimeService ||
			decision.ActiveRevisionID != initial.ActiveRevisionID ||
			decision.User == nil || decision.User.ID != initial.User.ID ||
			forwardedVisitorHeaderValue(decision.User.Email, visitorEmailHeaderCap) != forwardedVisitorHeaderValue(initial.User.Email, visitorEmailHeaderCap) ||
			forwardedVisitorHeaderValue(decision.User.Department, visitorDeptHeaderCap) != forwardedVisitorHeaderValue(initial.User.Department, visitorDeptHeaderCap) {
			cancel()
			return
		}
	}
}

func validateRuntimeNamespace(raw string) (string, error) {
	namespace := strings.TrimSpace(raw)
	if namespace == "" {
		return "", nil
	}
	if !isValidDNSLabel(namespace) {
		return "", fmt.Errorf("invalid runtime namespace %q", raw)
	}
	return namespace, nil
}

func validateRuntimeClusterDomain(raw string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" {
		return "", nil
	}
	if len(domain) > 253 || strings.ContainsAny(domain, ":/\\?#@ \t\r\n*") {
		return "", fmt.Errorf("invalid runtime cluster domain %q", raw)
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("invalid runtime cluster domain %q", raw)
	}
	for _, label := range labels {
		if !isValidDNSLabel(label) {
			return "", fmt.Errorf("invalid runtime cluster domain %q", raw)
		}
	}
	return domain, nil
}
