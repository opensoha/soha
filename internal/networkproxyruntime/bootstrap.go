package networkproxyruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"sigs.k8s.io/yaml"
)

func bootstrapConfiguration(ctx context.Context, engine string, doc map[string]any, p *domain.Protection) error {
	scope, err := proxytransport.NewScope(p.CIDRs, p.Hosts)
	if err != nil {
		return err
	}
	// Destination rewrites and chained transports can evade the final-IP check.
	if hasUnmanagedDial(doc) {
		return errUnsafeConfiguration
	}
	switch engine {
	case domain.EngineMihomo:
		return bootstrapMihomo(ctx, doc, scope)
	case domain.EngineSingBox:
		return bootstrapSingBox(ctx, doc, scope)
	case domain.EngineV2Ray:
		return bootstrapV2Ray(ctx, doc, scope)
	}
	return errUnsafeConfiguration
}
func hasUnmanagedDial(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			if slices.Contains([]string{"dialer-proxy", "dialerProxy", "detour", "proxySettings", "override_address", "override-address", "redirect", "redirects", "download_detour", "download-via", "external-ui-url", "external-ui", "external-ui-name", "external-controller-pipe", "external-controller-unix", "external-controller-tls", "certificate-provider", "certificate_provider", "ntp", "observatory", "burstObservatory", "browserForwarder"}, key) && nonemptySection(value) {
				return true
			}
			if key == "skip-cert-verify" && value == true || key == "insecure" && value == true || key == "allowInsecure" && value == true {
				return true
			}
			if hasUnmanagedDial(value) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if hasUnmanagedDial(child) {
				return true
			}
		}
	}
	return false
}
func fetchBootstrap(ctx context.Context, rawURL string, scope *proxytransport.Scope) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errUnsafeConfiguration
	}
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 5 * time.Second, TLSHandshakeTimeout: 5 * time.Second, DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			return nil, err
		}
		ip, err := scope.Resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("guarded bootstrap download failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("guarded bootstrap HTTP status rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errUnsafeConfiguration
	}
	return raw, nil
}
func bootstrapMihomo(ctx context.Context, doc map[string]any, scope *proxytransport.Scope) error {
	nodes, err := arraySection(doc["proxies"])
	if err != nil {
		return err
	}
	providers, err := object(doc["proxy-providers"])
	if err != nil || len(providers) > 32 {
		return errUnsafeConfiguration
	}
	providerNames := map[string][]any{}
	for name, raw := range providers {
		provider, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		payload, err := mihomoProviderPayload(ctx, provider, scope)
		if err != nil {
			return err
		}
		providerNames[name] = []any{}
		for _, raw := range payload {
			node, ok := raw.(map[string]any)
			if !ok {
				return errUnsafeConfiguration
			}
			providerNames[name] = append(providerNames[name], node["name"])
			nodes = append(nodes, node)
		}
	}
	if err := inlineMihomoGroups(doc, providerNames); err != nil {
		return err
	}

	if len(nodes) > 127 {
		return errUnsafeConfiguration
	}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		if err := pinMihomoTransport(ctx, node, scope); err != nil {
			return err
		}
	}
	if err := inlineMihomoRules(ctx, doc, scope); err != nil {
		return err
	}
	doc["proxies"] = nodes
	delete(doc, "proxy-providers")
	// Bootstrap is owned by this runtime. The engine never refreshes remote
	// providers or downloads geodata outside the enforcement transport.
	doc["geodata-mode"], doc["geo-auto-update"], doc["sniffer"] = false, false, map[string]any{"enable": false}
	delete(doc, "geox-url")
	delete(doc, "profile")
	doc["dns"] = map[string]any{"enable": false}
	delete(doc, "hosts")
	return nil
}
func mihomoProviderPayload(ctx context.Context, provider map[string]any, scope *proxytransport.Scope) ([]any, error) {
	kind, _ := provider["type"].(string)
	if kind == "inline" {
		return arraySection(provider["payload"])
	}
	if kind != "http" {
		return nil, errUnsafeConfiguration
	}
	rawURL, _ := provider["url"].(string)
	raw, err := fetchBootstrap(ctx, rawURL, scope)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	var doc map[string]any
	if yaml.UnmarshalStrict(raw, &doc) != nil {
		return nil, errUnsafeConfiguration
	}
	return arraySection(doc["proxies"])
}
func pinMihomoTransport(ctx context.Context, node map[string]any, scope *proxytransport.Scope) error {
	kind, _ := node["type"].(string)
	if !slices.Contains([]string{"http", "socks5", "ss", "ssr", "snell", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "anytls"}, kind) {
		return errUnsafeConfiguration
	}
	host, _ := node["server"].(string)
	port, err := strconv.Atoi(fmt.Sprint(node["port"]))
	if err != nil || port < 1 || port > 65535 {
		return errUnsafeConfiguration
	}
	ip, err := scope.Resolve(ctx, host)
	if err != nil {
		return err
	}
	if _, err := netip.ParseAddr(host); err != nil {
		if node["servername"] == nil {
			node["servername"] = host
		}
		if node["sni"] == nil {
			node["sni"] = host
		}
	}
	node["server"], node["port"], node["udp"] = ip.String(), port, false
	return nil
}
func inlineMihomoRules(ctx context.Context, doc map[string]any, scope *proxytransport.Scope) error {
	providers, err := object(doc["rule-providers"])
	if err != nil || len(providers) > 32 {
		return errUnsafeConfiguration
	}
	rules, err := arraySection(doc["rules"])
	if err != nil {
		return err
	}
	out := []any{}
	for _, raw := range rules {
		rule, ok := raw.(string)
		if !ok {
			return errUnsafeConfiguration
		}
		parts := strings.Split(rule, ",")
		if parts[0] == "GEOIP" || parts[0] == "GEOSITE" || parts[0] == "SUB-RULE" {
			return errUnsafeConfiguration
		}
		if parts[0] != "RULE-SET" {
			out = append(out, rule)
			continue
		}
		if len(parts) != 3 {
			return errUnsafeConfiguration
		}
		provider, ok := providers[parts[1]].(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		values, err := mihomoRulePayload(ctx, provider, scope)
		if err != nil {
			return err
		}
		for _, value := range values {
			entry, ok := value.(string)
			if !ok {
				return errUnsafeConfiguration
			}
			switch provider["behavior"] {
			case "domain":
				entry = "DOMAIN-SUFFIX," + strings.TrimPrefix(strings.TrimPrefix(entry, "+."), ".")
			case "ipcidr":
				entry = "IP-CIDR," + entry
			case "classical":
			default:
				return errUnsafeConfiguration
			}
			out = append(out, entry+","+parts[2])
			if len(out) > 16384 {
				return errUnsafeConfiguration
			}
		}
	}
	doc["rules"] = out
	delete(doc, "rule-providers")
	return nil
}
func mihomoRulePayload(ctx context.Context, provider map[string]any, scope *proxytransport.Scope) ([]any, error) {
	if provider["type"] == "inline" {
		return arraySection(provider["payload"])
	}
	if provider["type"] != "http" || provider["format"] == "mrs" {
		return nil, errUnsafeConfiguration
	}
	rawURL, _ := provider["url"].(string)
	raw, err := fetchBootstrap(ctx, rawURL, scope)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	if provider["format"] == "text" {
		out := []any{}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				out = append(out, line)
			}
		}
		return out, nil
	}
	var doc map[string]any
	if yaml.UnmarshalStrict(raw, &doc) != nil {
		return nil, errUnsafeConfiguration
	}
	return arraySection(doc["payload"])
}
func bootstrapSingBox(ctx context.Context, doc map[string]any, scope *proxytransport.Scope) error {
	if nonemptySection(doc["endpoints"]) {
		return errUnsafeConfiguration
	}
	route, err := object(doc["route"])
	if err != nil {
		return err
	}
	if nonemptySection(route["rule_set"]) || nonemptySection(route["geoip"]) || nonemptySection(route["geosite"]) {
		return errUnsafeConfiguration
	}
	nodes, err := arraySection(doc["outbounds"])
	if err != nil {
		return err
	}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		kind, _ := node["type"].(string)
		if slices.Contains([]string{"direct", "block", "selector", "urltest"}, kind) {
			continue
		}
		if !slices.Contains([]string{"http", "socks", "shadowsocks", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "anytls"}, kind) {
			return errUnsafeConfiguration
		}
		host, _ := node["server"].(string)
		ip, err := scope.Resolve(ctx, host)
		if err != nil {
			return err
		}
		tls, err := object(node["tls"])
		if err != nil {
			return err
		}
		if _, err := netip.ParseAddr(host); err != nil && tls["server_name"] == nil {
			tls["server_name"] = host
		}
		if len(tls) > 0 {
			node["tls"] = tls
		}
		node["server"] = ip.String()
	}
	// Local OS resolution is for rule classification only. The bridge resolves
	// again and supplies the only destination IP accepted by the backend.
	doc["dns"] = map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "soha-local"}}, "final": "soha-local"}
	return nil
}
func bootstrapV2Ray(ctx context.Context, doc map[string]any, scope *proxytransport.Scope) error {
	nodes, err := arraySection(doc["outbounds"])
	if err != nil {
		return err
	}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		kind, _ := node["protocol"].(string)
		if kind == "freedom" || kind == "blackhole" {
			continue
		}
		if !slices.Contains([]string{"http", "socks", "shadowsocks", "vmess", "vless", "trojan"}, kind) {
			return errUnsafeConfiguration
		}
		settings, err := object(node["settings"])
		if err != nil {
			return err
		}
		entries, err := arraySection(settings["servers"])
		if err != nil {
			return err
		}
		if kind == "vmess" || kind == "vless" {
			entries, err = arraySection(settings["vnext"])
			if err != nil {
				return err
			}
		}
		if len(entries) == 0 || len(entries) > 32 {
			return errUnsafeConfiguration
		}
		for _, raw := range entries {
			server, ok := raw.(map[string]any)
			if !ok {
				return errUnsafeConfiguration
			}
			host, _ := server["address"].(string)
			ip, err := scope.Resolve(ctx, host)
			if err != nil {
				return err
			}
			server["address"] = ip.String()
			if err := pinV2RayTLS(node, host); err != nil {
				return err
			}
		}
	}
	doc["dns"] = map[string]any{"servers": []string{"localhost"}}
	route, err := object(doc["routing"])
	if err != nil {
		return err
	}
	if nonemptySection(route["balancers"]) {
		return errUnsafeConfiguration
	}
	return nil
}
func pinV2RayTLS(node map[string]any, host string) error {
	stream, err := object(node["streamSettings"])
	if err != nil {
		return err
	}
	if stream["security"] != "tls" {
		return nil
	}
	tls, err := object(stream["tlsSettings"])
	if err != nil {
		return err
	}
	if _, err := netip.ParseAddr(host); err != nil && tls["serverName"] == nil {
		tls["serverName"] = host
	}
	stream["tlsSettings"] = tls
	node["streamSettings"] = stream
	return nil
}

func inlineMihomoGroups(doc map[string]any, providerNames map[string][]any) error {
	groups, err := arraySection(doc["proxy-groups"])
	if err != nil {
		return err
	}
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		uses, err := arraySection(group["use"])
		if err != nil {
			return err
		}
		members, err := arraySection(group["proxies"])
		if err != nil {
			return err
		}
		for _, raw := range uses {
			name, ok := raw.(string)
			if !ok || providerNames[name] == nil {
				return errUnsafeConfiguration
			}
			members = append(members, providerNames[name]...)
		}
		group["proxies"] = members
		delete(group, "use")
	}
	return nil
}
