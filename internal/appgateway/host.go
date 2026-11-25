package appgateway

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var reservedSubdomainLabels = map[string]struct{}{
	"auth":     {},
	"login":    {},
	"logout":   {},
	"admin":    {},
	"api":      {},
	"portal":   {},
	"static":   {},
	"assets":   {},
	"sso":      {},
	"console":  {},
	"www":      {},
	"mail":     {},
	"status":   {},
	"health":   {},
	"_soha":    {},
	"soha":     {},
	"internal": {},
	"gateway":  {},
}

type validatedHost struct {
	Hostname  string
	Authority string
	DNSLabel  string
	Port      string
}

func parseRequestHost(rawHost, appsDomain string) (validatedHost, bool) {
	trimmed := strings.TrimSpace(rawHost)
	if trimmed == "" || len(trimmed) > 253 || appsDomain == "" || strings.ContainsAny(trimmed, "/\\?#@ \t\r\n") {
		return validatedHost{}, false
	}
	hostname, port, ok := splitHostAndOptionalPort(trimmed)
	if !ok || hostname == "" || len(hostname) > 253 {
		return validatedHost{}, false
	}
	hostname = strings.ToLower(hostname)
	if net.ParseIP(hostname) != nil {
		return validatedHost{}, false
	}
	suffix := "." + appsDomain
	if !strings.HasSuffix(hostname, suffix) {
		return validatedHost{}, false
	}
	label := strings.TrimSuffix(hostname, suffix)
	if label == "" || strings.Contains(label, ".") {
		return validatedHost{}, false
	}
	if _, reserved := reservedSubdomainLabels[label]; reserved {
		return validatedHost{}, false
	}
	if !isValidDNSLabel(label) {
		return validatedHost{}, false
	}
	authority := hostname
	if port != "" {
		authority = net.JoinHostPort(hostname, port)
	}
	return validatedHost{
		Hostname:  hostname,
		Authority: authority,
		DNSLabel:  label,
		Port:      port,
	}, true
}

func extractNormalizedHostname(rawHost string) (string, string, bool) {
	trimmed := strings.TrimSpace(rawHost)
	if trimmed == "" || strings.ContainsAny(trimmed, "/\\?#@ \t\r\n") {
		return "", "", false
	}
	host, port, ok := splitHostAndOptionalPort(trimmed)
	if !ok || host == "" || net.ParseIP(host) != nil {
		return "", "", false
	}
	return strings.ToLower(host), port, true
}

func splitHostAndOptionalPort(raw string) (string, string, bool) {
	if strings.HasPrefix(raw, "[") || strings.Count(raw, ":") > 1 {
		return "", "", false
	}
	if !strings.Contains(raw, ":") {
		return raw, "", true
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" || port == "" {
		return "", "", false
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 || strconv.Itoa(portNum) != port {
		return "", "", false
	}
	return host, port, true
}

func isValidDNSLabel(label string) bool {
	if len(label) < 1 || len(label) > 63 {
		return false
	}
	for i := 0; i < len(label); i++ {
		ch := label[i]
		isAlphaNum := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		if isAlphaNum {
			continue
		}
		if ch == '-' && i > 0 && i < len(label)-1 {
			continue
		}
		return false
	}
	return true
}

func isHTTPSRequest(r *http.Request, trustedProxies []netip.Prefix) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !isTrustedPeer(r.RemoteAddr, trustedProxies) {
		return false
	}
	values := r.Header.Values("X-Forwarded-Proto")
	if len(values) == 0 {
		return false
	}
	lastHop := ""
	for _, rawVal := range values {
		for _, part := range strings.Split(rawVal, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				return false
			}
			lastHop = trimmed
		}
	}
	return strings.EqualFold(lastHop, "https")
}

func isTrustedPeer(remoteAddr string, trustedProxies []netip.Prefix) bool {
	if len(trustedProxies) == 0 || strings.TrimSpace(remoteAddr) == "" {
		return false
	}
	ipText := strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(ipText); err == nil {
		ipText = host
	}
	addr, err := netip.ParseAddr(ipText)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func httpsRedirectTarget(host validatedHost, r *http.Request, stripAllPorts bool) string {
	targetHost := host.Hostname
	if !stripAllPorts && host.Port != "" && host.Port != "80" && host.Port != "443" {
		targetHost = host.Authority
	}
	return "https://" + targetHost + safeRequestURI(r)
}

func safeRequestURI(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	escapedPath := r.URL.EscapedPath()
	if escapedPath == "" || !strings.HasPrefix(escapedPath, "/") {
		escapedPath = "/" + strings.TrimLeft(escapedPath, "/")
	}
	for strings.HasPrefix(escapedPath, "//") {
		escapedPath = "/" + strings.TrimLeft(escapedPath, "/")
	}
	if strings.HasPrefix(escapedPath, "/\\") {
		escapedPath = "/"
	}
	target := escapedPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	if containsControlChar(target) {
		return "/"
	}
	return target
}

func validateReturnPath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 2048 || containsControlChar(trimmed) {
		return "", false
	}
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/\\") {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return "", false
	}
	if parsed.Path == "/_soha/auth/callback" || parsed.Path == "/_soha/logout" {
		return "/", true
	}
	return trimmed, true
}

func validateSameOrigin(r *http.Request, expectedScheme string, host validatedHost, requireSignal bool) bool {
	if r == nil {
		return false
	}
	fetchSiteValues := r.Header.Values("Sec-Fetch-Site")
	if len(fetchSiteValues) > 1 {
		return false
	}
	hasSameOriginFetchSite := false
	if len(fetchSiteValues) == 1 {
		site := strings.ToLower(strings.TrimSpace(fetchSiteValues[0]))
		if site != "" && site != "same-origin" && site != "none" {
			return false
		}
		hasSameOriginFetchSite = site == "same-origin"
	}
	originValues := r.Header.Values("Origin")
	if len(originValues) > 1 {
		return false
	}
	if len(originValues) == 1 && strings.TrimSpace(originValues[0]) != "" {
		return originMatchesExpected(originValues[0], expectedScheme, host)
	}
	if requireSignal {
		return hasSameOriginFetchSite
	}
	return true
}

func originMatchesExpected(rawOrigin, expectedScheme string, host validatedHost) bool {
	trimmed := strings.TrimSpace(rawOrigin)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(parsed.Scheme, expectedScheme) || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	originHost, originPort, ok := splitHostAndOptionalPort(parsed.Host)
	if !ok || !strings.EqualFold(originHost, host.Hostname) {
		return false
	}
	return normalizeSchemePort(expectedScheme, originPort) == normalizeSchemePort(expectedScheme, host.Port)
}

func normalizeSchemePort(scheme, port string) string {
	if port != "" {
		return port
	}
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func containsControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
