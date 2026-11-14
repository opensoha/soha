package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

type websocketOriginPolicy struct {
	allowedOrigins map[string]struct{}
	authTokens     []string
}

func newWebSocketOriginPolicy(allowedOrigins []string, authTokens ...string) websocketOriginPolicy {
	return websocketOriginPolicy{
		allowedOrigins: normalizeWebSocketAllowedOrigins(allowedOrigins),
		authTokens:     allowedAuthTokens(authTokens...),
	}
}

func (p websocketOriginPolicy) Check(r *http.Request) bool {
	if r == nil {
		return false
	}
	if len(r.Header.Values("Origin")) > 1 {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return requestHasAnyBearerToken(r, p.authTokens) || requestHasVerifiedClientCertificate(r)
	}
	parsed, ok := parseWebSocketOrigin(origin)
	if !ok {
		return false
	}
	if _, ok := p.allowedOrigins[webSocketOriginKey(parsed)]; ok {
		return true
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	requestHost := webSocketHostName(r.Host)
	originHost := webSocketHostName(parsed.Host)
	return isLocalWebSocketHost(requestHost) && isLocalWebSocketHost(originHost)
}

func normalizeWebSocketAllowedOrigins(values []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		parsed, ok := parseWebSocketOrigin(strings.TrimSpace(value))
		if !ok {
			continue
		}
		allowed[webSocketOriginKey(parsed)] = struct{}{}
	}
	return allowed
}

func parseWebSocketOrigin(value string) (*url.URL, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, false
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, false
	}
	if parsed.Hostname() == "" {
		return nil, false
	}
	return parsed, true
}

func webSocketOriginKey(parsed *url.URL) string {
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func webSocketHostName(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func isLocalWebSocketHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

func requestHasVerifiedClientCertificate(r *http.Request) bool {
	return r.TLS != nil && len(r.TLS.PeerCertificates) > 0 && len(r.TLS.VerifiedChains) > 0
}
